"""Lifecycle bridge from external Agent events to AgentScope runtime and traces."""

from __future__ import annotations

import logging
import time
from collections import defaultdict
from contextlib import AbstractContextManager
from dataclasses import dataclass
from typing import Any

from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.trace import Status, StatusCode

from agentscope_sdk.runtime import RuntimeRun, build_run_resource_attributes
from agentscope_sdk.tracing import (
    _start_llm_span,
    _start_tool_span,
    force_flush,
    record_final_answer,
    set_llm_metrics,
    set_redacted_attribute,
    trace_llm,
    trace_skill,
    trace_thought,
    trace_tool,
)
from agentscope_sdk.tracing_contract import SemanticKind, SpanAttribute, SpanName

from .events import NormalizedEvent, normalize_event

_logger = logging.getLogger(__name__)


@dataclass
class _ActiveSpan:
    span: trace.Span
    turn_id: str = ""


@dataclass
class _ActiveTurn:
    turn_id: str
    turn_index: int
    span: trace.Span
    context: Context
    llm_count: int = 0
    tool_count: int = 0


class AdapterSession:
    """Own one external Agent task and keep its runtime run and OTel trace aligned.

    The session is deliberately synchronous because the supported Hermes
    callbacks run in its task thread. Adapter errors are contained by callers;
    no callback exception should alter the upstream agent's execution.
    """

    def __init__(self, runtime: Any, tracer: trace.Tracer, *, metadata: dict[str, Any] | None = None) -> None:
        self.runtime = runtime
        self.tracer = tracer
        self.metadata = dict(metadata or {})
        self.run: RuntimeRun | None = None
        self.last_run_id = ""
        self._root_cm: AbstractContextManager[Any] | None = None
        self._root_span: trace.Span | None = None
        self._root_context: Context | None = None
        self._active_turn: _ActiveTurn | None = None
        self._tool_spans: dict[str, list[_ActiveSpan]] = defaultdict(list)
        self._llm_spans: dict[str, list[_ActiveSpan]] = defaultdict(list)
        self._next_turn_index = 1
        self._span_attrs: dict[str, Any] = {}
        self._started_at = 0.0

    def start_run(
        self,
        input: str,
        *,
        session_id: str = "",
        conversation_id: str = "",
        task_id: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> RuntimeRun:
        if self.run is not None:
            raise RuntimeError("adapter session already has an active run")
        self._started_at = time.monotonic()
        self._root_cm = self.tracer.start_as_current_span(SpanName.RUN)
        self._root_span = self._root_cm.__enter__()
        self._root_context = trace.set_span_in_context(self._root_span)
        self._active_turn = None
        self._next_turn_index = 1
        trace_id = format(self._root_span.get_span_context().trace_id, "032x")
        run_metadata = {**self.metadata, **(metadata or {})}
        run = self.runtime.start_run(
            input=input,
            metadata=run_metadata,
            trace_id=trace_id,
            session_id=session_id,
            conversation_id=conversation_id,
            task_id=task_id,
        )
        if run is None or not run.id:
            self._root_span.set_status(Status(StatusCode.ERROR, "AgentScope runtime start failed"))
            self._close_root(None, RuntimeError, RuntimeError("AgentScope runtime start failed"))
            raise RuntimeError("AgentScope runtime start failed")
        self.run = run
        self.last_run_id = str(run.id)
        self._span_attrs = {
            **build_run_resource_attributes(
                run_id=run.id,
                trace_id=trace_id,
                agent_name=self.runtime.agent_name,
                agent_version=getattr(self.runtime, "_agent_version", ""),
                environment=getattr(self.runtime, "_environment", ""),
            ),
            "agentscope.agent.id": self.runtime.agent_id,
            "agentscope.agent.instance_id": self.runtime.instance_id,
            "agentscope.adapter": self.metadata.get("adapter_name", "external-agent"),
        }
        for key, value in self._span_attrs.items():
            set_redacted_attribute(self._root_span, key, value)
        for key, value in self.metadata.items():
            if isinstance(value, (str, bool, int, float, list, tuple)):
                set_redacted_attribute(self._root_span, key, value)
        return run

    def start_turn(self, turn_id: str = "", *, turn_index: int | None = None) -> tuple[str, int]:
        """Start one explicit Agent decision turn under the active run."""
        self._ensure_active()
        if self._active_turn is not None:
            raise RuntimeError("adapter session already has an active turn")
        resolved_index = turn_index if turn_index is not None else self._next_turn_index
        if resolved_index < 1:
            raise ValueError("turn_index must be positive")
        resolved_id = turn_id or f"turn-{resolved_index}"
        span = self.tracer.start_span(SpanName.TURN, context=self._root_context)
        set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.TURN)
        for key, value in self._span_attrs.items():
            set_redacted_attribute(span, key, value)
        set_redacted_attribute(span, SpanAttribute.TURN_ID, resolved_id)
        set_redacted_attribute(span, SpanAttribute.TURN_INDEX, resolved_index)
        context = trace.set_span_in_context(span, self._root_context)
        self._active_turn = _ActiveTurn(resolved_id, resolved_index, span, context)
        self._next_turn_index = max(self._next_turn_index, resolved_index + 1)
        return resolved_id, resolved_index

    @property
    def has_active_turn(self) -> bool:
        """Whether the session currently has an open explicit Turn."""
        return self._active_turn is not None

    def finish_turn(
        self,
        outcome: str = "continue",
        *,
        error: str = "",
        attributes: dict[str, Any] | None = None,
    ) -> bool:
        """Finish the active turn and record its aggregate execution metadata."""
        self._ensure_active()
        if self._active_turn is None:
            return False
        active_turn = self._active_turn
        self._close_open_llms(error, turn_id=active_turn.turn_id)
        self._close_open_tools(error, turn_id=active_turn.turn_id)
        set_redacted_attribute(active_turn.span, SpanAttribute.TURN_OUTCOME, outcome)
        set_redacted_attribute(active_turn.span, SpanAttribute.TURN_LLM_COUNT, active_turn.llm_count)
        set_redacted_attribute(active_turn.span, SpanAttribute.TURN_TOOL_COUNT, active_turn.tool_count)
        for key, value in (attributes or {}).items():
            set_redacted_attribute(active_turn.span, key, value)
        if error or outcome in {"error", "cancelled"}:
            active_turn.span.set_status(Status(StatusCode.ERROR, error or outcome))
        active_turn.span.end()
        self._active_turn = None
        return True

    def on_thought(self, stage: str, content: str, *, capture_content: bool = False) -> None:
        if not self._can_record("thought"):
            return
        event = normalize_event(f"thought.{stage}", stage, result=content)
        with trace_thought(
            self.tracer,
            event.name,
            summary=event.output_summary,
            content=event.output_summary,
            capture_content=capture_content,
            parent_context=self._parent_context(),
            stage=stage,
            **self._event_attrs(event),
        ):
            pass

    def on_llm_call(
        self,
        name: str,
        *,
        input_summary: str = "",
        output_summary: str = "",
        provider: str = "",
        model: str = "",
        input_tokens: int | None = None,
        output_tokens: int | None = None,
        total_tokens: int | None = None,
        cache_read_tokens: int | None = None,
        cache_write_tokens: int | None = None,
        ttft_ms: int | None = None,
        estimated_cost: float | None = None,
        actual_cost: float | None = None,
        duration_s: float | None = None,
        success: bool = True,
        error: str = "",
    ) -> None:
        if not self._can_record("llm"):
            return
        event = normalize_event(
            "llm.call", name, arguments=input_summary, result=output_summary, success=success, error=error
        )
        self._increment_turn_count("llm")
        with trace_llm(
            self.tracer,
            event.name,
            provider=provider,
            model=model,
            input_summary=event.input_summary,
            output_summary=event.output_summary,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            total_tokens=total_tokens,
            cache_read_tokens=cache_read_tokens,
            cache_write_tokens=cache_write_tokens,
            ttft_ms=ttft_ms,
            estimated_cost=estimated_cost,
            actual_cost=actual_cost,
            parent_context=self._parent_context(),
            **self._event_attrs(event),
        ) as span:
            if duration_s is not None:
                set_redacted_attribute(
                    span,
                    "llm.duration_ms",
                    max(1, int(duration_s * 1000)),
                )
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "LLM call failed"))

    def on_llm_start(
        self, name: str, *, call_id: str = "", input_summary: str = "", provider: str = "", model: str = ""
    ) -> None:
        if not self._can_record("llm start"):
            return
        event = normalize_event("llm.call", name, arguments=input_summary)
        span = _start_llm_span(
            self.tracer,
            event.name,
            parent_context=self._parent_context(),
            provider=provider,
            model=model,
            input_summary=event.input_summary,
            **self._event_attrs(event),
        )
        self._increment_turn_count("llm")
        self._llm_spans[call_id or name].append(_ActiveSpan(span, self._turn_id()))

    def on_llm_finish(
        self,
        name: str,
        *,
        call_id: str = "",
        output_summary: str = "",
        input_tokens: int | None = None,
        output_tokens: int | None = None,
        total_tokens: int | None = None,
        cache_read_tokens: int | None = None,
        cache_write_tokens: int | None = None,
        ttft_ms: int | None = None,
        estimated_cost: float | None = None,
        actual_cost: float | None = None,
        success: bool = True,
        error: str = "",
    ) -> None:
        if not self._can_record("llm finish"):
            return
        key = call_id or name
        entries = self._llm_spans.get(key)
        if not entries:
            if call_id:
                _logger.debug("Ignoring unmatched LLM finish for call_id=%s", call_id)
                return
            self.on_llm_call(
                name,
                output_summary=output_summary,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                total_tokens=total_tokens,
                cache_read_tokens=cache_read_tokens,
                cache_write_tokens=cache_write_tokens,
                ttft_ms=ttft_ms,
                estimated_cost=estimated_cost,
                actual_cost=actual_cost,
                success=success,
                error=error,
            )
            return
        active = entries.pop(0)
        span = active.span
        if output_summary:
            set_redacted_attribute(span, "llm.output_summary", output_summary)
        set_llm_metrics(
            span,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            total_tokens=total_tokens,
            cache_read_tokens=cache_read_tokens,
            cache_write_tokens=cache_write_tokens,
            ttft_ms=ttft_ms,
            estimated_cost=estimated_cost,
            actual_cost=actual_cost,
        )
        if not success:
            span.set_status(Status(StatusCode.ERROR, error or "LLM call failed"))
        span.end()

    def on_tool_start(
        self,
        name: str,
        arguments: Any = None,
        *,
        tool_id: str = "",
        tool_type: str = "function",
        side_effect_risk: str = "",
    ) -> None:
        if not self._can_record("tool start"):
            return
        event = normalize_event("tool.started", name, arguments=arguments)
        span = _start_tool_span(
            self.tracer,
            event.name,
            parent_context=self._parent_context(),
            tool_type=tool_type,
            arguments_summary=event.input_summary,
            side_effect_risk=side_effect_risk,
            **self._event_attrs(event),
        )
        self._increment_turn_count("tool")
        self._tool_spans[tool_id or name].append(_ActiveSpan(span, self._turn_id()))

    def on_tool_finish(
        self,
        name: str,
        result: Any = None,
        *,
        tool_id: str = "",
        success: bool = True,
        error: str = "",
        duration_s: float | None = None,
    ) -> None:
        if not self._can_record("tool finish"):
            return
        event = normalize_event("tool.completed", name, result=result, success=success, error=error)
        key = tool_id or name
        entries = self._tool_spans.get(key)
        if entries:
            active = entries.pop(0)
            span = active.span
            set_redacted_attribute(span, "tool.result_summary", event.output_summary)
            set_redacted_attribute(span, "tool.success", str(success).lower())
            if duration_s is not None:
                set_redacted_attribute(span, "tool.duration_ms", int(duration_s * 1000))
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "tool failed"))
            span.end()
            return
        if tool_id:
            _logger.debug("Ignoring unmatched tool finish for tool_id=%s", tool_id)
            return
        self._increment_turn_count("tool")
        with trace_tool(
            self.tracer,
            event.name,
            result_summary=event.output_summary,
            success=success,
            parent_context=self._parent_context(),
            **self._event_attrs(event),
        ) as span:
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "tool failed"))

    def on_memory_read(
        self, name: str, result: Any = None, *, arguments: Any = None, success: bool = True, error: str = ""
    ) -> None:
        if not self._can_record("memory read"):
            return
        self._record_generic(
            normalize_event("memory.read", name, arguments=arguments, result=result, success=success, error=error)
        )

    def on_memory_write(
        self, name: str, result: Any = None, *, arguments: Any = None, success: bool = True, error: str = ""
    ) -> None:
        if not self._can_record("memory write"):
            return
        self._record_generic(
            normalize_event("memory.write", name, arguments=arguments, result=result, success=success, error=error)
        )

    def on_skill_call(
        self, name: str, result: Any = None, *, arguments: Any = None, success: bool = True, error: str = ""
    ) -> None:
        if not self._can_record("skill"):
            return
        event = normalize_event("skill.call", name, arguments=arguments, result=result, success=success, error=error)
        with trace_skill(
            self.tracer,
            event.name,
            input_summary=event.input_summary,
            output_summary=event.output_summary,
            success=success,
            parent_context=self._parent_context(),
            **self._event_attrs(event),
        ) as span:
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "skill failed"))

    def on_guardrail(
        self, name: str, result: Any = None, *, arguments: Any = None, blocked: bool = False, error: str = ""
    ) -> None:
        if not self._can_record("guardrail"):
            return
        self._record_generic(
            normalize_event(
                "guardrail.blocked" if blocked else "guardrail.checked",
                name,
                arguments=arguments,
                result=result,
                success=not blocked,
                error=error,
            )
        )

    def on_state_update(
        self,
        status: str,
        *,
        message: str = "",
        attributes: dict[str, Any] | None = None,
    ) -> bool:
        """Mirror an external framework state into the active AgentScope run."""
        self._ensure_active()
        return bool(
            self.runtime.update_run_status(
                status,
                message=message,
                attributes=attributes or {},
            )
        )

    def on_external_event(
        self,
        name: str,
        *,
        semantic_kind: str = SemanticKind.INTERNAL,
        input_summary: Any = "",
        output_summary: Any = "",
        attributes: dict[str, Any] | None = None,
        success: bool = True,
        error: str = "",
    ) -> None:
        """Record a framework event that has no dedicated AgentScope helper."""
        if not self._can_record("external event"):
            return
        event = NormalizedEvent(
            semantic_kind=semantic_kind,
            name=name,
            input_summary=self._summary(input_summary),
            output_summary=self._summary(output_summary),
            attributes=dict(attributes or {}),
            success=success,
            error=error,
        )
        self._record_generic(event)

    def finish_run(self, output: str, *, metrics: dict[str, Any] | None = None) -> bool:
        self._ensure_active()
        self._close_open_llms()
        self._close_open_tools()
        with record_final_answer(
            self.tracer,
            SpanName.ANSWER,
            summary=output,
            output=output,
            parent_context=self._parent_context(),
            **self._event_attrs(NormalizedEvent(SemanticKind.FINAL_ANSWER, SpanName.ANSWER, output_summary=output)),
        ):
            pass
        self.finish_turn("final")
        latency_ms = int((time.monotonic() - self._started_at) * 1000)
        self._close_root(None, None, None)
        self._flush_spans()
        result = self.runtime.finish_run(output=output, metrics=metrics, latency_ms=latency_ms)
        self.run = None
        return result

    def fail_run(self, error: str, *, attributes: dict[str, Any] | None = None) -> bool:
        self._ensure_active()
        self._close_open_llms(error)
        self._close_open_tools(error)
        self.finish_turn("error", error=error)
        if self._root_span is not None:
            self._root_span.set_status(Status(StatusCode.ERROR, error))
        self._close_root(None, RuntimeError, RuntimeError(error))
        self._flush_spans()
        result = self.runtime.fail_run(error=error, attributes=attributes or {})
        self.run = None
        return result

    def _record_generic(self, event: NormalizedEvent) -> None:
        with self.tracer.start_as_current_span(event.name, context=self._parent_context()) as span:
            set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, event.semantic_kind)
            set_redacted_attribute(span, SpanAttribute.OPENINFERENCE_KIND, event.semantic_kind)
            set_redacted_attribute(span, "event.input_summary", event.input_summary)
            set_redacted_attribute(span, "event.output_summary", event.output_summary)
            for key, value in self._event_attrs(event).items():
                set_redacted_attribute(span, key, value)
            if not event.success:
                span.set_status(Status(StatusCode.ERROR, event.error or "external agent event failed"))

    def _event_attrs(self, event: NormalizedEvent) -> dict[str, Any]:
        return {**self._span_attrs, **self._turn_attrs(), **event.attributes}

    def _close_open_tools(self, error: str = "", *, turn_id: str | None = None) -> None:
        self._close_open_spans(self._tool_spans, error, turn_id=turn_id)

    def _close_open_llms(self, error: str = "", *, turn_id: str | None = None) -> None:
        self._close_open_spans(self._llm_spans, error, turn_id=turn_id)

    @staticmethod
    def _close_open_spans(
        spans_by_key: dict[str, list[_ActiveSpan]],
        error: str,
        *,
        turn_id: str | None,
    ) -> None:
        for key, entries in list(spans_by_key.items()):
            remaining: list[_ActiveSpan] = []
            for active in entries:
                if turn_id is not None and active.turn_id != turn_id:
                    remaining.append(active)
                    continue
                if error:
                    active.span.set_status(Status(StatusCode.ERROR, error))
                active.span.end()
            if remaining:
                spans_by_key[key] = remaining
            else:
                spans_by_key.pop(key, None)

    def _close_root(self, exc_type: Any, exc: Any, traceback: Any) -> None:
        if self._root_cm is not None:
            self._root_cm.__exit__(exc_type, exc, traceback)
        self._root_cm = None
        self._root_span = None
        self._root_context = None

    def _flush_spans(self) -> None:
        force_flush()

    def _ensure_active(self) -> None:
        if self.run is None or self._root_span is None:
            raise RuntimeError("adapter session has no active run")

    def _can_record(self, event_name: str) -> bool:
        if self.run is not None and self._root_span is not None:
            return True
        _logger.debug("Ignoring %s after AdapterSession run ended", event_name)
        return False

    def _parent_context(self) -> Context | None:
        if self._active_turn is not None:
            return self._active_turn.context
        return self._root_context

    def _turn_attrs(self) -> dict[str, Any]:
        if self._active_turn is None:
            return {}
        return {
            SpanAttribute.TURN_ID: self._active_turn.turn_id,
            SpanAttribute.TURN_INDEX: self._active_turn.turn_index,
        }

    def _turn_id(self) -> str:
        return self._active_turn.turn_id if self._active_turn is not None else ""

    def _increment_turn_count(self, kind: str) -> None:
        if self._active_turn is None:
            return
        if kind == "llm":
            self._active_turn.llm_count += 1
        elif kind == "tool":
            self._active_turn.tool_count += 1

    @staticmethod
    def _summary(value: Any, limit: int = 4096) -> str:
        if value is None:
            return ""
        if isinstance(value, str):
            return value[:limit]
        try:
            import json

            return json.dumps(value, ensure_ascii=False, sort_keys=True)[:limit]
        except (TypeError, ValueError):
            return str(value)[:limit]
