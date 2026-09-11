# Every version comes from versions.env; nothing here floats.
include versions.env
export

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BIN := bin
CLUSTER := $(BIN)/cluster
KIND_CLUSTER ?= sixfields
PROFILE ?= dev
FIXTURES := testdata/fixtures

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --- build ---

.PHONY: build
build: sync-embeds ## Build the cluster CLI into bin/
	@mkdir -p $(BIN)
	go build -trimpath -ldflags "-X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o $(CLUSTER) ./cmd/cluster

## --- test ---

.PHONY: test
test: ## Unit + pure-core + policy CEL tests
	hack/timebox.sh $(TEST_BUDGET_S) go test ./...

.PHONY: test-envtest
test-envtest: envtest ## API tests against a real apiserver
	KUBEBUILDER_ASSETS="$$(hack/envtest-assets.sh)" hack/timebox.sh $(TEST_ENVTEST_BUDGET_S) go test -tags envtest ./...

.PHONY: e2e
e2e: ## CAPD end-to-end against a live management cluster
	hack/timebox.sh $(E2E_BUDGET_S) go test -tags e2e -timeout $(E2E_BUDGET_S)s ./e2e/...

.PHONY: test-llm
test-llm: ## Cassette-replayed and live model evals. Never part of `make test`.
	go test -tags llm ./internal/explain/...

.PHONY: watch
watch: ## Sub-second loop over the pure packages
	gotestsum --watch -- ./internal/fold/... ./internal/why/... ./internal/eta/... ./internal/gen/... ./internal/msg/... ./internal/render/...

.PHONY: cover
cover: ## Coverage over the pure core
	go test -coverprofile=$(BIN)/cover.out ./internal/... && go tool cover -func=$(BIN)/cover.out | tail -1

## --- lint ---

.PHONY: lint
lint: lint-go lint-yaml ## golangci-lint + yamllint + kubeconform

.PHONY: lint-go
lint-go:
	golangci-lint run ./...
	cd hack/tools && go vet ./...

.PHONY: lint-yaml
lint-yaml:
	yamllint assembly policy hack/kind-config.yaml
	hack/kubeconform.sh

## --- dev loop ---

.PHONY: bootstrap
bootstrap: ## Install every pinned tool (Homebrew)
	hack/bootstrap.sh

.PHONY: doctor
doctor: ## Verify the machine: tools, arch, runtime socket, memory profile
	hack/doctor.sh

.PHONY: doctor-ai
doctor-ai: ## Memory math, local model recommendation, tokens/s check
	hack/doctor-ai.sh

.PHONY: profile
profile: ## Resize the container VM for a profile: make profile P=dev|e2e|bench
	hack/profile.sh "$(P)"

.PHONY: dev-up
dev-up: ## kind management cluster + clusterctl init + the assembly (idempotent)
	hack/dev-up.sh

.PHONY: dev-down
dev-down: ## Delete the management cluster and every workload cluster it created
	hack/dev-down.sh

.PHONY: policy-install
policy-install: ## Apply the admission policy to the management cluster
	kubectl apply -f policy/vap/

## --- assembly ---

.PHONY: render
render: ## Render the ClusterClass overlays to one file per overlay
	hack/render.sh

.PHONY: class-plan
class-plan: ## clusterctl alpha topology plan against the rendered class
	hack/class-plan.sh

.PHONY: sync-embeds
sync-embeds: ## Copy the runbooks and skills into the packages that embed them
	rsync -a --delete docs/runbooks/ internal/msg/runbooks/
	rsync -a --delete skills/ cmd/cluster/skills/

.PHONY: api-snapshot
api-snapshot: ## Regenerate docs/api-snapshot.{md,json} from the pinned API modules
	cd hack/tools && go run ./apisnapshot -root ../..

## --- fixtures ---

.PHONY: record-fixture
record-fixture: build ## NAME=<scenario> CLUSTER_NAME=<name> record a snapshot series
	$(CLUSTER) fixture record --name "$(NAME)" --cluster "$(CLUSTER_NAME)" --out $(FIXTURES)

.PHONY: replay
replay: build ## F=<fixture dir> replay a recorded timeline into the renderer
	$(CLUSTER) status --replay $(FIXTURES)/$(F) --speed 20

.PHONY: golden
golden: ## Regenerate every golden file (review the diff)
	UPDATE_GOLDEN=1 go test ./...

## --- bench ---

.PHONY: bench
bench: ## cluster-bench, sequential, per backend. Requires PROFILE=bench.
	hack/bench.sh

## --- envtest assets ---

.PHONY: envtest
envtest:
	@hack/envtest-assets.sh >/dev/null

.PHONY: paper
paper: ## Compile the arXiv paper and rebuild its submission zip
	cd docs/paper && pdflatex -interaction=nonstopmode sixfields.tex >/dev/null \
	  && pdflatex -interaction=nonstopmode sixfields.tex >/dev/null \
	  && rm -f arxiv-sixfields.zip && zip -q arxiv-sixfields.zip sixfields.tex
	@echo "docs/paper/sixfields.pdf  $$(cd docs/paper && pdfinfo sixfields.pdf | awk '/Pages/{print $$2}') pages"
	@echo "docs/paper/arxiv-sixfields.zip"

.PHONY: clean
clean:
	rm -rf $(BIN)
