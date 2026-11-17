# FastAPI with Pixi, on OpenShift

The FastAPI example from [score-spec-nomad](https://github.com/leov2000/score-spec-nomad/tree/main/examples/fastapi),
deployed to OpenShift with stock score-k8s and this repository's provisioners and patches. It runs the same artifact
as score-spec-nomad's Kubernetes and Nomad guides: the **Pixi bundle**, a relocatable `.tar.gz` with the app, its
dependencies and Python, baked into a small Debian image by score-spec-nomad's `Dockerfile`, unchanged. The image
has no Python, pixi or pip of its own.

The difference is who builds the image: here the cluster does. The Score file is the same workload with two
resources added:

- **`openshift-build`**: an ImageStream and a BuildConfig. `oc start-build --from-dir` uploads a folder holding the
  bundle and the `Dockerfile`, the cluster builds the image, and each new build rolls the Deployment by itself. No
  Podman, registry or registry login is needed where the bundle is built.
- **`route`**: an HTTPS Route (edge TLS, HTTP redirected) to the Service, on a host the router assigns.

The app reports which platform it's on from variables the orchestrator sets. OpenShift sets
`KUBERNETES_SERVICE_HOST`, so it answers `"platform": "kubernetes"`.

```
score.yaml          the workload, plus the build and route resources
Dockerfile          score-spec-nomad's, unchanged: Debian + ADD fastapi-hello-env.tar.gz, run with the bundle's Python
openshiftsteps.md   step guide: bundle on a Linux build machine, then oc: deploy, build on the cluster, reach it
Makefile            the cluster steps with score-ocp, given BUNDLE=<the .tar.gz> (make help)
```

The app's source and the bundle scripts stay in score-spec-nomad; the guides build the bundle from your checkout of
it.

## The guide

[openshiftsteps.md](openshiftsteps.md) runs every step by hand, as a CI job would, in one work folder, and was run as
written: the bundle on a Linux build machine (your CI runner), then everything on the cluster through `oc`, logged in
as a normal user who can create a project.

## The bundle has to be built on Linux, on the nodes' CPU

A bundle runs only on the OS and CPU it was built on. OpenShift nodes run Linux, so build the bundle on Linux with
the nodes' CPU (usually amd64, or arm64 on arm64 nodes). A bundle built on a Mac is a macOS bundle, and fails in
the image. How score-spec-nomad builds it, and what Python it can carry:
[its README, "The bundle"](https://github.com/leov2000/score-spec-nomad/tree/main/examples/fastapi#the-bundle).

## Run it

The short version, from the repository root, with a Linux `fastapi-hello-env.tar.gz` and `oc` logged in as a
normal user:

```sh
oc new-project fastapi-hello
make -C examples/fastapi deploy BUNDLE=/path/to/fastapi-hello-env.tar.gz   # first build, rollout
curl -k "$(make -s -C examples/fastapi url)"
# {"message":"Hello from fastapi-hello","platform":"kubernetes"}
```

After a code change, bundle again, then `make -C examples/fastapi build BUNDLE=...`; the Deployment rolls when the
build finishes.

## How the Score file maps

| Score | OpenShift (score-k8s + this repository's bundle) |
|---|---|
| `metadata.name: fastapi-hello` | the name of every object; the Route is `fastapi-hello-route` |
| `containers.api.image: fastapi-hello:latest` | the ImageStreamTag the build pushes; an image trigger on the Deployment swaps in each new build |
| `variables` with `${metadata.name}` | `env` (`GREETING=Hello from fastapi-hello`, `PORT=8000`) |
| `readinessProbe`, `livenessProbe` | the same probes on the container |
| `requests`, `limits` | the same `resources` |
| `service.ports.http: {port: 8080, targetPort: 8000}` | Service port 8080 → container 8000 |
| `resources.build` (`openshift-build`) | ImageStream `fastapi-hello` + binary Docker BuildConfig `fastapi-hello`, built from the uploaded folder |
| `resources.route` (`route`, `port: http`) | Route to the Service's `http` port, edge TLS, host assigned by the router |
| (patches) | restricted security context, random UID, read-only root filesystem, no service account token, `part-of` label |

## Differences from score-spec-nomad's Kubernetes guide

- **The cluster builds the image.** Instead of `podman build` and `podman push`, `oc start-build --from-dir` uploads
  the bundle and the `Dockerfile`, and OpenShift runs the same `Dockerfile` on its own builder.
- **`image` names the build's output** (`fastapi-hello:latest`) instead of `.` and `--image`, so the image trigger
  can find the container.
- **The Route replaces port-forwarding.**
- **The pod runs under OpenShift's `restricted-v2` SCC:** a random UID from the project's range instead of the
  `Dockerfile`'s `USER nobody`, and a read-only root filesystem. The bundle works unchanged: its files are readable
  by any user, and it writes nothing at runtime.

## With make

The Makefile runs the cluster steps through `bin/score-ocp`, with the pinned score-k8s from `make -C ../.. bin`.
`NAMESPACE` and `PROJECT` default to `fastapi-hello`:

```sh
make generate                 # out/manifests.yaml, no cluster or bundle needed
make app BUNDLE=...           # out/app: score.yaml, the Dockerfile and the bundle, the folder the cluster builds from
make deploy BUNDLE=...        # apply with prune, first build, wait for the rollout
make build BUNDLE=...         # build again from a new bundle, wait for the rollout
make url                      # print the app's URL
```
