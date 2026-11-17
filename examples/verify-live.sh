#!/usr/bin/env bash
# verify-live.sh: deploy the examples to a real OpenShift cluster with bin/score-ocp and check that they work.
#
#   examples/verify-live.sh [EXAMPLE...]     # default: every example; run after `oc login` as a normal user
#
# Each example is deployed into a fresh project <PREFIX><example> (default ex-<example>), which is deleted at the
# end unless KEEP=1. Logs of every score-ocp/oc run go to $LOG_DIR (default: a temporary directory, printed).
#
# Environment: OC (default oc), SCORE_K8S (default score-k8s), PREFIX (default ex-), KEEP=1, LOG_DIR,
# FASTAPI_BUNDLE (a Linux fastapi-hello-env.tar.gz for examples/fastapi; that example is skipped without it).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$HERE")"
OC="${OC:-oc}"
export SCORE_K8S="${SCORE_K8S:-score-k8s}"
PREFIX="${PREFIX:-ex-}"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/verify-live.XXXXXX")"
LOG_DIR="${LOG_DIR:-$WORK/logs}"
mkdir -p "$LOG_DIR"
cp -R "$HERE/app" "$WORK/app"   # variants written under $WORK keep their ../app/app.py file source
ALL=(quickstart fastapi route route-annotations build secrets statefulset shared-volume hpa multi-workload failures)

pass=0 fail=0 skip=0
results=()
CUR="" NS=""

# ---- reporting ------------------------------------------------------------------------------------------------------
record() { results+=("$(printf '%-5s %-18s %s' "$1" "$CUR" "$2")"); printf '  %-5s %s%s\n' "$1" "$2" "${3:+  -- $3}"; }
ok()   { pass=$((pass + 1)); record PASS "$1"; }
bad()  { fail=$((fail + 1)); record FAIL "$1" "${2:-}"; }
skp()  { skip=$((skip + 1)); record SKIP "$1" "${2:-}"; }
# check DESC CMD...   passes when CMD exits 0
check() { local d="$1" out; shift; if out="$("$@" 2>&1)"; then ok "$d"; else bad "$d" "$(printf '%s' "$out" | tail -1)"; fi; }
# eq DESC WANT GOT
eq() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1" "want '$2', got '$3'"; fi; }
# has DESC NEEDLE HAYSTACK
has() { case "$3" in *"$2"*) ok "$1" ;; *) bad "$1" "'$2' not in: $(printf '%s' "$3" | tr '\n' ' ' | cut -c1-200)" ;; esac; }
# eventually DESC SECONDS CMD...   passes as soon as CMD exits 0, polling every 3s
eventually() {
  local d="$1" t="$2" end out; shift 2; end=$(($(date +%s) + t))
  while :; do
    if out="$("$@" 2>&1)"; then ok "$d"; return 0; fi
    [ "$(date +%s)" -ge "$end" ] && { bad "$d" "not within ${t}s: $(printf '%s' "$out" | tail -1)"; return 1; }
    sleep 3
  done
}

