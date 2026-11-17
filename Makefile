# Bundle: score-k8s OpenShift provisioners + patch templates.
SCORE_K8S_REPO  ?= https://github.com/score-spec/score-k8s
SCORE_NOMAD_SRC ?= ../score-spec-nomad
PIN := $(shell tr -d '[:space:]' < SCORE_K8S_VERSION)
VERSION_PKG := github.com/score-spec/score-k8s/internal/version

.PHONY: bin test golden check-version verify-live

## Build the pinned score-k8s release (tag $(PIN) of $(SCORE_K8S_REPO)) into .bin/, the same way upstream's release
## does: the version is only set through -ldflags, so `go install` would report 0.0.0 and fail check-version.
## Alternatively, put the official release binary for your platform on PATH (github.com/score-spec/score-k8s/releases).
## Air-gapped: point SCORE_K8S_REPO at the internal git mirror, or use the release binaries mirrored in Artifactory.
## score-nomad (for the Nomad parity tests) is built from a local checkout when one exists.
bin:
	rm -rf .bin/src/score-k8s && mkdir -p .bin/src
	git -c advice.detachedHead=false clone -q --depth 1 --branch $(PIN) $(SCORE_K8S_REPO) .bin/src/score-k8s
	cd .bin/src/score-k8s && go build -ldflags "-X $(VERSION_PKG).Version=$(PIN) -X $(VERSION_PKG).GitCommit=$$(git rev-parse --short HEAD)" -o $(CURDIR)/.bin/score-k8s ./cmd/score-k8s
	-cd $(SCORE_NOMAD_SRC) && go build -o $(CURDIR)/.bin/score-nomad ./cmd/score-nomad

## Offline tests (no cluster). Uses $$SCORE_K8S_BIN, else .bin/score-k8s, else score-k8s on PATH.
test:
	cd test && go vet ./... && go test ./... -count=1

## Deploy the examples to the cluster you are logged in to (`oc login`, normal user) and check them there.
## EXAMPLES="route hpa" limits the run; KEEP=1 keeps the ex-<example> projects; OC=path/to/oc.
verify-live:
	SCORE_K8S=$${SCORE_K8S_BIN:-$(if $(wildcard .bin/score-k8s),$(CURDIR)/.bin/score-k8s,score-k8s)} examples/verify-live.sh $(EXAMPLES)

## Rewrite fixtures/full/manifests.golden.yaml after an intended output change.
golden:
	cd test && go test ./... -count=1 -run TestGolden -update

check-version:
	$${SCORE_K8S_BIN:-score-k8s} check-version "=$(PIN)"
