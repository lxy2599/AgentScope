"""Shared AgentScope tracing protocol values.

These values are persisted in trace attributes and consumed by the backend,
so they are kept in one small module instead of being repeated throughout the
SDK implementation.
"""

from __future__ import annotations

from enum import StrEnum


class SemanticKind(StrEnum):
    """AgentScope semantic kinds stored on OpenTelemetry spans."""

    AGENT = "AGENT"
    TURN = "TURN"
    LLM = "LLM"
    TOOL = "TOOL"
    RETRIEVER = "RETRIEVER"
    SKILL = "SKILL"
    THOUGHT = "THOUGHT"
    MEMORY = "MEMORY"
    GUARDRAIL = "GUARDRAIL"
    FINAL_ANSWER = "FINAL_ANSWER"
    INTERNAL = "INTERNAL"
    SDK_EXPORT_METRICS = "SDK_EXPORT_METRICS"


class SpanAttribute:
    """Stable AgentScope and semantic attribute names."""

    SEMANTIC_KIND = "agentscope.span.kind"
    RUN_ID = "agentscope.run.id"
    TRACE_ID = "agentscope.trace.id"
    TURN_ID = "agentscope.turn.id"
    TURN_INDEX = "agentscope.turn.index"
    TURN_OUTCOME = "agentscope.turn.outcome"
    TURN_LLM_COUNT = "agentscope.turn.llm_count"
    TURN_TOOL_COUNT = "agentscope.turn.tool_count"

    OPENINFERENCE_KIND = "openinference.span.kind"
    LLM_PROVIDER = "llm.provider"
    LLM_MODEL = "llm.model"
    TOOL_NAME = "tool.name"


class SpanName:
    """Stable names for SDK-owned spans."""

    RUN = "agent.run"
    TURN = "agent.turn"
    ANSWER = "agent.answer"
    ASSOCIATE = "agentscope.associate"
    EXPORT_METRICS = "agentscope.sdk.export_metrics"
