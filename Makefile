.PHONY: test test-python server db-up db-down web-install web-test web-build test-all dev

test:
	go test ./...

test-python:
	cd python && python -m pytest

server:
	go run ./cmd/agentscope-server

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
