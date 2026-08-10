"""Install the pinned Docker Compose CLI plugin into the Worker image."""

from __future__ import annotations

import hashlib
import os
from pathlib import Path

source = Path(os.environ["DOCKER_COMPOSE_PLUGIN"])
expected = os.environ["DOCKER_COMPOSE_SHA256"]
actual = hashlib.sha256(source.read_bytes()).hexdigest()
if actual != expected:
    raise RuntimeError("Docker Compose plugin SHA-256 mismatch")
destination = Path("/usr/local/lib/docker/cli-plugins/docker-compose")
destination.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
destination.write_bytes(source.read_bytes())
destination.chmod(0o755)
