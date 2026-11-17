# Deploy the FastAPI example to OpenShift with this bundle. Run `make help` for the targets.
# BUNDLE is the Linux Pixi bundle built with score-spec-nomad (fastapi-hello-env.tar.gz, for the nodes' CPU): see
# openshiftsteps.md. Needs `oc login` as a user who can use NAMESPACE, and the pinned score-k8s
# (`make -C ../.. bin`). The build's input goes to $(OUT)/app; the image is built on the cluster from it.

BUNDLE    ?=
NAMESPACE ?= fastapi-hello
PROJECT   ?= fastapi-hello
OUT       ?= out
OC        ?= oc
ROOT      := $(abspath $(CURDIR)/../..)
SCORE_K8S ?= $(if $(wildcard $(ROOT)/.bin/score-k8s),$(ROOT)/.bin/score-k8s,score-k8s)
SCORE_OCP := SCORE_K8S=$(SCORE_K8S) OC=$(OC) $(ROOT)/bin/score-ocp -p $(PROJECT) -n $(NAMESPACE)
APP       := $(abspath $(OUT))/app

.PHONY: help app generate dry-run deploy build url clean

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

app: ## Put score.yaml, the Dockerfile and $(BUNDLE) in $(OUT)/app, the folder the cluster builds from
	@test -f "$(BUNDLE)" || { echo "set BUNDLE to the Linux fastapi-hello-env.tar.gz (openshiftsteps.md step 2)"; exit 1; }
	mkdir -p $(APP)
	cp $(CURDIR)/score.yaml $(CURDIR)/Dockerfile $(APP)/
	cp $(BUNDLE) $(APP)/fastapi-hello-env.tar.gz

generate: ## Generate $(OUT)/manifests.yaml (no cluster or bundle needed)
	mkdir -p $(OUT)
	$(SCORE_OCP) --generate-only -o $(OUT)/manifests.yaml $(CURDIR)/score.yaml

dry-run: ## Server-side dry run of the apply
	mkdir -p $(OUT)
	$(SCORE_OCP) --dry-run -o $(OUT)/manifests.yaml $(CURDIR)/score.yaml

deploy: app ## Apply with prune; the first time, also build the image from $(OUT)/app and roll out
	$(SCORE_OCP) -o $(OUT)/manifests.yaml $(APP)/score.yaml
	$(OC) -n $(NAMESPACE) rollout status deployment/fastapi-hello --timeout=180s

build: app ## Build the image again from a new $(BUNDLE); the image trigger rolls the Deployment
	$(OC) -n $(NAMESPACE) start-build fastapi-hello --from-dir=$(APP) --follow --wait
	$(OC) -n $(NAMESPACE) rollout status deployment/fastapi-hello --timeout=180s

url: ## Print the app's URL
	@echo "https://$$($(OC) -n $(NAMESPACE) get route fastapi-hello-route -o jsonpath='{.spec.host}')/"

clean: ## Remove generated files
	rm -rf $(OUT)
