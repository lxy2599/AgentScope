.PHONY: test server db-up db-down

test:
	go test ./...

server:
	go run ./cmd/agentscope-server

db-up:
	docker compose up -d

db-down:
	docker compose down
