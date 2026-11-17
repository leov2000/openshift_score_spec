# score-k8s-openshift

One `score.yaml` → everything a workload needs on OpenShift, using **stock score-k8s** plus this bundle of
provisioners and patch templates. No fork. Implements `LOOK_score_k8s.md`.

```sh
make bin     # build the pinned score-k8s release (SCORE_K8S_VERSION) into .bin/
make test    # offline tests, no cluster
bin/score-ocp -p shop -n shop-dev web/score.yaml cache/score.yaml          # init, generate, apply --prune, first build
bin/score-ocp -p shop -n shop-dev --dry-run web/score.yaml                 # server-side dry run only
bin/score-ocp -p shop --generate-only web/score.yaml                       # just manifests.yaml
```

`score-ocp` is a thin wrapper around stock score-k8s; [Using score-k8s directly](#using-score-k8s-directly) shows the
same flow by hand.

## Deploy your own app

Put a `score.yaml` like this next to your app's `Dockerfile`; the cluster builds the image from that folder, and you
get a Deployment, a Service and an HTTPS Route. [`examples/fastapi/`](examples/fastapi/README.md) is a full example,
where the folder holds a relocatable Pixi bundle and a Debian `Dockerfile` (no Python in the image);
[`examples/quickstart/`](examples/quickstart/score.yaml) is a `Dockerfile` that copies the source into a Python image.

```yaml
apiVersion: score.dev/v1b1
metadata:
  name: myapp
containers:
  api:
    image: myapp:latest            # <metadata.name>:latest: what the build pushes
    variables:
      PORT: "8000"
    readinessProbe:
      httpGet: {path: /healthz, port: 8000}
    resources:
      requests: {cpu: 100m, memory: 128Mi}
      limits: {memory: 256Mi}
service:
  ports:
    http: {port: 8080, targetPort: 8000}
resources:
  build:
    type: openshift-build          # ImageStream + BuildConfig; new builds roll the Deployment
  route:
    type: route                    # https://<assigned host>/, edge TLS
    params: {port: http}
```

```sh
oc new-project myapp
bin/score-ocp -p myapp -n myapp path/to/myapp/score.yaml   # generate, apply, first build, rollout
curl -k "https://$(oc get route myapp-route -n myapp -o jsonpath='{.spec.host}')/"
oc start-build myapp -n myapp --from-dir=path/to/myapp --follow --wait      # after each code change
```

What the app must do:

- **Listen on the `targetPort`** and answer the probe paths (or drop the probes).
- **Run as any UID.** OpenShift ignores the image's `USER` and picks one from the project's range; files the app
  reads must be readable by any user.
- **Not write to its own filesystem.** The root filesystem is read-only, and the bundle has no `emptyDir` today
  (`TODO.md`), so an app that writes to `/tmp` or next to itself crash-loops. Most Python web apps write nothing.
- **Use `image: <metadata.name>:latest`**, or the build is never deployed (generate fails and says so).

Runnable examples for every resource type and patch template, plus Score files the bundle rejects, are in
[`examples/`](examples/README.md). `make verify-live` deploys all of them to the cluster you are logged in to and
checks them.

## Using score-k8s directly

The bundle is plain files: `provisioners/*.provisioners.yaml` and `patches/*.tpl`. Nothing is compiled into
score-k8s. `score-k8s init` copies them into the project's `.score-k8s/` folder, and `generate` reads them from
there. This is what `bin/score-ocp` does, step by step, for a CI system that runs its own steps.

**1. Get the pinned score-k8s** (`SCORE_K8S_VERSION`, currently 0.22.0). Either `make bin`, or download the official
binary for your platform from the [release page](https://github.com/score-spec/score-k8s/releases) and check it
against `checksums.txt`. Not `go install`: score-k8s sets its version only at build time, so a `go install` binary
reports 0.0.0 and fails `check-version`.

```sh
make bin && export PATH="$PWD/.bin:$PATH"    # clones the pinned tag from GitHub and builds it
score-k8s check-version "=$(cat SCORE_K8S_VERSION)"
```

**2. Init in a fresh, empty folder.** score-k8s remembers every workload it has seen in `.score-k8s/state.yaml`
and generates all of them every time. Reusing the folder would keep generating workloads you've removed, so they're
never pruned. Start from an empty folder on every run.

```sh
BUNDLE=/path/to/score-k8s-openshift
work="$(mktemp -d)" && cd "$work"
args=(--provisioners "$BUNDLE/provisioners/openshift.provisioners.yaml")
for t in "$BUNDLE"/patches/*.tpl; do args+=(--patch-templates "$t"); done   # file order matters: 99-sort runs last
score-k8s init --no-sample "${args[@]}"
```

`--provisioners` and `--patch-templates` also take `https://` URLs, so a pipeline can load the bundle without a
checkout. Pin the URLs to a tag, not a branch, and use an internal mirror in an air-gapped setup.

**3. Generate, with the project label.** `SCORE_OCP_PROJECT` makes `90-project-label` put
`app.kubernetes.io/part-of=<project>` on every object. Without it there's no label, so step 4 can't prune.
Pass Score files as absolute paths so relative `files:` sources still resolve. To deploy several workloads
together, list all their Score files in one `generate`.

```sh
SCORE_OCP_PROJECT=quickstart score-k8s generate -o manifests.yaml "$BUNDLE/examples/quickstart/score.yaml"
```

**4. Apply and prune** into an existing project. The quickstart also needs its Secret first:
`oc new-project quickstart && oc create secret generic web-secrets --from-literal=GREETING=hi`. `oc` prunes only
the kinds you list. This is the bundle's full set; add
`--prune-allowlist core/v1/PersistentVolumeClaim` only if PVCs should be deleted with their volume resource (pruning
deletes the data). Add `--dry-run=server` to check without changing anything.

```sh
oc apply -n quickstart -f manifests.yaml --prune -l app.kubernetes.io/part-of=quickstart \
  --prune-allowlist core/v1/Service --prune-allowlist core/v1/ConfigMap \
  --prune-allowlist apps/v1/Deployment --prune-allowlist apps/v1/StatefulSet \
  --prune-allowlist route.openshift.io/v1/Route --prune-allowlist build.openshift.io/v1/BuildConfig \
  --prune-allowlist image.openshift.io/v1/ImageStream \
  --prune-allowlist autoscaling/v2/HorizontalPodAutoscaler --prune-allowlist policy/v1/PodDisruptionBudget \
  --prune-allowlist networking.k8s.io/v1/NetworkPolicy
```

**5. Build (workloads with an `openshift-build` resource).** The BuildConfig takes the folder you upload. Each build
pushes `<workload>:latest`, and the image trigger rolls the workload, with no re-apply. `--wait` matters:
`--follow` alone exits 0 when the build fails.

```sh
oc start-build web -n quickstart --from-dir="$BUNDLE/examples/quickstart" --follow --wait
oc rollout status deploy/web -n quickstart
curl -k "https://$(oc get route web-route -n quickstart -o jsonpath='{.spec.host}')/"   # hello from web-… greeting=hi
```

`score-ocp` runs this only when `<workload>:latest` doesn't exist yet. After that, builds belong to your pipeline.

## Resource types

| type | emits | params |
|---|---|---|
| `route` | `route.openshift.io/v1` Route, edge TLS + redirect; optional allow-from-router NetworkPolicy | `port` (required, name or number), `host`, `path`, `networkPolicy` |
| `openshift-build` | ImageStream `<workload>` (local lookup) + binary Docker BuildConfig → `<workload>:latest` | `resources`, `pullSecret`, `pushSecret`, `secrets[]` (build volumes: name or `{name, mountPath}`), `annotations`, `imageStreams[]` |
| `openshift-secret` | nothing; `envFrom.secretRef` on every container | `name` (required) |
| `volume` | StatefulSet: volumeClaimTemplate. Deployment: shared PVC `<workload>-<res>` | `size`, `storageClassName`, `accessMode` |
| `hpa` | `autoscaling/v2` HPA (CPU) targeting the Deployment/StatefulSet; PDB when `minReplicas >= 2` | `maxReplicas` (required), `minReplicas`, `targetCPUUtilizationPercentage` |

Every provisioner rejects unknown params. Names come from workload and resource names, never `.Guid`.

## Patch templates (applied in file order)

| file | does |
|---|---|
| `00-stable-instance` | replaces the random `app.kubernetes.io/instance` suffix with the workload name (labels + selectors) |
| `05-workload-guard` | fails if a workload produced no Deployment/StatefulSet (score-k8s bug, see below) |
| `20-image-trigger` | `image.openshift.io/triggers` for containers using `<workload>:latest` when the workload has `openshift-build` |
| `30-route-annotations` | copies `cert-manager.io/*` and `haproxy.router.openshift.io/*` workload annotations onto its Routes |
| `40-secret-envfrom` | `envFrom.secretRef` per `openshift-secret` resource, all containers and init containers |
| `50-hpa-guard` | fails if a workload with `hpa` has a container without a CPU request |
| `60-security-context` | community `unprivileged.tpl` without fixed UID/GID/fsGroup, for Deployments and StatefulSets |
| `90-project-label` | `app.kubernetes.io/part-of=$SCORE_OCP_PROJECT` on every object (no-op when unset) |
| `99-sort` | stable manifest order (score-k8s emits workloads in Go map order) |

## Status

Offline: **every feature done** (`make test`; strict typed decodes against `github.com/openshift/api` and
`k8s.io/api`; golden file at `fixtures/full/manifests.golden.yaml`).

Dry-run and live: a single-node OpenShift 4.22 cluster, through `oc` as a non-admin user. The live
column is checked by `make verify-live` on the [examples](examples/README.md#checking-them); last run 2026-11-16:
104 passed, 1 skipped. That run used a local source build (the same score-k8s code as 0.22.0, different
dependency versions); every example generates byte-identical manifests with the pinned 0.22.0, and the offline
suite passes against it.

| Feature | Dry-run | Live |
|---|---|---|
| Route | ✓ | ✓ HTTPS via edge TLS, HTTP → 302 to HTTPS, path routing |
| Build | ✓ | ✓ first build from `score-ocp`; build secrets readable during `RUN`, absent from the image; a failing build fails `score-ocp` |
| Image trigger | – | ✓ a new build rolls the Deployment with no re-apply; re-apply causes no extra rollout |
| Route annotations | – | ✓ `timeout: 45s` lets a 35 s request return 200; without it the router returns 504 at 30 s |
| Secrets | – | ✓ keys in every container's env; missing Secret → `secret "…" not found`, pod doesn't start |
| Volumes | ✓ | ✓ StatefulSet PVC bound, data survives pod deletion, SCC assigns fsGroup; Deployment PVC kept on prune by default |
| HPA / PDB | ✓ | partly: HPA finds its target, enforces `minReplicas`, PDB allows 1 disruption; no CPU scaling without a metrics API |
| Security context | ✓ restricted-v2 | ✓ UID from the namespace range, read-only root filesystem, no service account token |
| NetworkPolicy | ✓ | partly: Route works with the policy; other pods are blocked, and traffic without the policy stays open. Router-side "unreachable without it" needs more than one node |
| Wrapper | ✓ | ✓ empty project → running in one command; dry run creates nothing; removed resources and workloads are pruned |
| Nomad parity | out of scope (done separately) | |
| End-to-end | ✓ full fixture admitted | ✓ two workloads with `service-port`, StatefulSet, Secret and Route (`examples/multi-workload`) |

What is still open is in [`TODO.md`](TODO.md).

## Findings that changed the plan

1. **Stock output is not deterministic.** `app.kubernetes.io/instance` gets a random suffix stored in
   `.score-k8s/state.yaml`, and it is used in the Deployment's **immutable** selector. In stateless CI every apply after
   the first would be rejected. Fixed by `00-stable-instance.tpl`.
2. **Multi-workload order is random.** `generate.go` ranges over a Go map. Fixed by `99-sort.tpl`.
3. **score-k8s silently drops workloads.** `convert/workloads.go:59` and `:124` return `errors.Wrapf(err, …)` where
   `err` is always nil, and `Wrapf` of nil is nil. So for an unsupported `k8s.score.dev/kind` (e.g. a typo) or a
   claim volume on a Deployment, score-k8s exits 0 and emits no workload. With `apply --prune` that deletes the
   running workload. Guarded by `05-workload-guard.tpl`, which stays: no upstream fix is expected soon.
   `TestStockSilentlyDropsUnsupportedKind` fails if a score-k8s upgrade ever changes this.
4. Workload annotations reach only the pod template (Deployment/Service get only `k8s.score.dev/workload-name`).
5. Provisioner templates can read `.WorkloadMetadata`, so the HPA reads `k8s.score.dev/kind` directly and needs no
   `scaleTargetRef` patch.
6. The stock `route` provisioner's path regex (`^/|(/([^/]+))+$`) accepts trailing slashes. The bundle's does not.

Found on the live cluster:

7. **Docker-strategy `source.secrets` end up in the image.** They are copied into the build context, reachable only via
   `COPY`/`ADD`, and stay in that layer (OpenShift docs, "Input secrets and config maps" > "Docker strategy"). The
   `openshift-build` provisioner uses build volumes (`dockerStrategy.volumes`) instead, mounted for `RUN` steps only,
   default path `/build-secrets/<name>`. `destinationDir` is rejected.
8. **`oc start-build --follow` exits 0 when the build fails.** `score-ocp` uses `--follow --wait`.
9. **score-k8s accumulates workloads in `.score-k8s/state.yaml`** and generates all of them, so a workload dropped
   from the command line was never pruned, and two projects run from one directory relabelled each other's objects.
   `score-ocp` now runs score-k8s in a fresh temporary directory, generating exactly the Score files given. Stock
   provisioners that keep random state (e.g. generated database passwords) are therefore not supported through
   `score-ocp`; every bundle provisioner is deterministic.
10. Re-applying prints `configured` for the Deployment and PDB, but it's a no-op: generation and ReplicaSets are
    unchanged, because OpenShift re-resolves `<workload>:latest` to the trigger's digest during admission.
11. Both router namespaces (`openshift-ingress`, `openshift-host-network`) carry `network.openshift.io/policy-group:
    ingress`, so the allow-from-router policy covers pod-network and host-network routers.

## Platform settings to fill in

- `SCORE_K8S_VERSION` pins score-k8s (0.22.0). To upgrade: change it, `make bin`, `make test`. `TestGolden` shows
  any change in the generated manifests, and `TestStockSilentlyDropsUnsupportedKind` fails if the upgrade fixes
  the silent-drop bug.
- `volume`: `$allowedClasses` (empty = any class allowed).
- `route`: `$networkPolicyDefault`. Set it to true only if namespaces are default-deny.
- `60-security-context` keeps `readOnlyRootFilesystem: true` from the community template. Many images (nginx, etc.)
  need a writable path, and the bundle's `volume` replaces the stock `emptyDir` one, so today only a PVC gives one
  (see `TODO.md`).
- `score-ocp` does not prune PVCs unless `SCORE_OCP_PRUNE_PVC=1` (pruning deletes data).

## Notes

- `--prune-allowlist` needs oc ≥ 4.13 (older: `--prune-whitelist`).
- Images must run as an arbitrary non-root UID with a read-only root filesystem (see `examples/quickstart/Dockerfile`).
