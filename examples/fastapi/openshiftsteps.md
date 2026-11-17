# Run the FastAPI example on OpenShift

Bundle the app with Pixi on a Linux build machine (your CI runner), and let an existing OpenShift cluster bake the
bundle into an image: generate an ImageStream, BuildConfig, Deployment, Service and Route from `score.yaml` with
stock score-k8s and this bundle, and reach the app over HTTPS. The same steps as score-spec-nomad's
[k8ssteps.md](https://github.com/leov2000/score-spec-nomad/blob/main/examples/fastapi/k8ssteps.md), with the cluster
building the image instead of Podman: no container tooling or registry login is needed on the build machine. The
bundle is a relocatable `.tar.gz` with everything in it, Python included, and the image is a small Debian plus that
bundle. Checked on 2026-11-16, every step as written: the Linux steps (0, 2 and the bundle in 8) on Debian 12 arm64,
and the `oc` steps against a single-node OpenShift 4.22 cluster as a non-admin user, with pixi 0.84.0, Python
3.13.18, oc 4.22 and score-k8s 0.22.0.

```
$WORK/
  bin/        pixi, score-k8s
  dl/         downloads
  pythons/    Python 3.13, which is copied into the bundle
  cache/      pixi, rattler and uv caches
  bundle/     a copy of score-spec-nomad's example: the bundle is built here
  app/        the build's input: score.yaml, the Dockerfile and the bundle, uploaded to the cluster as one folder
  gen/        score-k8s's project folder and the generated manifests
```

## What's already there

- A Linux build machine with the same CPU as the cluster's nodes (a bundle runs only on the OS and CPU it was built
  on), with `curl`, `python3`, Go, git and `oc` 4.13 or later.
