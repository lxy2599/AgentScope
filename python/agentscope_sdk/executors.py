"""Execution contracts and registry for AgentScope evaluation runners.

The runner uses the existing ``eval_jobs`` claim, heartbeat, and result
protocol for every evaluation.  This module only selects *how* a claimed job
is executed: a legacy in-process target function or a benchmark harness
adapter supplied by the application.

No benchmark package is imported here.  Future Harbor, tau2, or browser
harnesses can implement the contract in this module without coupling the SDK
to their runtime dependencies.
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import StrEnum
from typing import TYPE_CHECKING, Any, Protocol

from agentscope_sdk.target import RegisteredTarget

if TYPE_CHECKING:
    from agentscope_sdk.client import ClaimedJob


class BenchmarkErrorType(StrEnum):
    """Stable failure categories for benchmark execution."""

    ENVIRONMENT_ERROR = "environment_error"
    TARGET_ERROR = "target_error"
    HARNESS_ERROR = "harness_error"
    VERIFIER_ERROR = "verifier_error"
    CONTRACT_ERROR = "benchmark_contract_error"
    UNSUPPORTED_EXECUTION_KIND = "unsupported_execution_kind"


class BenchmarkExecutionError(RuntimeError):
    """A classified failure raised while executing a benchmark job."""

    def __init__(
        self,
        error_type: BenchmarkErrorType,
        message: str,
        *,
        details: Mapping[str, Any] | None = None,
    ) -> None:
        super().__init__(message)
        self.error_type = error_type
        self.details = dict(details or {})


class BenchmarkEnvironmentError(BenchmarkExecutionError):
    """Raised when a benchmark environment cannot be provisioned or reset."""

    def __init__(self, message: str, *, details: Mapping[str, Any] | None = None) -> None:
        super().__init__(BenchmarkErrorType.ENVIRONMENT_ERROR, message, details=details)


class BenchmarkTargetError(BenchmarkExecutionError):
    """Raised when the target agent cannot execute a benchmark task."""

    def __init__(self, message: str, *, details: Mapping[str, Any] | None = None) -> None:
        super().__init__(BenchmarkErrorType.TARGET_ERROR, message, details=details)


class BenchmarkHarnessError(BenchmarkExecutionError):
    """Raised when the external benchmark harness itself fails."""

    def __init__(self, message: str, *, details: Mapping[str, Any] | None = None) -> None:
        super().__init__(BenchmarkErrorType.HARNESS_ERROR, message, details=details)


class BenchmarkVerifierError(BenchmarkExecutionError):
    """Raised when an official verifier cannot produce a valid verdict."""

    def __init__(self, message: str, *, details: Mapping[str, Any] | None = None) -> None:
        super().__init__(BenchmarkErrorType.VERIFIER_ERROR, message, details=details)


class BenchmarkContractError(BenchmarkExecutionError):
    """Raised when an adapter violates the benchmark execution contract."""

    def __init__(self, message: str, *, details: Mapping[str, Any] | None = None) -> None:
        super().__init__(BenchmarkErrorType.CONTRACT_ERROR, message, details=details)


@dataclass(frozen=True)
class BenchmarkArtifact:
    """A reference to an artifact generated during benchmark execution."""

    kind: str
    uri: str
    metadata: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        """Return a JSON-serializable artifact representation."""
        result: dict[str, Any] = {"kind": self.kind, "uri": self.uri}
        if self.metadata:
            result["metadata"] = self.metadata
        return result


@dataclass
class BenchmarkExecutionResult:
    """Normalized result returned by a native benchmark adapter.

    ``official_score`` and ``official_passed`` are the harness's authoritative
    verdict.  They deliberately remain separate from AgentScope's generic
    latency, cost, and tracing metrics.  An invalid result represents an
    infrastructure or harness failure and must never be counted as an agent
    capability failure.
    """

    actual_output: Any = None
    valid: bool = True
    official_score: float | None = None
    official_passed: bool | None = None
    official_reason: str = ""
    official_metrics: dict[str, Any] = field(default_factory=dict)
    artifacts: list[BenchmarkArtifact] = field(default_factory=list)
    error_type: BenchmarkErrorType | None = None
    error_message: str = ""
    error_details: dict[str, Any] = field(default_factory=dict)

    def __post_init__(self) -> None:
        if self.error_type is not None and not isinstance(self.error_type, BenchmarkErrorType):
            raise BenchmarkContractError("error_type must be a BenchmarkErrorType")
        has_score = self.official_score is not None
        has_passed = self.official_passed is not None
        if has_score != has_passed:
            raise BenchmarkContractError(
                "official_score and official_passed must be supplied together",
            )
        if self.valid and self.error_type is not None:
            raise BenchmarkContractError("a valid benchmark result cannot contain an error type")
        if not self.valid:
            if self.error_type is None:
                raise BenchmarkContractError("an invalid benchmark result requires an error type")
            if not self.error_message:
                raise BenchmarkContractError("an invalid benchmark result requires an error message")

    @property
    def has_official_verdict(self) -> bool:
        """Whether this result contains an official benchmark verdict."""
        return self.official_score is not None and self.official_passed is not None

    @classmethod
    def invalid(
        cls,
        error_type: BenchmarkErrorType,
        error_message: str,
        *,
        actual_output: Any = None,
        official_metrics: Mapping[str, Any] | None = None,
        artifacts: list[BenchmarkArtifact] | None = None,
        error_details: Mapping[str, Any] | None = None,
    ) -> BenchmarkExecutionResult:
        """Create an invalid result without treating it as an agent failure."""
        return cls(
            actual_output=actual_output,
            valid=False,
            official_metrics=dict(official_metrics or {}),
            artifacts=list(artifacts or []),
            error_type=error_type,
            error_message=error_message,
            error_details=dict(error_details or {}),
        )

    def to_details(self) -> dict[str, Any]:
        """Return the normalized benchmark payload stored in result details."""
        details: dict[str, Any] = {
            "valid": self.valid,
            "official_metrics": self.official_metrics,
            "artifacts": [artifact.to_dict() for artifact in self.artifacts],
        }
        if self.has_official_verdict:
            details["official_score"] = self.official_score
            details["official_passed"] = self.official_passed
            details["official_reason"] = self.official_reason
        if not self.valid:
            details["error_type"] = self.error_type.value if self.error_type else ""
            details["error_message"] = self.error_message
            details["error_details"] = self.error_details
        return details


@dataclass(frozen=True)
class BenchmarkExecutionRequest:
    """Execution context supplied to an executor without harness coupling."""

    claimed_job: ClaimedJob
    target: RegisteredTarget | None
    context: dict[str, Any]

    @property
    def execution_config(self) -> dict[str, Any]:
        """Return the optional benchmark configuration supplied by the server."""
        return self.claimed_job.execution_config


class BenchmarkExecutor(Protocol):
    """Protocol implemented by all runner execution strategies."""

    def execute(self, request: BenchmarkExecutionRequest) -> BenchmarkExecutionResult:
        """Execute one job or raise a classified benchmark error."""


class ExternalBenchmarkHarness(Protocol):
    """Stable boundary for future wrappers around official benchmark packages.

    A Harbor, tau2, or browser harness integration can implement this protocol
    and be called from a target decorated with ``@target``.  The SDK never
    imports those optional packages directly.
    """

    def execute(self, request: BenchmarkExecutionRequest) -> BenchmarkExecutionResult:
        """Run an official harness and return its normalized outcome."""


class FunctionExecutor:
    """Legacy executor for ordinary decorated target functions."""

    def execute(self, request: BenchmarkExecutionRequest) -> BenchmarkExecutionResult:
        if request.target is None:
            raise BenchmarkContractError("function execution requires a registered target")
        return BenchmarkExecutionResult(
            actual_output=request.target.func(request.claimed_job.case_input, request.context),
        )


class BenchmarkContractExecutor:
    """Executor for decorated target wrappers around external harnesses.

    The target must return ``BenchmarkExecutionResult``.  This makes the
    external harness boundary explicit and prevents accidental use of a
    textual final answer as an official benchmark score.
    """

    def execute(self, request: BenchmarkExecutionRequest) -> BenchmarkExecutionResult:
        if request.target is None:
            raise BenchmarkContractError("native benchmark execution requires a registered target")
        result = request.target.func(request.claimed_job.case_input, request.context)
        if not isinstance(result, BenchmarkExecutionResult):
            raise BenchmarkContractError(
                "native benchmark targets must return BenchmarkExecutionResult",
                details={"returned_type": type(result).__name__},
            )
        if result.valid and not result.has_official_verdict:
            raise BenchmarkContractError(
                "valid native benchmark results must include an official verdict",
            )
        return result


class ExecutorRegistry:
    """Maps an ``execution_kind`` from a claimed job to an executor."""

    def __init__(self) -> None:
        self._executors: dict[str, BenchmarkExecutor] = {}

    def register(self, execution_kind: str, executor: BenchmarkExecutor, *, replace: bool = False) -> None:
        """Register an executor for an execution kind."""
        normalized_kind = _normalize_execution_kind(execution_kind)
        if normalized_kind in self._executors and not replace:
            raise ValueError(f"Executor already registered for execution kind '{normalized_kind}'")
        self._executors[normalized_kind] = executor

    def resolve(self, execution_kind: str | None) -> BenchmarkExecutor:
        """Return an executor or raise a stable unsupported-kind error."""
        normalized_kind = _normalize_execution_kind(execution_kind)
        executor = self._executors.get(normalized_kind)
        if executor is None:
            raise BenchmarkExecutionError(
                BenchmarkErrorType.UNSUPPORTED_EXECUTION_KIND,
                f"Unsupported execution kind '{normalized_kind}'",
                details={"execution_kind": normalized_kind},
            )
        return executor


def _normalize_execution_kind(execution_kind: str | None) -> str:
    return (execution_kind or "function").strip().lower() or "function"


def create_default_executor_registry() -> ExecutorRegistry:
    """Build the executor registry used by the runner by default."""
    registry = ExecutorRegistry()
    registry.register("function", FunctionExecutor())
    contract_executor = BenchmarkContractExecutor()
    registry.register("native_benchmark", contract_executor)
    registry.register("benchmark_contract", contract_executor)
    from agentscope_sdk.harbor import HarborExecutor
    from agentscope_sdk.tau2 import Tau2Executor

    registry.register("harbor", HarborExecutor())
    registry.register("tau2", Tau2Executor())
    return registry


_default_executor_registry = create_default_executor_registry()


def get_executor_registry() -> ExecutorRegistry:
    """Return the process-wide default executor registry."""
    return _default_executor_registry
