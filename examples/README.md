# Examples

Each folder deploys with one `score-ocp` command into its own project, on any OpenShift 4.13+ cluster, as a
normal (non-admin) user. No hosts are hardcoded: Routes get `<route>-<project>.<apps domain>` from the router.
Each `score.yaml` starts with the exact commands to run it.

```sh
oc new-project quickstart
oc create secret generic web-secrets --from-literal=GREETING='hello from a Secret'
bin/score-ocp -p quickstart examples/quickstart/score.yaml      # builds, deploys, exposes
curl -k https://$(oc get route web-route -o jsonpath='{.spec.host}')/
```

| example | shows | needs first |
|---|---|---|
| [`quickstart/`](quickstart/score.yaml) | build from a Dockerfile on the cluster, Route, Secret, in one command; new builds roll out by themselves | Secret `web-secrets` |
| [`fastapi/`](fastapi/README.md) | score-spec-nomad's FastAPI app as its relocatable Pixi bundle, baked into a Debian image (no Python installed) by the cluster, with a Service and Route; step guide (Linux build machine, then `oc`) run as written | a Linux Pixi bundle, built in the guides |
| [`route/`](route/web.score.yaml) | Route on a sub-path (`/api`), allow-from-router NetworkPolicy; a second workload to show pod-to-pod is blocked | – |
| [`route-annotations/`](route-annotations/score.yaml) | router timeout and cert-manager annotations on the workload, copied onto the Route | – |
| [`build/`](build/score.yaml) | every `openshift-build` option: build secrets (default and custom mount path), build resources, annotations, extra ImageStreams | Secrets `npmrc`, `certs` |
| [`secrets/`](secrets/score.yaml) | two Secrets injected into two containers as env vars | Secrets `app-config`, `db-creds` |
| [`statefulset/`](statefulset/score.yaml) | StatefulSet with a per-replica PVC | – |
| [`shared-volume/`](shared-volume/score.yaml) | Deployment with one shared PVC | – |
| [`hpa/`](hpa/score.yaml) | CPU HPA plus PodDisruptionBudget | metrics API, to actually scale |
| [`multi-workload/`](multi-workload/web.score.yaml) | two workloads deployed together, `web` finding `cache` through `service-port` | Secret `cache-secrets` |
| [`failures/`](failures/) | Score files the bundle rejects at generate time, each with its `# expect:` error | – |

Only `quickstart/`, `fastapi/` and `build/` build an image; `fastapi/` needs a Linux Pixi bundle, so `make verify-live` skips it unless `FASTAPI_BUNDLE` points at one. The others run [`app/app.py`](app/app.py) (Python standard
library, any UID, read-only root filesystem) in the stock `registry.access.redhat.com/ubi9/python-312` image,
mounted with Score `files:` from `../app/app.py`, so they deploy in seconds. `quickstart/app.py` is a copy that
the build bakes in (`oc start-build` uploads only that folder); a test keeps the two identical.

## Checking them

- `make test` (offline): every example generates and strict-decodes, has no hardcoded Route host, and every
  `failures/` file fails with its `# expect:` message (`test/examples_test.go`).
- `make verify-live` (cluster): [`verify-live.sh`](verify-live.sh) deploys each example into a fresh project
  `ex-<example>`, checks it on the cluster, and deletes the project. `EXAMPLES="route hpa"` runs a subset,
  `KEEP=1` keeps the projects, `OC=` points at a specific `oc`.

What `verify-live.sh` checks, by feature and patch template:

| feature / template | live check | example |
|---|---|---|
| Route | HTTPS 200 on the assigned host; HTTP → 302; path outside `/api` → 503 | quickstart, route |
| Build | first build from `score-ocp`; both build secrets visible during `RUN`, absent from the running image; extra ImageStream imported; annotations and build resources on the BuildConfig; a failing first build fails `score-ocp` | build |
| Image trigger, `20-image-trigger` | a second `oc start-build` rolls the Deployment with no re-apply; re-deploy causes no rollout | quickstart |
| Route annotations, `30-route-annotations` | `timeout: 45s` lets a 35 s request return 200; without it the router returns 504 at 30 s; cert-manager annotations copied, other prefixes not | route-annotations |
| Secrets, `40-secret-envfrom` | missing Secret → pod waits with `secret "db-creds" not found`; created → pod starts; keys in both containers; no Secret in the manifests | secrets |
| Volumes | volumeClaimTemplate PVC bound, SCC sets fsGroup, data survives pod deletion; Deployment PVC kept on prune unless `SCORE_OCP_PRUNE_PVC=1` | statefulset, shared-volume |
| `05-workload-guard` | a `k8s.score.dev/kind` typo is rejected before apply, and the running StatefulSet is not pruned | statefulset |
| HPA / PDB, `50-hpa-guard` | HPA scales to `minReplicas`, finds its target, PDB allows 1 disruption; scale-up on CPU only if a metrics API exists (else SKIP) | hpa |
| Security context, `60-security-context` | pod admitted under `restricted-v2`, UID from the namespace range, root filesystem read-only, no service account token | quickstart, statefulset |
| NetworkPolicy | policy present; client → web blocked, web → client open (no policy on client) | route |
| Wrapper | empty project → running in one command; server-side dry run creates nothing; removed resource and removed workload pruned | quickstart, route, multi-workload |
| `00-stable-instance` | selector uses the bare workload name; re-deploy accepted | quickstart |
| `90-project-label` | every generated object carries `part-of=<project>` on the cluster | quickstart, multi-workload |
| `99-sort` | same manifests whatever the order of the Score files | multi-workload |
| End-to-end | two workloads, `service-port` wiring, StatefulSet + Secret + Route together | multi-workload |