# ---- cluster helpers ------------------------------------------------------------------------------------------------
oc_() { "$OC" -n "$NS" "$@"; }
# deploy [score-ocp args...] SCORE_FILE...   with this example's project label, namespace and log
deploy() {
  "$ROOT/bin/score-ocp" -p "$CUR" -n "$NS" -o "$WORK/$CUR.manifests.yaml" "$@" >>"$LOG_DIR/$CUR.log" 2>&1 \
    || { grep -v '^INFO' "$LOG_DIR/$CUR.log" | tail -2; return 1; }
}
# deploy_fails WANT [args...] FILE...   score-ocp must fail and print WANT
deploy_fails() {
  local want="$1" out; shift
  out="$("$ROOT/bin/score-ocp" -p "$CUR" -n "$NS" -o "$WORK/$CUR.rejected.yaml" "$@" 2>&1)" && { echo "score-ocp succeeded"; return 1; }
  printf '%s\n' "$out" >>"$LOG_DIR/$CUR.log"
  grep -qF -- "$want" <<<"$out" || { echo "error did not mention: $want"; return 1; }
}
fresh_ns() {
  NS="$PREFIX$CUR"
  echo; echo "== $CUR (project $NS)"
  if "$OC" get project "$NS" >/dev/null 2>&1; then "$OC" delete project "$NS" --wait=true >/dev/null 2>&1; fi
  local i
  for i in $(seq 60); do
    "$OC" new-project "$NS" --skip-config-write >/dev/null 2>&1 && return 0
    sleep 3
  done
  bad "create project $NS"; return 1
}
rollout() { oc_ rollout status "$1" --timeout="${2:-300s}"; }
host() { oc_ get route "$1" -o jsonpath='{.spec.host}'; }
code() { curl -sk -m "${2:-20}" -o /dev/null -w '%{http_code}' "$1"; }
body() { curl -sk -m "${2:-20}" "$1"; }
jp() { oc_ get "$1" -o jsonpath="$2"; }
# pod_get TARGET URL   HTTP GET from inside TARGET's pod; prints the status code or "unreachable"
pod_get() {
  oc_ exec "$1" -c main -- python -c '
import sys, urllib.request, urllib.error
try:
    print(urllib.request.urlopen(sys.argv[1], timeout=5).status)
except urllib.error.HTTPError as e:
    print(e.code)
except Exception:
    print("unreachable")' "$2" 2>/dev/null
}
# every object in the generated manifests carries app.kubernetes.io/part-of=<example> on the cluster
labelled_on_cluster() {
  local kinds missing=""
  kinds="$(awk '/^kind:/{k=$2} /^  name:/{if(k){print tolower(k)"/"$2; k=""}}' "$WORK/$CUR.manifests.yaml")"
  for o in $kinds; do
    [ "$(oc_ get "$o" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/part-of}' 2>/dev/null)" = "$CUR" ] || missing="$missing $o"
  done
  [ -z "$missing" ] || { echo "unlabelled or missing:$missing"; return 1; }
}
restricted_pod() {  # restricted_pod LABEL_SELECTOR [CONTAINER]: SCC, UID in the namespace range, no root, no token
  local pod scc uid range start size c="${2:-main}"
  pod="$(oc_ get pod -l "$1" --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}')"
  scc="$(jp "pod/$pod" '{.metadata.annotations.openshift\.io/scc}')"
  [ "$scc" = restricted-v2 ] || { echo "SCC $scc"; return 1; }
  uid="$(oc_ exec "$pod" -c "$c" -- id -u)"
  range="$("$OC" get namespace "$NS" -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.uid-range}')"
  start="${range%/*}"; size="${range#*/}"
  [ "$uid" -ge "$start" ] && [ "$uid" -lt $((start + size)) ] || { echo "uid $uid not in $range"; return 1; }
  oc_ exec "$pod" -c "$c" -- sh -c 'touch /probe 2>/dev/null' && { echo "root filesystem is writable"; return 1; }
  oc_ exec "$pod" -c "$c" -- test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token || { echo "SA token mounted"; return 1; }
}

