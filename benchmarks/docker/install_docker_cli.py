"""Install the pinned Docker client from a verified build asset.

The archive is acquired by AgentLab's host-side build-asset bootstrap, then
copied into the trusted Worker image. Benchmark tasks and agent sidecars never
download or install software at runtime.
"""

from __future__ import annotations

import hashlib
import io
import os
import tarfile
from pathlib import Path

archive = Path(os.environ["DOCKER_CLI_ARCHIVE"]).read_bytes()
expected_sha256 = os.environ["DOCKER_CLI_SHA256"]
actual_sha256 = hashlib.sha256(archive).hexdigest()
if actual_sha256 != expected_sha256:
    raise RuntimeError("Docker CLI archive SHA-256 mismatch")

with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as tar:
    member = tar.getmember("docker/docker")
    if not member.isfile():
        raise RuntimeError("Docker CLI archive has no regular docker binary")
    source = tar.extractfile(member)
    if source is None:
        raise RuntimeError("Docker CLI archive could not be extracted")
    destination = Path("/usr/local/bin/docker")
    destination.write_bytes(source.read())
    destination.chmod(0o755)
