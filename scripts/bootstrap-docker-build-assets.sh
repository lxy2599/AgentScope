#!/usr/bin/env bash
# Stage the owned AgentScope SDK source for a reproducible Hermes image build.
# The source is copied during image construction; runtime containers neither
# mount the AgentScope repository nor install packages.
set -euo pipefail

readonly ASSET_DIR="${ASSET_DIR:-.docker-build-assets}"
readonly SDK_SOURCE="${AGENTSCOPE_SDK_SOURCE:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../agentscope/python" && pwd)}"
readonly SDK_ASSET_DIR="${ASSET_DIR}/agentscope-python"
readonly MANIFEST="${ASSET_DIR}/agentscope-python.sha256"

if [[ ! -f "${SDK_SOURCE}/pyproject.toml" ]] || [[ ! -f "${SDK_SOURCE}/agentscope_sdk/__init__.py" ]]; then
  echo "Missing AgentScope SDK source at ${SDK_SOURCE}" >&2
  exit 1
fi

python3 - "${SDK_SOURCE}" "${SDK_ASSET_DIR}" <<'PY'
from __future__ import annotations

import shutil
import sys
from pathlib import Path

source = Path(sys.argv[1]).resolve()
target = Path(sys.argv[2])
if target.exists():
    shutil.rmtree(target)
shutil.copytree(
    source,
    target,
    # Exclude every local environment/cache directory. Docker's context
    # ignore rules exclude these paths too; copying them into the manifest
    # would make integrity verification fail inside the image.
    ignore=shutil.ignore_patterns(
        ".git",
        ".venv",
        ".ruff_cache",
        ".pytest_cache",
        ".mypy_cache",
        ".coverage",
        "htmlcov",
        "build",
        "dist",
        "*.egg-info",
        "__pycache__",
        "*.pyc",
        "*.pyo",
    ),
)
PY
(
  cd "${SDK_ASSET_DIR}"
  find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
) > "${MANIFEST}"

echo "Staged AgentScope SDK build asset at ${SDK_ASSET_DIR}" >&2
