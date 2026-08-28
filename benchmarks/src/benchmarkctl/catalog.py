from __future__ import annotations

import copy
import hashlib
import json
import re
from dataclasses import asdict
from pathlib import Path
from typing import Any, Callable

import yaml

from .errors import CatalogError
from .models import (
    AgentArtifact,
    AgentProfile,
    EffectiveRun,
    ExecutionProfile,
    ModelProfile,
    RunTemplate,
    Suite,
)

SCHEMA_VERSION = "v1"
TASK_RUNNER_PROTOCOL = "v1"
DIRECTORIES = {
    "BenchmarkSuite": "suites",
    "AgentArtifact": "artifacts",
    "ModelProfile": "model-profiles",
    "AgentProfile": "agent-profiles",
    "ExecutionProfile": "execution-profiles",
    "BenchmarkRunTemplate": "run-templates",
}
_FORBIDDEN_SECRET_KEY = re.compile(
    r"(?:^|[_-])(api[_-]?key|secret|token|password|credential|private[_-]?key|authorization)(?:$|[_-])",
    re.IGNORECASE,
)
_FORBIDDEN_SECRET_COMPACT = {
    "apikey",
    "secret",
    "token",
    "password",
    "credential",
    "credentials",
    "privatekey",
    "authorization",
    "accesstoken",
    "projecttoken",
    "bearer",
}


def _is_secret_like_key(key: str) -> bool:
    compact = re.sub(r"[^a-z0-9]", "", key.lower())
    return bool(_FORBIDDEN_SECRET_KEY.search(key)) or compact in _FORBIDDEN_SECRET_COMPACT or compact.endswith(
        ("apikey", "secret", "token", "password", "credential", "privatekey")
    )
_ID = re.compile(r"^[a-z0-9][a-z0-9._-]*$")
_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
_SEMVER = re.compile(
    r"^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    r"(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?$"
)
_HARBOR_DATASET = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/@:+-]*$")
_PYTHON_IMPORT_PATH = re.compile(
    r"^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*:[A-Za-z_][A-Za-z0-9_]*$"
)
_SAFE_RELATIVE_PATH = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]*$")
_HARBOR_REQUIRED_FIELDS = {"version", "agent_import_path", "jobs_root"}
_HARBOR_SOURCE_FIELDS = {"dataset", "task_path", "task_path_root"}
_HARBOR_OPTIONAL_FIELDS = {"job_name", "config_path", "dataset", "task_path", "task_path_root"}