# ---- examples -------------------------------------------------------------------------------------------------------
ex_quickstart() {
  CUR=quickstart; fresh_ns || return
  oc_ create secret generic web-secrets --from-literal=GREETING='hello from a Secret' >/dev/null
  local f="$HERE/quickstart/score.yaml" h gen nbuilds id1
  check "one command: empty project -> first build -> rollout" deploy "$f"
  check "build pushed ImageStreamTag web:latest" oc_ get istag web:latest
  check "Deployment rolled out" rollout deploy/web
  h="$(host web-route)"
  has "HTTPS via the assigned Route host, Secret key in env" "greeting=hello from a Secret" "$(body "https://$h/")"
  eq "HTTP redirects to HTTPS" 302 "$(code "http://$h/")"
  eq "selector uses the stable instance label (00-stable-instance)" web \
    "$(jp deploy/web '{.spec.selector.matchLabels.app\.kubernetes\.io/instance}')"
  has "image trigger on web:latest (20-image-trigger)" '"name":"web:latest"' \
    "$(jp deploy/web '{.metadata.annotations.image\.openshift\.io/triggers}')"
  check "every object labelled part-of=quickstart (90-project-label)" labelled_on_cluster
  check "pod restricted-v2, UID from namespace range, read-only root, no token (60-security-context)" \
    restricted_pod app.kubernetes.io/name=web

  gen="$(jp deploy/web '{.metadata.generation}')"; nbuilds="$(oc_ get builds -o name | wc -l | tr -d ' ')"
  check "re-deploy accepted (immutable selector unchanged)" deploy "$f"
  eq "re-deploy starts no build (tag exists)" "$nbuilds" "$(oc_ get builds -o name | wc -l | tr -d ' ')"
  eq "re-deploy causes no rollout" "$gen" "$(jp deploy/web '{.metadata.generation}')"

  id1="$(body "https://$h/" | sed -n 's/.*build=\([0-9]*\).*/\1/p')"
  check "second build (oc start-build)" oc_ start-build web --from-dir="$HERE/quickstart" --wait
  new_build_served() { local b; b="$(body "https://$h/" | sed -n 's/.*build=\([0-9]*\).*/\1/p')"; [ -n "$b" ] && [ "$b" != "$id1" ]; }
  eventually "second build rolls the Deployment without re-apply" 180 new_build_served

  mkdir -p "$WORK/quickstart-noroute"
  sed '/^  route:$/,$d' "$f" >"$WORK/quickstart-noroute/score.yaml"
  check "re-deploy without the route resource" deploy "$WORK/quickstart-noroute/score.yaml"
  check "Route pruned" sh -c "! '$OC' -n '$NS' get route web-route 2>/dev/null"
  check "Deployment kept" oc_ get deploy web
}

ex_fastapi() {
  CUR=fastapi; NS=""
  if [ ! -f "${FASTAPI_BUNDLE:-}" ]; then
    echo; echo "== fastapi"; skp "fastapi (Pixi bundle in a Debian image)" "set FASTAPI_BUNDLE to a Linux fastapi-hello-env.tar.gz"
    return
  fi
  fresh_ns || return
  local app="$WORK/fastapi-app" h
  mkdir -p "$app" && cp "$HERE/fastapi/score.yaml" "$HERE/fastapi/Dockerfile" "$app/" && cp "$FASTAPI_BUNDLE" "$app/fastapi-hello-env.tar.gz"
  check "one command: empty project -> image built on the cluster from the bundle -> rollout" deploy "$app/score.yaml"
  check "Deployment rolled out" rollout deploy/fastapi-hello
  h="$(host fastapi-hello-route)"
  eq "GET / over the Route" '{"message":"Hello from fastapi-hello","platform":"kubernetes"}' "$(body "https://$h/")"
  eq "GET /healthz" '{"status":"ok"}' "$(body "https://$h/healthz")"
  eq "HTTP redirects to HTTPS" 302 "$(code "http://$h/")"
  eq "Service 8080 -> container 8000" 8000 "$(jp svc/fastapi-hello '{.spec.ports[0].targetPort}')"
  eq "runs the bundle's own Python" /opt/app/env "$(oc_ exec deploy/fastapi-hello -c api -- /opt/app/env/bin/python3.13 -c 'import sys; print(sys.prefix)')"
  eq "no other Python in the image" none "$(oc_ exec deploy/fastapi-hello -c api -- sh -c 'command -v python3 python || echo none')"
  check "pod restricted-v2, random UID (not the image's nobody), read-only root, no token" \
    restricted_pod app.kubernetes.io/name=fastapi-hello api
}

ex_route() {
  CUR=route; fresh_ns || return
  local w="$HERE/route/web.score.yaml" c="$HERE/route/client.score.yaml" h
  check "server-side dry run" deploy --dry-run "$w" "$c"
  eq "dry run created nothing" "" "$(oc_ get deploy,route,svc,networkpolicy -o name)"
  check "deploy web + client" deploy "$w" "$c"
  check "web rolled out" rollout deploy/web
  check "client rolled out" rollout deploy/client
  h="$(host web-route)"
  eventually "HTTPS /api reaches web" 60 sh -c "[ \"\$(curl -sk -m 10 -o /dev/null -w '%{http_code}' https://$h/api)\" = 200 ]"
  eq "path outside /api is not routed (router 503)" 503 "$(code "https://$h/")"
  eq "HTTP redirects to HTTPS" 302 "$(code "http://$h/api")"
  check "allow-from-router NetworkPolicy exists" oc_ get networkpolicy web-route-allow-router
  eq "client -> web blocked by the policy" unreachable "$(pod_get deploy/client http://web:8080/healthz)"
  eq "web -> client open (no policy on client)" 200 "$(pod_get deploy/web http://client:8080/healthz)"
  check "re-deploy with only web.score.yaml" deploy "$w"
  check "client Deployment, Service and ConfigMap pruned" sh -c \
    "[ -z \"\$('$OC' -n '$NS' get deploy/client svc/client configmap/client-main-file-f644ad79d1 -o name 2>/dev/null)\" ]"
  eq "web still served" 200 "$(code "https://$h/api")"
}

ex_route_annotations() {
  CUR=route-annotations; fresh_ns || return
  local f="$HERE/route-annotations/score.yaml" h t0 c dt
  check "deploy" deploy "$f"
  check "rolled out" rollout deploy/slow
  eq "timeout annotation on Route (30-route-annotations)" 45s "$(jp route/slow-route '{.metadata.annotations.haproxy\.router\.openshift\.io/timeout}')"
  eq "cert-manager annotation on Route" corp-ca "$(jp route/slow-route '{.metadata.annotations.cert-manager\.io/issuer-name}')"
  eq "other prefixes not copied" "" "$(jp route/slow-route '{.metadata.annotations.example\.com/team}')"
  h="$(host slow-route)"
  eventually "Route serving" 60 sh -c "[ \"\$(curl -sk -m 10 -o /dev/null -w '%{http_code}' https://$h/healthz)\" = 200 ]"
  eq "35 s request succeeds under the 45 s timeout" 200 "$(code "https://$h/slow?s=35" 60)"
  mkdir -p "$WORK/annotations-default"
  grep -v 'haproxy.router.openshift.io/timeout' "$f" >"$WORK/annotations-default/score.yaml"
  check "re-deploy without the timeout annotation" deploy "$WORK/annotations-default/score.yaml"
  eq "annotation removed from Route" "" "$(jp route/slow-route '{.metadata.annotations.haproxy\.router\.openshift\.io/timeout}')"
  sleep 10   # router reload
  t0=$(date +%s); c="$(code "https://$h/slow?s=35" 60)"; dt=$(($(date +%s) - t0))
  eq "without it the same request gets the router's 30 s timeout (504)" 504 "$c"
  [ "$dt" -lt 34 ] && ok "504 after ${dt}s (router default)" || bad "504 after ${dt}s (router default)" "expected ~30s"
}

ex_build() {
  CUR=build; fresh_ns || return
  oc_ create secret generic npmrc --from-literal=.npmrc='//registry.npmjs.org/:_authToken=example' >/dev/null
  oc_ create secret generic certs --from-literal=ca.crt='example CA' >/dev/null
  local log
  check "deploy, first build with two build secrets" deploy "$HERE/build/score.yaml"
  log="$(oc_ logs build/builder-1 2>&1)"
  has "secret npmrc mounted at the default path during RUN" "build secret npmrc: mounted at /build-secrets/npmrc" "$log"
  has "secret certs mounted at its mountPath during RUN" "build secret certs: mounted at /etc/pki/build-certs" "$log"
  check "rolled out" rollout deploy/builder
  check "build secrets are not in the image" oc_ exec deploy/builder -c main -- \
    sh -c 'test ! -e /build-secrets/npmrc/.npmrc && test ! -e /etc/pki/build-certs/ca.crt'
  eventually "extra ImageStream tag ubi9-python:312 imported" 120 oc_ get istag ubi9-python:312
  eq "annotations on the BuildConfig" payments "$(jp bc/builder '{.metadata.annotations.example\.com/team}')"
  eq "build pod resource limits" 1Gi "$(jp bc/builder '{.spec.resources.limits.memory}')"

  mkdir -p "$WORK/broken"
  printf 'FROM registry.access.redhat.com/ubi9/python-312\nRUN echo failing on purpose && exit 1\n' >"$WORK/broken/Dockerfile"
  printf 'apiVersion: score.dev/v1b1\nmetadata:\n  name: broken\ncontainers:\n  main:\n    image: broken:latest\nresources:\n  build:\n    type: openshift-build\n' \
    >"$WORK/broken/score.yaml"
  local out rc
  out="$("$ROOT/bin/score-ocp" -p broken -n "$NS" -o "$WORK/broken.yaml" "$WORK/broken/score.yaml" 2>&1)"; rc=$?
  printf '%s\n' "$out" >>"$LOG_DIR/$CUR.log"
  [ "$rc" -ne 0 ] && ok "a failing first build fails score-ocp (exit $rc)" || bad "a failing first build fails score-ocp" "exit 0"
}

ex_secrets() {
  CUR=secrets; fresh_ns || return
  oc_ create secret generic app-config --from-literal=GREETING=hi --from-literal=LOG_FORMAT=json >/dev/null
  waiting_msg() { oc_ get pod -l app.kubernetes.io/name=api -o jsonpath='{.items[*].status.containerStatuses[*].state.waiting.message}' | grep -q 'secret "db-creds" not found'; }
  check "deploy before Secret db-creds exists" deploy "$HERE/secrets/score.yaml"
  eventually "missing Secret stops the pod: secret \"db-creds\" not found" 120 waiting_msg
  oc_ create secret generic db-creds --from-literal=DB_PASSWORD=s3cret >/dev/null
  check "pod starts once the Secret exists" rollout deploy/api
  eq "main gets GREETING from app-config" hi "$(oc_ exec deploy/api -c main -- printenv GREETING)"
  eq "main gets DB_PASSWORD from db-creds" s3cret "$(oc_ exec deploy/api -c main -- printenv DB_PASSWORD)"
  eq "second container gets both Secrets too" "json s3cret" \
    "$(oc_ exec deploy/api -c worker -- sh -c 'echo $LOG_FORMAT $DB_PASSWORD')"
  eq "plain variables still set" info "$(oc_ exec deploy/api -c main -- printenv LOG_LEVEL)"
  eq "no Secret object in the manifests" 0 "$(grep -c '^kind: Secret$' "$WORK/secrets.manifests.yaml")"
}

ex_statefulset() {
  CUR=statefulset; fresh_ns || return
  local f="$HERE/statefulset/score.yaml" pvc
  check "deploy" deploy "$f"
  check "rolled out" rollout sts/store
  pvc="$(jp sts/store '{.spec.volumeClaimTemplates[0].metadata.name}')-store-0"
  eq "PVC $pvc bound" Bound "$(jp "pvc/$pvc" '{.status.phase}')"
  check "SCC assigned an fsGroup" test -n "$(jp pod/store-0 '{.spec.securityContext.fsGroup}')"
  check "random UID can write to the volume" oc_ exec store-0 -c main -- sh -c 'echo kept > /data/note'
  oc_ delete pod store-0 --wait=true >/dev/null
  check "pod recreated" sh -c "sleep 5; '$OC' -n '$NS' wait --for=condition=Ready pod/store-0 --timeout=180s"
  eq "data survives pod deletion" kept "$(oc_ exec store-0 -c main -- cat /data/note)"
  check "pod restricted-v2, UID from namespace range, read-only root, no token" restricted_pod app.kubernetes.io/name=store

  mkdir -p "$WORK/typo"
  sed 's/k8s.score.dev\/kind: StatefulSet/k8s.score.dev\/kind: Statefulset/' "$f" >"$WORK/typo/score.yaml"
  check "kind typo rejected before apply (05-workload-guard)" deploy_fails "produced no Deployment or StatefulSet" "$WORK/typo/score.yaml"
  check "running StatefulSet not pruned" oc_ get sts store
  eq "pod still ready" True "$(jp pod/store-0 '{.status.conditions[?(@.type=="Ready")].status}')"
}

ex_shared_volume() {
  CUR=shared-volume; fresh_ns || return
  local f="$HERE/shared-volume/score.yaml"
  check "deploy" deploy "$f"
  check "rolled out" rollout deploy/files
  eq "shared PVC files-uploads bound" Bound "$(jp pvc/files-uploads '{.status.phase}')"
  check "pod writes to the PVC" oc_ exec deploy/files -c main -- sh -c 'echo x > /uploads/x'
  mkdir -p "$WORK/novol"
  sed -e '/^    volumes:$/,/resources.uploads/d' -e '/^resources:$/,$d' "$f" >"$WORK/novol/score.yaml"
  check "re-deploy without the volume" deploy "$WORK/novol/score.yaml"
  check "PVC kept by default (data safety)" oc_ get pvc files-uploads
  check "re-deploy with SCORE_OCP_PRUNE_PVC=1" env SCORE_OCP_PRUNE_PVC=1 "$ROOT/bin/score-ocp" -p "$CUR" -n "$NS" \
    -o "$WORK/$CUR.manifests.yaml" "$WORK/novol/score.yaml"
  pvc_gone() { [ -z "$(oc_ get pvc files-uploads -o name 2>/dev/null)" ] || [ -n "$(jp pvc/files-uploads '{.metadata.deletionTimestamp}')" ]; }
  eventually "PVC pruned with SCORE_OCP_PRUNE_PVC=1" 60 pvc_gone
}

ex_hpa() {
  CUR=hpa; fresh_ns || return
  local active pods p
  check "deploy" deploy "$HERE/hpa/score.yaml"
  ready2() { [ "$(jp deploy/scaler '{.status.readyReplicas}')" = 2 ]; }
  eventually "HPA scales the Deployment to minReplicas 2" 180 ready2
  eq "HPA found its target (AbleToScale)" True "$(jp hpa/scaler '{.status.conditions[?(@.type=="AbleToScale")].status}')"
  pdb1() { [ "$(jp pdb/scaler '{.status.disruptionsAllowed}')" = 1 ]; }
  eventually "PDB allows exactly 1 disruption" 60 pdb1
  local i reason
  for i in $(seq 30); do   # the HPA reports ScalingActive after its first metrics poll
    active="$(jp hpa/scaler '{.status.conditions[?(@.type=="ScalingActive")].status}')"
    [ -n "$active" ] && break
    sleep 3
  done
  if [ "$active" != True ]; then
    reason="$(jp hpa/scaler '{.status.conditions[?(@.type=="ScalingActive")].reason}')"
    skp "scale up on CPU load" "no CPU metrics: ScalingActive=${active:-unset} ${reason}"
    return
  fi
  pods="$(oc_ get pod -l app.kubernetes.io/name=scaler -o jsonpath='{.items[*].metadata.name}')"
  for p in $pods; do oc_ exec "$p" -c main -- timeout 240 python -c 'while True: pass' >/dev/null 2>&1 & done
  scaled() { [ "$(jp hpa/scaler '{.status.desiredReplicas}')" -gt 2 ]; }
  eventually "scale up on CPU load" 240 scaled
  wait
}

ex_multi_workload() {
  CUR=multi-workload; fresh_ns || return
  oc_ create secret generic cache-secrets --from-literal=CACHE_PASSWORD=changeme >/dev/null
  local w="$HERE/multi-workload/web.score.yaml" c="$HERE/multi-workload/cache.score.yaml" h
  check "deploy web + cache in one command" deploy "$w" "$c"
  check "web rolled out" rollout deploy/web
  check "cache rolled out" rollout sts/cache
  eq "web gets CACHE_HOST from service-port" cache "$(oc_ exec deploy/web -c main -- printenv CACHE_HOST)"
  eq "web reaches cache on 6379" 200 "$(pod_get deploy/web http://cache:6379/healthz)"
  eq "cache gets its Secret" changeme "$(oc_ exec cache-0 -c main -- printenv CACHE_PASSWORD)"
  h="$(host web-route)"
  eventually "web served via its Route" 60 sh -c "[ \"\$(curl -sk -m 10 -o /dev/null -w '%{http_code}' https://$h/)\" = 200 ]"
  "$ROOT/bin/score-ocp" -p "$CUR" --generate-only -o "$WORK/order1.yaml" "$w" "$c" >/dev/null 2>&1
  "$ROOT/bin/score-ocp" -p "$CUR" --generate-only -o "$WORK/order2.yaml" "$c" "$w" >/dev/null 2>&1
  check "same manifests whatever the file order (99-sort)" cmp -s "$WORK/order1.yaml" "$WORK/order2.yaml"
  check "every object labelled part-of=multi-workload" labelled_on_cluster
  check "re-deploy with only cache.score.yaml" deploy "$c"
  check "web Deployment, Service and Route pruned" sh -c \
    "[ -z \"\$('$OC' -n '$NS' get deploy/web svc/web route/web-route -o name 2>/dev/null)\" ]"
  check "cache untouched" oc_ get sts cache
}

ex_failures() {
  CUR=failures; NS=""; echo; echo "== failures (no cluster changes)"
  local f want
  for f in "$HERE"/failures/*.score.yaml; do
    want="$(sed -n 's/^# expect: //p' "$f")"
    check "$(basename "$f" .score.yaml): $want" deploy_fails "$want" --generate-only "$f"
  done
}

# ---- main -----------------------------------------------------------------------------------------------------------
command -v "$OC" >/dev/null || { echo "oc not found (set OC)" >&2; exit 2; }
"$OC" whoami >/dev/null 2>&1 || { echo "not logged in: run oc login first" >&2; exit 2; }
[ $# -gt 0 ] && targets=("$@") || targets=("${ALL[@]}")
echo "cluster: $("$OC" whoami --show-server) as $("$OC" whoami)   logs: $LOG_DIR"
for t in "${targets[@]}"; do
  fn="ex_${t//-/_}"
  declare -F "$fn" >/dev/null || { echo "unknown example: $t (known: ${ALL[*]})" >&2; exit 2; }
  "$fn"
  if [ "${KEEP:-0}" != 1 ] && [ -n "$NS" ]; then "$OC" delete project "$NS" --wait=false >/dev/null 2>&1; fi
done

echo; echo "== summary: $pass passed, $fail failed, $skip skipped"
printf '%s\n' "${results[@]}" | grep -v '^PASS' || true
[ "$fail" -eq 0 ]
