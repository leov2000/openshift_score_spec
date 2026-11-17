# TODO

## Before anyone else uses the bundle

- [ ] **Make the templates reachable by URL.** The README says `score-k8s init` can load the provisioners and
      patches from `https://` URLs, but the repo is private (the raw GitHub URL returns 404). Make it public, or
      mirror the files internally (e.g. Artifactory for the air-gapped setup), and tag a release to pin the URLs to.

## Verification that needs a different cluster

- [ ] **HPA scale-up on CPU.** Needs a metrics API (cluster monitoring, which a small test cluster may have turned off)
      (about 14 GB of VM memory), then `make verify-live EXAMPLES=hpa`; the SKIP turns into a real check.
- [ ] **NetworkPolicy, router side.** "Unreachable through the Route without the policy" can't be shown on one
      node: the host-network router on the same node is never subject to policy. Needs a multi-node cluster.
- [ ] **cert-manager annotations on Routes.** They are copied (checked), but nothing acts on them without
      cert-manager and its openshift-routes integration.

## Decisions

- [ ] **Scope of `score-ocp`.** It generates, applies with prune, and runs only the *first* build. Later builds
      and rollout checks belong to the delivery pipeline. Either narrow `score-ocp` to generate (+ apply/prune,
      which depends on the bundle's labels) and show builds in a CI example, or keep it and document that it
      builds once.
- [ ] **"Behaviour you might not expect" section in the README**, stating the defaults the bundle imposes and
      the way around each (next section).

## Known limits (documented, not planned; revisit if users hit them)

Defaults the templates impose on top of stock score-k8s:

- No writable path besides a PVC: `60-security-context` makes the root filesystem read-only, and the bundle's
  `volume` replaces the stock `emptyDir` provisioner. Images that write to `/tmp` (nginx, …) crash-loop.
- The service account token is never mounted (`automountServiceAccountToken: false`, no opt-out). Apps that call
  the Kubernetes API (leader election, Vault Kubernetes auth, operators) fail at runtime.
- `networkPolicy: true` admits only the router, so other workloads in the namespace can't reach that workload
  either; in default-deny namespaces nothing allows workload-to-workload traffic.
- Routes are edge TLS only (no re-encrypt or passthrough).

Behaviour of score-k8s / Kubernetes that surprises on later deploys:

- Changing a mounted file's content doesn't restart pods: score-k8s names the ConfigMap after the target path,
  not the content, so it is updated in place.
- Rotating a Secret doesn't reach running pods (env vars are read at start): `oc rollout restart`.
- StatefulSet volumes can't be resized or renamed after the first deploy (`volumeClaimTemplates` is immutable;
  the claim name is a hash of the resource id). Resize the PVC directly, or delete the StatefulSet with
  `--cascade=orphan` and re-apply.

## Wrapper polish

- [ ] Pass `--quiet` to score-k8s (every run prints dozens of `INFO` lines) and print the Route URL at the end.
- [ ] Surface the provisioner's own message: today it is buried at the end of
      `failed to provision resources: … error calling fail: <message>`.
- [ ] **Find the bundle from anywhere.** `score-ocp` looks for `provisioners/`, `patches/` and `SCORE_K8S_VERSION`
      one level above its own path, without resolving symlinks. Linked onto the PATH
      (`ln -s …/bin/score-ocp /usr/local/bin/`) it fails with `…/SCORE_K8S_VERSION: No such file or directory`.
      Resolve the link first, and accept `SCORE_OCP_BUNDLE=/opt/score-openshift` (e.g. in a CI image).
- [ ] Teardown: `score-ocp --delete` (today: `oc delete` every kind with `-l app.kubernetes.io/part-of=<project>`).
- [ ] A CI pipeline example (Tekton, or GitLab / GitHub Actions) for the stateless-CI use case.
