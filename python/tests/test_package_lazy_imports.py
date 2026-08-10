"""Regression tests for the lightweight AgentScope SDK package import."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

_PACKAGE_ROOT = Path(__file__).resolve().parents[1]


def _run_isolated_python(script: str) -> subprocess.CompletedProcess[str]:
    """Run an import probe without test-suite module cache contamination."""
    environment = os.environ.copy()
    existing_pythonpath = environment.get("PYTHONPATH", "")
    environment["PYTHONPATH"] = os.pathsep.join(
        part for part in (str(_PACKAGE_ROOT), existing_pythonpath) if part
    )
    return subprocess.run(
        [sys.executable, "-c", script],
        cwd=_PACKAGE_ROOT,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )


def test_runtime_root_import_is_lightweight_and_never_connects() -> None:
    """Presence users can import lifecycle helpers without LLM/benchmark imports."""
    probe = r'''
import builtins
import socket
import sys

heavy_sdk_modules = {
    "agentscope_sdk.evaluators",
    "agentscope_sdk.executors",
    "agentscope_sdk.harbor",
    "agentscope_sdk.judges",
    "agentscope_sdk.llm",
    "agentscope_sdk.runtime_trace",
    "agentscope_sdk.snapshots",
    "agentscope_sdk.tau2",
    "agentscope_sdk.tracing",
}

original_import = builtins.__import__
def forbid_litellm(name, globals=None, locals=None, fromlist=(), level=0):
    if name == "litellm" or name.startswith("litellm."):
        raise AssertionError("the lightweight lifecycle import must not import litellm")
    return original_import(name, globals, locals, fromlist, level)

network_attempts = []
def forbid_connect(self, address):
    network_attempts.append(address)
    raise AssertionError("the lightweight lifecycle import must not open a network connection")

builtins.__import__ = forbid_litellm
socket.socket.connect = forbid_connect

from agentscope_sdk import get_runtime, init

assert callable(init)
assert get_runtime() is None
assert not heavy_sdk_modules.intersection(sys.modules), sorted(heavy_sdk_modules.intersection(sys.modules))
assert not network_attempts, network_attempts
'''
    result = _run_isolated_python(probe)
    assert result.returncode == 0, result.stderr or result.stdout


def test_historical_root_exports_still_resolve_lazily() -> None:
    """Representative eager exports retain their old root-level import form."""
    probe = r'''
import sys

import agentscope_sdk as sdk

assert "HarborExecutor" not in sdk.__dict__
assert "configure_tracing" not in sdk.__dict__

from agentscope_sdk import HarborExecutor, configure_tracing, target
from agentscope_sdk.harbor import HarborExecutor as DirectHarborExecutor
from agentscope_sdk.target import target as direct_target

assert HarborExecutor is DirectHarborExecutor
assert sdk.HarborExecutor is HarborExecutor
assert callable(configure_tracing)
assert target is direct_target
assert callable(sdk.target)
assert "agentscope_sdk.harbor" in sys.modules
assert "agentscope_sdk.tracing" in sys.modules

star_exports = {}
exec("from agentscope_sdk import *", star_exports)
assert set(sdk.__all__).issubset(star_exports)
assert star_exports["target"] is direct_target
assert star_exports["HarborExecutor"] is DirectHarborExecutor

try:
    sdk.not_a_public_sdk_export
except AttributeError:
    pass
else:
    raise AssertionError("unknown public attributes must still fail normally")
'''
    result = _run_isolated_python(probe)
    assert result.returncode == 0, result.stderr or result.stdout
