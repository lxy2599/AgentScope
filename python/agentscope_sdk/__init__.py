"""AgentScope Python SDK public API.

The package root intentionally keeps the runtime lifecycle API lightweight.  Agent
processes that only register themselves and send heartbeats should not need to
import optional LLM, tracing, Harbor, or benchmark execution dependencies.
Other historically-root-level public names remain available and are imported on
first access via :pep:`562` module ``__getattr__``.
"""

from __future__ import annotations

from importlib import import_module
from typing import TYPE_CHECKING, Any

# The presence/heartbeat path is deliberately lightweight.  The target
# decorator is also eager because its public function name matches the target
# submodule name; binding it here preserves the long-standing root-level
# ``target`` export even after other modules import that submodule.  In
# particular, keep tracing, Harbor, evaluators, and benchmark executors out of
# this import path: downstream frameworks may initialize LLM integrations while
# importing those modules.
from agentscope_sdk.runtime import (
    CAPABILITY_KINDS,
    CONTRACT_VERSION,
    RUN_STATUS_THINKING,
    RUN_STATUS_WAITING_MEMORY,
    RUN_STATUS_WAITING_USER,
    Runtime,
    RuntimeRun,
    build_agent_metadata,
    build_run_metadata,
    fail_run,
    finish_run,
    get_runtime,
    init,
    normalize_capability,
    shutdown_runtime,
    start_run,
    update_run_status,
)
from agentscope_sdk.target import target

if TYPE_CHECKING:
    # Keep type checkers and IDEs aware of the complete historical root API
    # without making these imports part of the runtime import path.
    from agentscope_sdk.evaluators import EvaluationResult, EvaluatorConfigError, evaluate_rule
    from agentscope_sdk.executors import (
        BenchmarkArtifact,
        BenchmarkContractError,
        BenchmarkEnvironmentError,
        BenchmarkErrorType,
        BenchmarkExecutionError,
        BenchmarkExecutionRequest,
        BenchmarkExecutionResult,
        BenchmarkHarnessError,
        BenchmarkTargetError,
        BenchmarkVerifierError,
        ExecutorRegistry,
        ExternalBenchmarkHarness,
        create_default_executor_registry,
        get_executor_registry,
    )
    from agentscope_sdk.harbor import HarborExecutor, build_harbor_command, parse_harbor_result
    from agentscope_sdk.judges import (
        JudgeProvider,
        JudgeRequest,
        JudgeResponseError,
        JudgeResult,
        parse_judge_response,
    )
    from agentscope_sdk.runtime_trace import RuntimeTraceSession
    from agentscope_sdk.snapshots import RunSnapshotBuilder
    from agentscope_sdk.target import (
        RegisteredTarget,
        TargetNotFoundError,
        TargetRegistrationError,
        get_target,
        target,
    )
    from agentscope_sdk.tau2 import Tau2Executor, build_tau2_command, parse_tau2_result
    from agentscope_sdk.tracing import (
        TracingStats,
        build_final_answer_attributes,
        build_thought_attributes,
        configure_tracing,
        force_flush,
        get_tracing_stats,
        record_final_answer,
        shutdown_tracing,
        trace_llm,
        trace_retrieval,
        trace_skill,
        trace_thought,
        trace_tool,
    )

