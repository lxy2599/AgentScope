.PHONY: test test-python server db-up db-down web-install web-test web-build test-all dev demo demo-real-agent demo-support-agent demo-multi-eval demo-generic-observability-agent demo-enterprise-agentops demo-trace-dataset-loop demo-experiment-comparison

test:
	go test ./...

test-python:
	cd python && python -m pytest

server:
	go run ./cmd/agentscope-server

demo-trace-dataset-loop:
	python3 python/examples/trace_dataset_loop_demo.py --base-url http://localhost:8080

demo-experiment-comparison:
	python3 python/examples/demo_experiment_comparison.py --server-url http://localhost:8080

db-up:
	docker compose up -d

db-down:
	docker compose down

web-install:
	cd web && npm install

web-test:
	cd web && npm test

web-build:
	cd web && npm run build

test-all: test web-test web-build

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
