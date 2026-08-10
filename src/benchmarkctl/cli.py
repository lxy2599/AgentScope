from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Sequence

from .catalog import load_catalog
from .errors import CatalogError


def _write(payload: dict, output: str | None) -> None:
    text = json.dumps(payload, indent=2, sort_keys=True) + "\n"
    if output:
        destination = Path(output)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(text, encoding="utf-8")
    else:
        sys.stdout.write(text)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="benchmarkctl", description="AgentLab benchmark catalog tooling")
    commands = parser.add_subparsers(dest="command", required=True)

    validate = commands.add_parser("validate", help="validate all versioned catalog YAML")
    validate.add_argument("catalog", nargs="?", default=".", help="catalog root (default: current directory)")

    sync = commands.add_parser("catalog-sync", help="compile the catalog into a deterministic local manifest")
    sync.add_argument("catalog", nargs="?", default=".", help="catalog root (default: current directory)")
    sync.add_argument("--output", help="optional path for the compiled local catalog manifest")
    sync.add_argument("--server-url", help="AgentScope server URL for remote catalog sync")
    sync.add_argument("--project-id", help="AgentScope project ID for remote catalog sync")

    run = commands.add_parser("run", help="compile a run request for a future Worker")
    run.add_argument("catalog", nargs="?", default=".", help="catalog root (default: current directory)")
    run.add_argument("--template", required=True, help="BenchmarkRunTemplate id")
    run.add_argument("--output", help="optional JSON output path; stdout by default")
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    try:
        catalog = load_catalog(args.catalog)
        if args.command == "validate":
            print(f"validated catalog: {len(catalog.run_templates)} run template(s)")
            return 0
        if args.command == "catalog-sync":
            if bool(args.server_url) != bool(args.project_id):
                raise CatalogError("--server-url and --project-id must be used together")
            manifest = catalog.to_manifest()
            if args.output:
                _write(manifest, args.output)
                print(f"wrote catalog manifest: {args.output}", file=sys.stderr)
            if args.server_url:
                import os
                from benchmark_worker.platform import PlatformClient

                cookie = os.environ.get("AGENTSCOPE_SESSION_COOKIE", "")
                if not cookie:
                    raise CatalogError("AGENTSCOPE_SESSION_COOKIE is required for remote catalog sync")
                PlatformClient(args.server_url).sync_catalog(args.project_id, manifest, cookie)
                # Do not include the cookie or arbitrary server body in CLI output.
                print(f"synchronized catalog to project: {args.project_id}", file=sys.stderr)
            if not args.output and not args.server_url:
                raise CatalogError("catalog-sync requires --output and/or --server-url with --project-id")
            return 0
        if args.command == "run":
            request = {
                "schema_version": "v1",
                "kind": "BenchmarkRunRequest",
                "effective_run": catalog.compile(args.template).to_dict(),
                "execution": "not_submitted",
            }
            _write(request, args.output)
            return 0
    except CatalogError as exc:
        print(f"benchmarkctl: error: {exc}", file=sys.stderr)
        return 2
    raise AssertionError(f"unhandled command {args.command}")