def _ensure_mapping(value: Any, where: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise CatalogError(f"{where} must be a mapping")
    return value


def _reject_secrets(value: Any, path: str = "$") -> None:
    if isinstance(value, dict):
        for key, child in value.items():
            if not isinstance(key, str):
                raise CatalogError(f"{path} has a non-string mapping key")
            if _is_secret_like_key(key):
                raise CatalogError(f"secret-like field is forbidden in catalog: {path}.{key}")
            _reject_secrets(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _reject_secrets(child, f"{path}[{index}]")


def _document(path: Path) -> dict[str, Any]:
    try:
        raw = yaml.safe_load(path.read_text(encoding="utf-8"))
    except yaml.YAMLError as exc:
        raise CatalogError(f"invalid YAML in {path}: {exc}") from exc
    document = _ensure_mapping(raw, str(path))
    _reject_secrets(document)
    return document


def _exact_keys(document: dict[str, Any], required: set[str], optional: set[str], source: str) -> None:
    missing = required - document.keys()
    unknown = document.keys() - required - optional
    if missing:
        raise CatalogError(f"{source} missing required field(s): {', '.join(sorted(missing))}")
    if unknown:
        raise CatalogError(f"{source} has unknown field(s): {', '.join(sorted(unknown))}")


def _string(document: dict[str, Any], field: str, source: str) -> str:
    value = document[field]
    if not isinstance(value, str) or not value:
        raise CatalogError(f"{source}.{field} must be a non-empty string")
    return value


def _identifier(value: str, source: str) -> str:
    if not _ID.fullmatch(value):
        raise CatalogError(f"{source} must use lowercase letters, digits, '.', '_' or '-'")
    return value


def _strings(value: Any, source: str) -> tuple[str, ...]:
    if not isinstance(value, list) or not all(isinstance(item, str) and item for item in value):
        raise CatalogError(f"{source} must be a list of non-empty strings")
    return tuple(value)


def _mapping(value: Any, source: str) -> dict[str, Any]:
    return copy.deepcopy(_ensure_mapping(value, source))


def _base(document: dict[str, Any], kind: str, source: str) -> str:
    if document.get("schema_version") != SCHEMA_VERSION:
        raise CatalogError(f"{source}.schema_version must be {SCHEMA_VERSION!r}")
    if document.get("kind") != kind:
        raise CatalogError(f"{source}.kind must be {kind!r}")
    return _identifier(_string(document, "id", source), f"{source}.id")


def _parse_suite(document: dict[str, Any], source: str) -> Suite:
    _exact_keys(document, {"schema_version", "kind", "id", "harness", "dataset", "harness_version", "source_commit", "required_capabilities", "defaults"}, set(), source)
    return Suite(
        id=_base(document, "BenchmarkSuite", source),
        harness=_string(document, "harness", source),
        dataset=_string(document, "dataset", source),
        harness_version=_string(document, "harness_version", source),
        source_commit=_string(document, "source_commit", source),
        required_capabilities=_strings(document["required_capabilities"], f"{source}.required_capabilities"),
        defaults=_mapping(document["defaults"], f"{source}.defaults"),
    )


def _parse_artifact(document: dict[str, Any], source: str) -> AgentArtifact:
    _exact_keys(document, {"schema_version", "kind", "id", "framework", "image", "image_digest", "source_commit", "task_runner_protocol"}, {"capabilities"}, source)
    digest = _string(document, "image_digest", source)
    if not _DIGEST.fullmatch(digest):
        raise CatalogError(f"{source}.image_digest must be a sha256 image digest")
    protocol = _string(document, "task_runner_protocol", source)
    if protocol != TASK_RUNNER_PROTOCOL:
        raise CatalogError(f"{source}.task_runner_protocol must be {TASK_RUNNER_PROTOCOL!r}")
    capabilities = _strings(document.get("capabilities", []), f"{source}.capabilities")
    return AgentArtifact(
        id=_base(document, "AgentArtifact", source),
        framework=_string(document, "framework", source),
        image=_string(document, "image", source),
        image_digest=digest,
        source_commit=_string(document, "source_commit", source),
        task_runner_protocol=protocol,
        capabilities=capabilities,
    )


def _parse_model(document: dict[str, Any], source: str) -> ModelProfile:
    _exact_keys(document, {"schema_version", "kind", "id", "provider", "model", "parameters"}, set(), source)
    return ModelProfile(
        id=_base(document, "ModelProfile", source),
        provider=_string(document, "provider", source),
        model=_string(document, "model", source),
        parameters=_mapping(document["parameters"], f"{source}.parameters"),
    )


def _parse_agent_profile(document: dict[str, Any], source: str) -> AgentProfile:
    _exact_keys(document, {"schema_version", "kind", "id", "framework", "config"}, set(), source)
    return AgentProfile(
        id=_base(document, "AgentProfile", source),
        framework=_string(document, "framework", source),
        config=_mapping(document["config"], f"{source}.config"),
    )


def _safe_relative_path(value: Any, source: str) -> None:
    if not isinstance(value, str) or not value:
        raise CatalogError(f"{source} must be a non-empty relative POSIX path")
    if not _SAFE_RELATIVE_PATH.fullmatch(value) or any(part in {"", ".", ".."} for part in value.split("/")):
        raise CatalogError(f"{source} must be a safe relative POSIX path without '.' or '..' segments")


def _validate_harbor(harbor: Any, source: str) -> None:
    """Validate the declarative, non-secret Harbor v1 harness block.

    Harbor executes commands in a later worker stage.  The catalog deliberately
    records only typed identifiers and safe relative paths; it never accepts an
    arbitrary command, environment map, or free-form CLI arguments.
    """

    config = _ensure_mapping(harbor, source)
    allowed = _HARBOR_REQUIRED_FIELDS | _HARBOR_OPTIONAL_FIELDS
    missing = _HARBOR_REQUIRED_FIELDS - config.keys()
    unknown = config.keys() - allowed
    if missing:
        raise CatalogError(f"{source} missing required field(s): {', '.join(sorted(missing))}")
    if unknown:
        raise CatalogError(f"{source} has unknown Harbor field(s): {', '.join(sorted(unknown))}")

    version = config["version"]
    if not isinstance(version, str) or not _SEMVER.fullmatch(version):
        raise CatalogError(f"{source}.version must be a SemVer version without a leading 'v'")

    source_fields = _HARBOR_SOURCE_FIELDS & config.keys()
    if len(source_fields) != 1:
        raise CatalogError(f"{source} must set exactly one of dataset, task_path, or task_path_root")
    if "dataset" in config:
        dataset = config["dataset"]
        if not isinstance(dataset, str) or not _HARBOR_DATASET.fullmatch(dataset):
            raise CatalogError(f"{source}.dataset must be a non-empty safe Harbor dataset reference")
    if "task_path" in config:
        _safe_relative_path(config["task_path"], f"{source}.task_path")
    if "task_path_root" in config:
        _safe_relative_path(config["task_path_root"], f"{source}.task_path_root")

    agent_import_path = config["agent_import_path"]
    if not isinstance(agent_import_path, str) or not _PYTHON_IMPORT_PATH.fullmatch(agent_import_path):
        raise CatalogError(f"{source}.agent_import_path must be a Python 'module:attribute' import path")

    _safe_relative_path(config["jobs_root"], f"{source}.jobs_root")
    if "config_path" in config:
        _safe_relative_path(config["config_path"], f"{source}.config_path")
    if "job_name" in config:
        job_name = config["job_name"]
        if not isinstance(job_name, str) or not _ID.fullmatch(job_name):
            raise CatalogError(f"{source}.job_name must use lowercase letters, digits, '.', '_' or '-'")


def _validate_execution(execution: dict[str, Any], source: str, *, allow_harbor: bool = False) -> None:
    allowed = {"concurrency", "task_timeout_s", "max_retries", "fail_fast", "artifact_policy"}
    if allow_harbor:
        allowed.add("harbor")
    unknown = execution.keys() - allowed
    if unknown:
        raise CatalogError(f"{source} has unknown execution field(s): {', '.join(sorted(unknown))}")
    for field in ("concurrency", "task_timeout_s", "max_retries"):
        if field in execution and (not isinstance(execution[field], int) or isinstance(execution[field], bool) or execution[field] < (0 if field == "max_retries" else 1)):
            raise CatalogError(f"{source}.{field} must be an integer in its valid range")
    if "fail_fast" in execution and not isinstance(execution["fail_fast"], bool):
        raise CatalogError(f"{source}.fail_fast must be boolean")
    if "artifact_policy" in execution and execution["artifact_policy"] not in {"none", "failures_only", "all"}:
        raise CatalogError(f"{source}.artifact_policy must be none, failures_only, or all")
    if "harbor" in execution:
        if not allow_harbor:
            raise CatalogError(f"{source}.harbor is only allowed in ExecutionProfile.execution")
        _validate_harbor(execution["harbor"], f"{source}.harbor")


def _parse_execution(document: dict[str, Any], source: str) -> ExecutionProfile:
    _exact_keys(document, {"schema_version", "kind", "id", "execution"}, set(), source)
    execution = _mapping(document["execution"], f"{source}.execution")
    _validate_execution(execution, f"{source}.execution", allow_harbor=True)
    return ExecutionProfile(id=_base(document, "ExecutionProfile", source), execution=execution)


def _validate_task_selection(selection: dict[str, Any], source: str) -> None:
    mode = selection.get("mode")
    allowed_by_mode = {
        "all": {"mode"},
        "ids": {"mode", "include", "exclude"},
        "sample": {"mode", "count", "seed", "exclude"},
        "previous_failures": {"mode", "source_experiment"},
    }
    if mode not in allowed_by_mode:
        raise CatalogError(f"{source}.mode must be one of {', '.join(sorted(allowed_by_mode))}")
    unknown = selection.keys() - allowed_by_mode[mode]
    if unknown:
        raise CatalogError(f"{source} has fields incompatible with mode {mode!r}: {', '.join(sorted(unknown))}")
    if mode == "ids":
        include = selection.get("include")
        if not isinstance(include, list) or not include or not all(isinstance(item, str) and item for item in include):
            raise CatalogError(f"{source}.include must contain at least one task id")
        if "exclude" in selection:
            _strings(selection["exclude"], f"{source}.exclude")
    if mode == "sample":
        if not isinstance(selection.get("count"), int) or isinstance(selection["count"], bool) or selection["count"] < 1:
            raise CatalogError(f"{source}.count must be a positive integer")
        if not isinstance(selection.get("seed"), int) or isinstance(selection["seed"], bool):
            raise CatalogError(f"{source}.seed must be an integer")
        if "exclude" in selection:
            _strings(selection["exclude"], f"{source}.exclude")
    if mode == "previous_failures" and (not isinstance(selection.get("source_experiment"), str) or not selection["source_experiment"]):
        raise CatalogError(f"{source}.source_experiment must be a non-empty string")


def _parse_template(document: dict[str, Any], source: str) -> RunTemplate:
    required = {"schema_version", "kind", "id", "suite", "agent_artifact", "model_profile", "agent_profile", "execution_profile", "task_selection", "trials_per_case"}
    _exact_keys(document, required, {"execution_overrides"}, source)
    task_selection = _mapping(document["task_selection"], f"{source}.task_selection")
    _validate_task_selection(task_selection, f"{source}.task_selection")
    overrides = _mapping(document.get("execution_overrides", {}), f"{source}.execution_overrides")
    _validate_execution(overrides, f"{source}.execution_overrides")
    trials = document["trials_per_case"]
    if not isinstance(trials, int) or isinstance(trials, bool) or trials < 1:
        raise CatalogError(f"{source}.trials_per_case must be a positive integer")
    return RunTemplate(
        id=_base(document, "BenchmarkRunTemplate", source),
        suite=_string(document, "suite", source),
        agent_artifact=_string(document, "agent_artifact", source),
        model_profile=_string(document, "model_profile", source),
        agent_profile=_string(document, "agent_profile", source),
        execution_profile=_string(document, "execution_profile", source),
        task_selection=task_selection,
        trials_per_case=trials,
        execution_overrides=overrides,
    )


Parser = Callable[[dict[str, Any], str], Any]
_PARSERS: dict[str, Parser] = {
    "BenchmarkSuite": _parse_suite,
    "AgentArtifact": _parse_artifact,
    "ModelProfile": _parse_model,
    "AgentProfile": _parse_agent_profile,
    "ExecutionProfile": _parse_execution,
    "BenchmarkRunTemplate": _parse_template,
}


class Catalog:
    def __init__(self) -> None:
        self.suites: dict[str, Suite] = {}
        self.artifacts: dict[str, AgentArtifact] = {}
        self.models: dict[str, ModelProfile] = {}
        self.agent_profiles: dict[str, AgentProfile] = {}
        self.execution_profiles: dict[str, ExecutionProfile] = {}
        self.run_templates: dict[str, RunTemplate] = {}

    def _collection(self, kind: str) -> dict[str, Any]:
        return {
            "BenchmarkSuite": self.suites,
            "AgentArtifact": self.artifacts,
            "ModelProfile": self.models,
            "AgentProfile": self.agent_profiles,
            "ExecutionProfile": self.execution_profiles,
            "BenchmarkRunTemplate": self.run_templates,
        }[kind]

    def add(self, kind: str, value: Any, source: str) -> None:
        collection = self._collection(kind)
        if value.id in collection:
            raise CatalogError(f"duplicate {kind} id {value.id!r}; second definition: {source}")
        collection[value.id] = value

    def compile(self, template_id: str) -> EffectiveRun:
        try:
            template = self.run_templates[template_id]
            suite = self.suites[template.suite]
            artifact = self.artifacts[template.agent_artifact]
            model = self.models[template.model_profile]
            agent_profile = self.agent_profiles[template.agent_profile]
            execution_profile = self.execution_profiles[template.execution_profile]
        except KeyError as exc:
            raise CatalogError(f"run template {template_id!r} references unknown object {exc.args[0]!r}") from exc
        if artifact.framework != agent_profile.framework:
            raise CatalogError(
                f"run template {template_id!r} mixes artifact framework {artifact.framework!r} "
                f"with agent profile framework {agent_profile.framework!r}"
            )
        missing_capabilities = set(suite.required_capabilities) - set(artifact.capabilities)
        if missing_capabilities:
            raise CatalogError(
                f"artifact {artifact.id!r} lacks suite capabilities: {', '.join(sorted(missing_capabilities))}"
            )
        execution = copy.deepcopy(suite.defaults)
        _validate_execution(execution, f"suite {suite.id!r}.defaults")
        execution.update(execution_profile.execution)
        execution.update(template.execution_overrides)
        _validate_execution(execution, f"effective execution for {template_id!r}", allow_harbor=True)
        if suite.harness == "harbor" and "harbor" not in execution:
            raise CatalogError(
                f"effective execution for {template_id!r} requires a Harbor configuration for suite {suite.id!r}"
            )
        return EffectiveRun(
            template_id=template.id,
            suite=suite,
            agent_artifact=artifact,
            model_profile=model,
            agent_profile=agent_profile,
            execution=execution,
            task_selection=copy.deepcopy(template.task_selection),
            trials_per_case=template.trials_per_case,
        )

    def to_manifest(self) -> dict[str, Any]:
        objects = {
            "suites": [asdict(item) for item in self.suites.values()],
            "artifacts": [asdict(item) for item in self.artifacts.values()],
            "model_profiles": [asdict(item) for item in self.models.values()],
            "agent_profiles": [asdict(item) for item in self.agent_profiles.values()],
            "execution_profiles": [asdict(item) for item in self.execution_profiles.values()],
            "run_templates": [asdict(item) for item in self.run_templates.values()],
        }
        canonical = json.dumps(objects, sort_keys=True, separators=(",", ":"))
        return {
            "schema_version": SCHEMA_VERSION,
            "catalog_sha256": hashlib.sha256(canonical.encode("utf-8")).hexdigest(),
            "objects": objects,
        }


def load_catalog(root: str | Path) -> Catalog:
    root_path = Path(root).resolve()
    catalog = Catalog()
    if not root_path.is_dir():
        raise CatalogError(f"catalog root does not exist: {root_path}")
    for kind, directory in DIRECTORIES.items():
        directory_path = root_path / directory
        if not directory_path.exists():
            continue
        if not directory_path.is_dir():
            raise CatalogError(f"catalog path is not a directory: {directory_path}")
        for path in sorted((*directory_path.glob("*.yaml"), *directory_path.glob("*.yml"))):
            document = _document(path)
            if document.get("kind") != kind:
                raise CatalogError(f"{path} belongs in {DIRECTORIES.get(document.get('kind'), 'a known catalog directory')}, not {directory}")
            catalog.add(kind, _PARSERS[kind](document, str(path)), str(path))
    # Resolve every template during load, so catalog-sync/validate fail early.
    for template_id in catalog.run_templates:
        catalog.compile(template_id)
    return catalog
