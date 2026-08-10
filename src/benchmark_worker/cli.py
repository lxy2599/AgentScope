from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path
from typing import Sequence

from .harbor_executor import HarborJobExecutor, HarborRuntimeOptions
from .platform import HTTPPlatformWorkerBackend, PlatformClient, PlatformError


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="benchmark-worker",
        description="AgentLab Harbor benchmark Worker: durable queue claim, pinned agent sidecar, official Harbor verifier.",
    )
    parser.add_argument("--server-url", required=True, help="AgentScope server URL")
    parser.add_argument("--worker-id", required=True, help="stable worker name registered with AgentScope")
    parser.add_argument("--poll-interval-s", type=float, default=10.0, help="seconds between no-job polls (default: 10)")
    parser.add_argument("--heartbeat-interval-s", type=float, default=20.0, help="seconds between independent job lease heartbeats (default: 20)")
    parser.add_argument("--work-root", type=Path, default=Path(os.environ.get("AGENTLAB_BENCHMARK_WORK_ROOT", ".agentlab/benchmark-worker")), help="per-job private work directory")
    parser.add_argument("--harbor-bin", default=os.environ.get("HARBOR_BIN", "harbor"), help="Harbor CLI executable (default: HARBOR_BIN or harbor)")
    parser.add_argument("--keep-workspaces", action="store_true", help="retain failed/succeeded job directories for local debugging")
    parser.add_argument("--once", action="store_true", help="register and process at most one claimed Harbor job, then exit")
    parser.add_argument("--dry-run", action="store_true", help="validate local configuration only; do not send HTTP requests")
    return parser


def _project_token() -> str:
    token = os.environ.get("AGENTSCOPE_PROJECT_TOKEN", "")
    if not token:
        raise ValueError("AGENTSCOPE_PROJECT_TOKEN must be provided through the runtime environment")
    return token


def _catalog_root() -> Path | None:
    """Resolve the host-visible benchmark catalog mount, if configured.

    Harbor may launch Docker environments that need to read a local task fixture.
    The catalog path therefore has to be both absolute and mounted at the same
    absolute path in the trusted Worker container; a sidecar never receives it.
    """

    raw = os.environ.get("AGENTLAB_BENCHMARK_CATALOG_ROOT", "").strip()
    if not raw:
        return None
    root = Path(raw).expanduser().resolve()
    if not root.is_absolute() or not root.is_dir():
        raise ValueError("AGENTLAB_BENCHMARK_CATALOG_ROOT must be an existing absolute directory")
    return root


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    if args.poll_interval_s <= 0:
        _parser().error("--poll-interval-s must be positive")
    if args.heartbeat_interval_s <= 0:
        _parser().error("--heartbeat-interval-s must be positive")
    try:
        project_token = _project_token()
        catalog_root = _catalog_root()
        if args.dry_run:
            # Do not echo runtime values. This mode is safe for shell/CI validation.
            print(f"benchmark-worker dry-run ready: worker={args.worker_id} execution_kind=harbor")
            return 0

        client = PlatformClient(args.server_url)
        client.register_benchmark_worker(project_token, args.worker_id)
        backend = HTTPPlatformWorkerBackend(client, project_token)
        worker = HarborJobExecutor(
            args.worker_id,
            backend,
            options=HarborRuntimeOptions(
                work_root=args.work_root,
                harbor_binary=args.harbor_bin,
                catalog_root=catalog_root,
                artifact_root=args.work_root / "artifacts",
                cleanup_workspaces=not args.keep_workspaces,
            ),
            heartbeat_interval_s=args.heartbeat_interval_s,
        )
        print(f"benchmark-worker registered: worker={args.worker_id} execution_kind=harbor")
        while True:
            processed = worker.process_once()
            if args.once:
                return 0
            if not processed:
                time.sleep(args.poll_interval_s)
    except (PlatformError, ValueError) as exc:
        # PlatformError has a redacted message and never keeps request headers.
        print(f"benchmark-worker error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":  # pragma: no cover - console-script entrypoint.
    raise SystemExit(main())
