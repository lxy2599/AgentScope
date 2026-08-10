.PHONY: install test validate catalog-sync run api-e2e build

install:
	uv sync --extra test

test:
	uv run pytest

validate:
	uv run benchmarkctl validate .

catalog-sync:
	mkdir -p .benchmarkctl
	uv run benchmarkctl catalog-sync . --output .benchmarkctl/catalog.json

run:
	@test -n "$(RUN)" || (echo "Usage: make run RUN=<template-id>" >&2; exit 2)
	uv run benchmarkctl run . --template "$(RUN)"

build:
	@echo "The benchmark Worker image is added with the Harbor sidecar implementation."

api-e2e:
	PYTHONPATH=src python3 scripts/api_e2e.py --server-url "$${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:18080}"
