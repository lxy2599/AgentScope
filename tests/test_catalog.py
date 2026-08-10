from __future__ import annotations

from pathlib import Path

import pytest

from benchmark_worker.platform import catalog_sync_payload
from benchmarkctl.catalog import load_catalog
from benchmarkctl.errors import CatalogError

ROOT = Path(__file__).resolve().parents[1]
HARBOR_CONFIG = {
    "version": "0.20.0",
    "dataset": "terminal-bench@2.0",
    "agent_import_path": "benchmark_worker.harbor_agent:TaskRunnerAgent",
    "jobs_root": "harbor-jobs",
    "job_name": "terminal-bench-smoke",
}
EXECUTION_CONFIG = {
    "concurrency": 1,
    "task_timeout_s": 900,
    "max_retries": 0,
    "fail_fast": True,
    "artifact_policy": "all",
    "harbor": HARBOR_CONFIG,
}


def copy_catalog(tmp_path: Path) -> Path:
    import shutil

    for name in ("suites", "artifacts", "model-profiles", "agent-profiles", "execution-profiles", "run-templates"):
        shutil.copytree(ROOT / name, tmp_path / name)
    return tmp_path


def harbor_profile(root: Path) -> Path:
    return root / "execution-profiles" / "harbor-smoke.yaml"


def test_smoke_catalog_compiles_effective_config() -> None:
    catalog = load_catalog(ROOT)
    run = catalog.compile("openhands-terminal-bench-smoke")

    assert run.suite.harness == "harbor"
    assert run.agent_artifact.image_digest.startswith("sha256:")
    assert run.execution == EXECUTION_CONFIG
    assert run.task_selection == {"mode": "ids", "include": ["smoke-task-001"]}

    hermes = catalog.compile("hermes-terminal-bench-smoke")
    assert hermes.model_profile.id == "hermes-ark-deepseek-v4-flash"
    assert hermes.model_profile.model == "deepseek-v4-flash-ga-260731"


def test_harbor_execution_is_preserved_in_manifest_and_catalog_sync_payload() -> None:
    manifest = load_catalog(ROOT).to_manifest()
    execution_profile = next(
        profile for profile in manifest["objects"]["execution_profiles"] if profile["id"] == "harbor-smoke"
    )

    assert execution_profile == {"id": "harbor-smoke", "execution": EXECUTION_CONFIG}

    sync_payload = catalog_sync_payload(manifest)
    synced_profile = next(profile for profile in sync_payload["profiles"] if profile["id"] == "harbor-smoke")
    assert synced_profile == {
        "id": "harbor-smoke",
        "kind": "execution",
        "config": execution_profile["execution"],
    }
    assert synced_profile["config"]["harbor"] == HARBOR_CONFIG


def test_catalog_manifest_is_deterministic() -> None:
    first = load_catalog(ROOT).to_manifest()
    second = load_catalog(ROOT).to_manifest()

    assert first["catalog_sha256"] == second["catalog_sha256"]
    assert {template["id"] for template in first["objects"]["run_templates"]} == {
        "hermes-harbor-agent-smoke",
        "hermes-terminal-bench-smoke",
        "openhands-terminal-bench-smoke",
        "openclaw-terminal-bench-smoke",
        "openhands-harbor-agent-smoke",
        "openclaw-harbor-agent-smoke",
    }


@pytest.mark.parametrize("field", ["api_key", "apiKey", "AGENTSCOPE_PROJECT_TOKEN"])
def test_rejects_secret_like_fields(tmp_path: Path, field: str) -> None:
    root = copy_catalog(tmp_path)
    (root / "model-profiles" / "bad.yaml").write_text(
        f"""schema_version: v1\nkind: ModelProfile\nid: unsafe\nprovider: demo\nmodel: demo\nparameters:\n  {field}: forbidden\n""",
        encoding="utf-8",
    )

    with pytest.raises(CatalogError, match="secret-like field"):
        load_catalog(root)


def test_rejects_secret_like_harbor_field(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    path = harbor_profile(root)
    path.write_text(path.read_text(encoding="utf-8") + "    api_key: forbidden\n", encoding="utf-8")

    with pytest.raises(CatalogError, match="secret-like field"):
        load_catalog(root)


@pytest.mark.parametrize(
    ("needle", "replacement", "message"),
    [
        ('version: "0.20.0"', "version: v0.16.1", "version must be a SemVer"),
        ("dataset: terminal-bench@2.0", "dataset: terminal bench@2.0", "dataset must be a non-empty safe Harbor dataset reference"),
        (
            "agent_import_path: benchmark_worker.harbor_agent:TaskRunnerAgent",
            "agent_import_path: agentlab_harbor.agents.OpenHandsAgent",
            "agent_import_path must be a Python",
        ),
        ("jobs_root: harbor-jobs", "jobs_root: ../harbor-jobs", "jobs_root must be a safe relative POSIX path"),
    ],
)
def test_rejects_invalid_harbor_field(tmp_path: Path, needle: str, replacement: str, message: str) -> None:
    root = copy_catalog(tmp_path)
    path = harbor_profile(root)
    path.write_text(path.read_text(encoding="utf-8").replace(needle, replacement), encoding="utf-8")

    with pytest.raises(CatalogError, match=message):
        load_catalog(root)


def test_rejects_unknown_harbor_field(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    path = harbor_profile(root)
    path.write_text(path.read_text(encoding="utf-8") + "    arbitrary_command: harbor run --unsafe\n", encoding="utf-8")

    with pytest.raises(CatalogError, match="unknown Harbor field"):
        load_catalog(root)


def test_rejects_harbor_config_outside_execution_profile(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    template = root / "run-templates" / "openhands-terminal-bench-smoke.yaml"
    template.write_text(
        template.read_text(encoding="utf-8")
        + "execution_overrides:\n"
        + "  harbor:\n"
        + "    version: 0.16.1\n"
        + "    dataset: terminal-bench@2.0\n"
        + "    agent_import_path: benchmark_worker.harbor_agent:TaskRunnerAgent\n"
        + "    jobs_root: harbor-jobs\n",
        encoding="utf-8",
    )

    with pytest.raises(CatalogError, match="unknown execution field"):
        load_catalog(root)


def test_rejects_harbor_suite_without_harbor_execution_config(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    path = harbor_profile(root)
    lines = path.read_text(encoding="utf-8").splitlines()
    path.write_text("\n".join(line for line in lines if not line.startswith("  harbor:") and not line.startswith("    ")) + "\n", encoding="utf-8")

    with pytest.raises(CatalogError, match="requires a Harbor configuration"):
        load_catalog(root)


def test_rejects_missing_reference(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    path = root / "run-templates" / "openhands-terminal-bench-smoke.yaml"
    path.write_text(path.read_text(encoding="utf-8").replace("suite: terminal-bench-2.0", "suite: unknown-suite"), encoding="utf-8")

    with pytest.raises(CatalogError, match="references unknown object"):
        load_catalog(root)


def test_rejects_framework_mismatch(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    path = root / "run-templates" / "openhands-terminal-bench-smoke.yaml"
    path.write_text(path.read_text(encoding="utf-8").replace("agent_profile: openhands-default", "agent_profile: openclaw-default"), encoding="utf-8")

    with pytest.raises(CatalogError, match="mixes artifact framework"):
        load_catalog(root)


def test_rejects_unknown_fields(tmp_path: Path) -> None:
    root = copy_catalog(tmp_path)
    path = harbor_profile(root)
    path.write_text(path.read_text(encoding="utf-8") + "unexpected: true\n", encoding="utf-8")

    with pytest.raises(CatalogError, match="unknown field"):
        load_catalog(root)
