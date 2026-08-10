from __future__ import annotations

from dataclasses import asdict, dataclass
from typing import Any


@dataclass(frozen=True)
class Suite:
    id: str
    harness: str
    dataset: str
    harness_version: str
    source_commit: str
    required_capabilities: tuple[str, ...]
    defaults: dict[str, Any]


@dataclass(frozen=True)
class AgentArtifact:
    id: str
    framework: str
    image: str
    image_digest: str
    source_commit: str
    task_runner_protocol: str
    capabilities: tuple[str, ...]


@dataclass(frozen=True)
class ModelProfile:
    id: str
    provider: str
    model: str
    parameters: dict[str, Any]


@dataclass(frozen=True)
class AgentProfile:
    id: str
    framework: str
    config: dict[str, Any]


@dataclass(frozen=True)
class ExecutionProfile:
    id: str
    execution: dict[str, Any]


@dataclass(frozen=True)
class RunTemplate:
    id: str
    suite: str
    agent_artifact: str
    model_profile: str
    agent_profile: str
    execution_profile: str
    task_selection: dict[str, Any]
    trials_per_case: int
    execution_overrides: dict[str, Any]


@dataclass(frozen=True)
class EffectiveRun:
    """Fully resolved, serialisable request consumed by a future benchmark worker."""

    template_id: str
    suite: Suite
    agent_artifact: AgentArtifact
    model_profile: ModelProfile
    agent_profile: AgentProfile
    execution: dict[str, Any]
    task_selection: dict[str, Any]
    trials_per_case: int

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)