- `oc` logged in as a normal user who can create a project.
- The cluster can pull `docker.io/library/debian` during the build (or mirror it, and change the `FROM` line).
- Checkouts of this repository and of [score-spec-nomad](https://github.com/leov2000/score-spec-nomad), where the
  app and its bundle scripts live.

## Set up the shell

Run every step in a shell with these set. `REPO` is your checkout of this repository, `NOMAD_REPO` your checkout of
score-spec-nomad.

```sh
export REPO=~/score-k8s-openshift
export NOMAD_REPO=~/score-spec-nomad
export WORK=~/fastapi-ocp                 # any folder; everything goes here
export NS=fastapi-hello                   # the OpenShift project to create

export PATH=$WORK/bin:$PATH
export PIXI_HOME=$WORK/pixi-home PIXI_CACHE_DIR=$WORK/cache/pixi RATTLER_CACHE_DIR=$WORK/cache/rattler UV_CACHE_DIR=$WORK/cache/uv
mkdir -p $WORK/bin $WORK/dl $WORK/pythons $WORK/cache $WORK/app
```

## 0. Get the tools, once

```sh
cd $WORK/dl
ARCH=$(uname -m)                          # x86_64 or aarch64

# Python: a python-build-standalone build (Astral), because it runs from any directory, so it can go inside the
# bundle. Distribution packages are tied to where they're installed.
PBS=cpython-3.13.18+20261026-$ARCH-unknown-linux-gnu-install_only.tar.gz
curl -sSfLO "https://github.com/astral-sh/python-build-standalone/releases/download/20261026/$PBS"
curl -sSfL https://github.com/astral-sh/python-build-standalone/releases/download/20261026/SHA256SUMS -o SHA256SUMS
grep " $PBS\$" SHA256SUMS | sha256sum -c -
tar -xzf "$PBS" -C $WORK/pythons && mv $WORK/pythons/python $WORK/pythons/3.13
$WORK/pythons/3.13/bin/python3.13 --version   # Python 3.13.18

# pixi
PIXI_TGZ=pixi-$ARCH-unknown-linux-musl.tar.gz
curl -sSfLO "https://github.com/prefix-dev/pixi/releases/download/v0.84.0/$PIXI_TGZ"
curl -sSfLO "https://github.com/prefix-dev/pixi/releases/download/v0.84.0/$PIXI_TGZ.sha256"
sha256sum -c "$PIXI_TGZ.sha256" && tar -xzf "$PIXI_TGZ" -C $WORK/bin pixi

# score-k8s, the version this repository is tested against ($REPO/SCORE_K8S_VERSION): make bin clones that tag and
# builds it. Or download the release binary and check it against checksums.txt.
make -C $REPO bin && cp $REPO/.bin/score-k8s $WORK/bin/
score-k8s --version | head -1             # score-k8s 0.22.0
```

## 1. Check the cluster

```sh
oc whoami                                 # a normal user
oc get nodes -o jsonpath='{.items[*].status.nodeInfo.architecture}{"\n"}' 2>/dev/null \
  || echo "no rights to list nodes: ask which CPU they have"   # must match this machine (amd64 = x86_64)
```

## 2. Bundle the app

The same as score-spec-nomad's k8ssteps.md step 2. Copy only the sources, so nothing is written in either repository:

```sh
rm -rf $WORK/bundle && mkdir -p $WORK/bundle
cp -R $NOMAD_REPO/examples/fastapi/{pyproject.toml,pixi.toml,src} $WORK/bundle/
cd $WORK/bundle

# a local conda channel with a copy of the Python in $WORK/pythons/3.13, for this machine (linux-64 or linux-aarch64)
python3 $NOMAD_REPO/scripts/pixi-kit/make-python-channel.py $WORK/pythons/3.13 conda-local

# pixi locks every platform in pixi.toml at once, so the others need a python package too; never installed here
M=$NOMAD_REPO/scripts/pixi-kit/make-symlink-python.py
for p in linux-64 linux-aarch64 osx-arm64; do
  [ -d conda-local/$p ] || python3 $M --platform $p --version 3.13.18 $WORK/pythons/3.13 conda-local
done

pixi install
pixi run bundle               # out/fastapi-hello-env.tar.gz: the app, its dependencies and Python
```

## 3. Put the build's input in one folder

The cluster builds from one uploaded folder: the bundle, the `Dockerfile` (score-spec-nomad's, unchanged) and
`score.yaml`, so `score-ocp` can find the folder from the Score file:

```sh
cp $WORK/bundle/out/fastapi-hello-env.tar.gz $WORK/app/
cp $REPO/examples/fastapi/{Dockerfile,score.yaml} $WORK/app/
ls $WORK/app                              # Dockerfile  fastapi-hello-env.tar.gz  score.yaml
```

## 4. Generate the manifests

score-k8s keeps every workload it has seen in its project folder, so start from an empty one. `init` copies this
repository's provisioners and patch templates into it; `SCORE_OCP_PROJECT` labels every object for step 5's prune.

```sh
rm -rf $WORK/gen && mkdir -p $WORK/gen && cd $WORK/gen
args=(--provisioners "$REPO/provisioners/openshift.provisioners.yaml")
for t in "$REPO"/patches/*.tpl; do args+=(--patch-templates "$t"); done
score-k8s init --no-sample "${args[@]}"
SCORE_OCP_PROJECT=fastapi-hello score-k8s generate -o manifests.yaml $WORK/app/score.yaml
grep '^kind:' manifests.yaml              # BuildConfig, Deployment, ImageStream, Route, Service
```

## 5. Deploy

```sh
oc new-project $NS
oc apply -n $NS -f $WORK/gen/manifests.yaml --prune -l app.kubernetes.io/part-of=fastapi-hello \
  --prune-allowlist core/v1/Service --prune-allowlist core/v1/ConfigMap \
  --prune-allowlist apps/v1/Deployment --prune-allowlist apps/v1/StatefulSet \
  --prune-allowlist route.openshift.io/v1/Route --prune-allowlist build.openshift.io/v1/BuildConfig \
  --prune-allowlist image.openshift.io/v1/ImageStream \
  --prune-allowlist autoscaling/v2/HorizontalPodAutoscaler --prune-allowlist policy/v1/PodDisruptionBudget \
  --prune-allowlist networking.k8s.io/v1/NetworkPolicy
```

The pod waits for its image (`ImagePullBackOff` on `fastapi-hello:latest`) until the first build in step 6.

## 6. Bake the bundle into an image, on the cluster

`--from-dir` uploads `$WORK/app` to the BuildConfig, which runs the `Dockerfile` on it: `ADD` extracts the bundle
into the Debian image, and the result is pushed as `fastapi-hello:latest`. The image trigger then rolls the
Deployment. `--wait` matters: `--follow` alone exits 0 when the build fails.

```sh
oc start-build fastapi-hello -n $NS --from-dir=$WORK/app --follow --wait   # STEP 2/8: ADD fastapi-hello-env.tar.gz /opt/app/env
oc rollout status deployment/fastapi-hello -n $NS --timeout=180s
```

## 7. Reach it

The Route has a host the router assigned, with edge TLS (`-k` for a cluster with a self-signed certificate):

```sh
HOST=$(oc get route fastapi-hello-route -n $NS -o jsonpath='{.spec.host}')
curl -sk -w '\n' https://$HOST/           # {"message":"Hello from fastapi-hello","platform":"kubernetes"}
curl -sk -w '\n' https://$HOST/healthz    # {"status":"ok"}
curl -s -o /dev/null -w '%{http_code}\n' http://$HOST/   # 302: HTTP redirects to HTTPS
```

Check that the pod runs the Python from the bundle, and that there's no other Python in the image:

```sh
oc exec -n $NS deploy/fastapi-hello -- ls -l /opt/app/env/bin/python3.13   # a real file
oc exec -n $NS deploy/fastapi-hello -- /opt/app/env/bin/python3.13 -c 'import sys; print(sys.prefix)'
                                          # /opt/app/env
oc exec -n $NS deploy/fastapi-hello -- sh -c 'command -v python3 || echo "no python3 on PATH"'
```

And that it runs as OpenShift requires: a random UID from the project's range, not `nobody` from the `Dockerfile`,
and a read-only root filesystem. The bundle's files are readable by any user, and it writes nothing:

```sh
oc exec -n $NS deploy/fastapi-hello -- id -u        # e.g. 1000700000
oc get project $NS -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.uid-range}{"\n"}'
oc exec -n $NS deploy/fastapi-hello -- touch /probe # touch: cannot touch '/probe': Read-only file system
```

## 8. After a code change

Bundle again and build again; the Deployment rolls by itself, with no re-apply:

```sh
cd $WORK/bundle
sed -i 's/{"status": "ok"}/{"status": "ok", "build": 2}/' src/fastapi_hello/main.py
pixi run bundle
cp out/fastapi-hello-env.tar.gz $WORK/app/
oc start-build fastapi-hello -n $NS --from-dir=$WORK/app --follow --wait
oc rollout status deployment/fastapi-hello -n $NS --timeout=180s
sleep 10                                  # the router takes a few seconds to stop sending to the old pod
curl -sk -w '\n' https://$HOST/healthz    # {"status":"ok","build":2}
```

For a change in your checkout of score-spec-nomad, repeat steps 2 and 3 instead. Re-run steps 4 and 5 only when
`score.yaml` changes.

## 9. Clean up

```sh
oc delete project $NS
rm -rf $WORK
```

## With score-ocp

`bin/score-ocp` runs steps 4 to 6 in one command: init in a fresh temporary folder, generate with the project label,
apply with prune, and the first build, from the folder holding `score.yaml`, when `fastapi-hello:latest` doesn't
exist yet. Later builds are step 8.

```sh
oc new-project $NS
$REPO/bin/score-ocp -p fastapi-hello -n $NS $WORK/app/score.yaml
```