# Public names that historically came from non-runtime modules.  Values are
# cached in this module after the first lookup, preserving normal import and
# identity behavior for callers such as ``from agentscope_sdk import target``.
_LAZY_EXPORTS: dict[str, tuple[str, str]] = {
    "BenchmarkArtifact": ("agentscope_sdk.executors", "BenchmarkArtifact"),
    "BenchmarkContractError": ("agentscope_sdk.executors", "BenchmarkContractError"),
    "BenchmarkEnvironmentError": ("agentscope_sdk.executors", "BenchmarkEnvironmentError"),
    "BenchmarkErrorType": ("agentscope_sdk.executors", "BenchmarkErrorType"),
    "BenchmarkExecutionError": ("agentscope_sdk.executors", "BenchmarkExecutionError"),
    "BenchmarkExecutionRequest": ("agentscope_sdk.executors", "BenchmarkExecutionRequest"),
    "BenchmarkExecutionResult": ("agentscope_sdk.executors", "BenchmarkExecutionResult"),
    "BenchmarkHarnessError": ("agentscope_sdk.executors", "BenchmarkHarnessError"),
    "BenchmarkTargetError": ("agentscope_sdk.executors", "BenchmarkTargetError"),
    "BenchmarkVerifierError": ("agentscope_sdk.executors", "BenchmarkVerifierError"),
    "EvaluationResult": ("agentscope_sdk.evaluators", "EvaluationResult"),
    "EvaluatorConfigError": ("agentscope_sdk.evaluators", "EvaluatorConfigError"),
    "ExecutorRegistry": ("agentscope_sdk.executors", "ExecutorRegistry"),
    "ExternalBenchmarkHarness": ("agentscope_sdk.executors", "ExternalBenchmarkHarness"),
    "HarborExecutor": ("agentscope_sdk.harbor", "HarborExecutor"),
    "JudgeProvider": ("agentscope_sdk.judges", "JudgeProvider"),
    "JudgeRequest": ("agentscope_sdk.judges", "JudgeRequest"),
    "JudgeResponseError": ("agentscope_sdk.judges", "JudgeResponseError"),
    "JudgeResult": ("agentscope_sdk.judges", "JudgeResult"),
    "RegisteredTarget": ("agentscope_sdk.target", "RegisteredTarget"),
    "RunSnapshotBuilder": ("agentscope_sdk.snapshots", "RunSnapshotBuilder"),
    "RuntimeTraceSession": ("agentscope_sdk.runtime_trace", "RuntimeTraceSession"),
    "TargetNotFoundError": ("agentscope_sdk.target", "TargetNotFoundError"),
    "TargetRegistrationError": ("agentscope_sdk.target", "TargetRegistrationError"),
    "Tau2Executor": ("agentscope_sdk.tau2", "Tau2Executor"),
    "TracingStats": ("agentscope_sdk.tracing", "TracingStats"),
    "build_final_answer_attributes": ("agentscope_sdk.tracing", "build_final_answer_attributes"),
    "build_harbor_command": ("agentscope_sdk.harbor", "build_harbor_command"),
    "build_tau2_command": ("agentscope_sdk.tau2", "build_tau2_command"),
    "build_thought_attributes": ("agentscope_sdk.tracing", "build_thought_attributes"),
    "configure_tracing": ("agentscope_sdk.tracing", "configure_tracing"),
    "create_default_executor_registry": ("agentscope_sdk.executors", "create_default_executor_registry"),
    "evaluate_rule": ("agentscope_sdk.evaluators", "evaluate_rule"),
    "force_flush": ("agentscope_sdk.tracing", "force_flush"),
    "get_executor_registry": ("agentscope_sdk.executors", "get_executor_registry"),
    "get_target": ("agentscope_sdk.target", "get_target"),
    "get_tracing_stats": ("agentscope_sdk.tracing", "get_tracing_stats"),
    "parse_harbor_result": ("agentscope_sdk.harbor", "parse_harbor_result"),
    "parse_judge_response": ("agentscope_sdk.judges", "parse_judge_response"),
    "parse_tau2_result": ("agentscope_sdk.tau2", "parse_tau2_result"),
    "record_final_answer": ("agentscope_sdk.tracing", "record_final_answer"),
    "shutdown_tracing": ("agentscope_sdk.tracing", "shutdown_tracing"),
    "trace_llm": ("agentscope_sdk.tracing", "trace_llm"),
    "trace_retrieval": ("agentscope_sdk.tracing", "trace_retrieval"),
    "trace_skill": ("agentscope_sdk.tracing", "trace_skill"),
    "trace_thought": ("agentscope_sdk.tracing", "trace_thought"),
    "trace_tool": ("agentscope_sdk.tracing", "trace_tool"),
}


def __getattr__(name: str) -> Any:
    """Lazily resolve a historical root-level SDK export.

    Python calls this for ``from agentscope_sdk import NAME`` when ``NAME`` is
    not already in this module's globals.  Caching makes subsequent lookups
    indistinguishable from the former eager export behavior.
    """
    try:
        module_name, attribute_name = _LAZY_EXPORTS[name]
    except KeyError as exc:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}") from exc

    module = import_module(module_name)
    value = getattr(module, attribute_name)
    globals()[name] = value
    return value


def __dir__() -> list[str]:
    """Expose lazy public names to interactive shells and documentation tools."""
    return sorted(set(globals()) | set(_LAZY_EXPORTS))


__all__ = [
    "CAPABILITY_KINDS",
    "CONTRACT_VERSION",
    "RUN_STATUS_THINKING",
    "RUN_STATUS_WAITING_MEMORY",
    "RUN_STATUS_WAITING_USER",
    "BenchmarkArtifact",
    "BenchmarkContractError",
    "BenchmarkEnvironmentError",
    "BenchmarkErrorType",
    "BenchmarkExecutionError",
    "BenchmarkExecutionRequest",
    "BenchmarkExecutionResult",
    "BenchmarkHarnessError",
    "BenchmarkTargetError",
    "BenchmarkVerifierError",
    "EvaluationResult",
    "EvaluatorConfigError",
    "ExecutorRegistry",
    "ExternalBenchmarkHarness",
    "HarborExecutor",
    "JudgeProvider",
    "JudgeRequest",
    "JudgeResponseError",
    "JudgeResult",
    "RegisteredTarget",
    "RunSnapshotBuilder",
    "Runtime",
    "RuntimeRun",
    "RuntimeTraceSession",
    "TargetNotFoundError",
    "TargetRegistrationError",
    "Tau2Executor",
    "TracingStats",
    "build_agent_metadata",
    "build_final_answer_attributes",
    "build_harbor_command",
    "build_run_metadata",
    "build_tau2_command",
    "build_thought_attributes",
    "configure_tracing",
    "create_default_executor_registry",
    "evaluate_rule",
    "fail_run",
    "finish_run",
    "force_flush",
    "get_executor_registry",
    "get_runtime",
    "get_target",
    "get_tracing_stats",
    "init",
    "normalize_capability",
    "parse_harbor_result",
    "parse_judge_response",
    "parse_tau2_result",
    "record_final_answer",
    "shutdown_runtime",
    "shutdown_tracing",
    "start_run",
    "target",
    "trace_llm",
    "trace_retrieval",
    "trace_skill",
    "trace_thought",
    "trace_tool",
    "update_run_status",
]

__version__ = "0.1.0"
