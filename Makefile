.PHONY: test test-python server db-up db-down

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
