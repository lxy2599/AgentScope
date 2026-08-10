.PHONY: fmt-check go-mod-check vet go-staticcheck test test-race go-vuln test-python python-lint python-format-check python-audit shell-check server db-up db-down web-install web-lint web-test web-build web-audit openclaw-install openclaw-test openclaw-audit test-all dev demo demo-real-agent demo-support-agent demo-multi-eval demo-generic-observability-agent demo-enterprise-agentops demo-trace-dataset-loop demo-experiment-comparison demo-ingest-health demo-hermes-agentops setup-openhands-native demo-openhands-native-agentops demo-phase4-agentops phase4-load-smoke phase4-recovery-test

COMPOSE := docker compose -f docker-compose.yml -f docker-compose.phase4.yml

fmt-check:
	@files="$$(git ls-files -co --exclude-standard -- '*.go' | sort -u)"; \
	unformatted="$$(gofmt -l $$files)"; \
	test -z "$$unformatted" || { echo "Go files need gofmt:"; echo "$$unformatted"; exit 1; }

go-mod-check:
	go mod verify
	go mod tidy -diff

vet:
	go vet ./...

go-staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...

test:
	go test -count=1 ./...

test-race:
	go test -count=1 -race ./...

go-vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

test-python:
	cd python && uv run --frozen --extra dev pytest -q

python-lint:
	cd python && uv run --frozen --extra dev ruff check agentscope_sdk examples
	cd python && uv run --frozen --extra dev mypy agentscope_sdk

python-format-check:
	cd python && uv run --frozen --extra dev ruff format --check agentscope_sdk

python-audit:
	cd python && uv run --frozen --extra dev pip-audit --strict

shell-check:
	bash -n scripts/*.sh

server:
	go run ./cmd/agentscope-server

demo-trace-dataset-loop:
	python3 python/examples/trace_dataset_loop_demo.py --base-url http://localhost:8080

demo-experiment-comparison:
	python3 python/examples/demo_experiment_comparison.py --server-url http://localhost:8080

demo-ingest-health:
	python3 python/examples/demo_ingest_health.py --server-url http://localhost:8080

db-up:
	$(COMPOSE) up -d

db-down:
	$(COMPOSE) down

web-install:
	cd web && npm ci

web-lint:
	cd web && npm run lint

web-test:
	cd web && npm test -- --no-file-parallelism

web-build:
	cd web && npm run build

web-audit:
	cd web && npm audit --audit-level=moderate

openclaw-install:
	cd integrations/openclaw/agentscope-observability && npm ci

openclaw-test:
	cd integrations/openclaw/agentscope-observability && npm run typecheck && npm test

openclaw-audit:
	cd integrations/openclaw/agentscope-observability && npm audit --audit-level=moderate

test-all: fmt-check go-mod-check vet go-staticcheck test-race go-vuln test-python python-lint python-format-check python-audit shell-check web-lint web-test web-build web-audit openclaw-test openclaw-audit

dev:
	./scripts/dev.sh

demo:
	./scripts/demo.sh

demo-real-agent:
	./scripts/demo-real-agent.sh

demo-support-agent:
	./scripts/demo-support-agent.sh

demo-multi-eval:
	./scripts/demo-multi-eval.sh

demo-generic-observability-agent:
	./scripts/demo-generic-observability-agent.sh

demo-enterprise-agentops:
	./scripts/demo-enterprise-agentops.sh

demo-hermes-agentops:
	./scripts/demo-hermes-agentops.sh

setup-openhands-native:
	./scripts/setup-openhands-native.sh

demo-openhands-native-agentops:
	./scripts/demo-openhands-native-agentops.sh

demo-phase4-agentops:
	./scripts/demo-phase4-agentops.sh

phase4-load-smoke:
	./scripts/test-phase4-load.sh

phase4-recovery-test:
	./scripts/test-phase4-recovery.sh

# AgentLab monorepo lifecycle. These targets intentionally coexist with the
# existing AgentScope development targets above.
.PHONY: agentlab-bootstrap agentlab-build agentlab-platform-up agentlab-platform-down agentlab-agents-up agentlab-agents-down agentlab-up agentlab-down agentlab-status agentlab-bench
agentlab-bootstrap:
	./scripts/agentlab.sh bootstrap
agentlab-build:
	./scripts/agentlab.sh build
agentlab-platform-up:
	./scripts/agentlab.sh platform-up
agentlab-platform-down:
	./scripts/agentlab.sh platform-down
agentlab-agents-up:
	./scripts/agentlab.sh agents-up
agentlab-agents-down:
	./scripts/agentlab.sh agents-down
agentlab-up:
	./scripts/agentlab.sh up
agentlab-down:
	./scripts/agentlab.sh down
agentlab-status:
	./scripts/agentlab.sh status
agentlab-bench:
	@test -n "$(RUN)" || (echo "Usage: make agentlab-bench RUN=<template-id>" >&2; exit 2)
	$(MAKE) -C benchmarks run RUN="$(RUN)"
