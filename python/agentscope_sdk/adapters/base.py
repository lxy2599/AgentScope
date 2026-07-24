"""Lifecycle bridge from external Agent events to AgentScope runtime and traces."""

from __future__ import annotations

import time
from collections import defaultdict
from contextlib import AbstractContextManager
from typing import Any

from opentelemetry import trace
from opentelemetry.trace import Status, StatusCode

from agentscope_sdk.runtime import RuntimeRun, build_run_resource_attributes
from agentscope_sdk.tracing import (
    record_final_answer,
    set_redacted_attribute,
    trace_llm,
    trace_skill,
    trace_thought,
    trace_tool,
)

from .events import NormalizedEvent, normalize_event


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
        self._tool_spans: dict[str, list[tuple[AbstractContextManager[Any], trace.Span]]] = defaultdict(list)
        self._llm_spans: dict[str, list[tuple[AbstractContextManager[Any], trace.Span]]] = defaultdict(list)
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
        self._root_cm = self.tracer.start_as_current_span("agent.run")
        self._root_span = self._root_cm.__enter__()
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
            **build_run_resource_attributes(run_id=run.id, trace_id=trace_id, agent_name=self.runtime.agent_name),
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

    def on_thought(self, stage: str, content: str, *, capture_content: bool = False) -> None:
        event = normalize_event(f"thought.{stage}", stage, result=content)
        with trace_thought(
            self.tracer,
            event.name,
            summary=event.output_summary,
            content=event.output_summary,
            capture_content=capture_content,
            stage=stage,
            **self._event_attrs(event),
        ):
            pass

    def on_llm_call(self, name: str, *, input_summary: str = "", output_summary: str = "", provider: str = "", model: str = "", input_tokens: int | None = None, output_tokens: int | None = None, success: bool = True, error: str = "") -> None:
        event = normalize_event("llm.call", name, arguments=input_summary, result=output_summary, success=success, error=error)
        with trace_llm(self.tracer, event.name, provider=provider, model=model, input_summary=event.input_summary, output_summary=event.output_summary, input_tokens=input_tokens, output_tokens=output_tokens, **self._event_attrs(event)) as span:
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "LLM call failed"))

    def on_llm_start(self, name: str, *, call_id: str = "", input_summary: str = "", provider: str = "", model: str = "") -> None:
        event = normalize_event("llm.call", name, arguments=input_summary)
        cm = trace_llm(self.tracer, event.name, provider=provider, model=model, input_summary=event.input_summary, **self._event_attrs(event))
        span = cm.__enter__()
        self._llm_spans[call_id or name].append((cm, span))

    def on_llm_finish(self, name: str, *, call_id: str = "", output_summary: str = "", input_tokens: int | None = None, output_tokens: int | None = None, success: bool = True, error: str = "") -> None:
        entries = self._llm_spans[call_id or name]
        if not entries:
            self.on_llm_call(name, output_summary=output_summary, input_tokens=input_tokens, output_tokens=output_tokens, success=success, error=error)
            return
        cm, span = entries.pop(0)
        if output_summary:
            set_redacted_attribute(span, "llm.output_summary", output_summary)
        if input_tokens is not None:
            set_redacted_attribute(span, "llm.input_tokens", input_tokens)
        if output_tokens is not None:
            set_redacted_attribute(span, "llm.output_tokens", output_tokens)
        if input_tokens is not None and output_tokens is not None:
            set_redacted_attribute(span, "llm.total_tokens", input_tokens + output_tokens)
        if not success:
            span.set_status(Status(StatusCode.ERROR, error or "LLM call failed"))
        cm.__exit__(None, None, None)

    def on_tool_start(self, name: str, arguments: Any = None, *, tool_id: str = "", tool_type: str = "function", side_effect_risk: str = "") -> None:
        event = normalize_event("tool.started", name, arguments=arguments)
        cm = trace_tool(self.tracer, event.name, tool_type=tool_type, arguments_summary=event.input_summary, side_effect_risk=side_effect_risk, **self._event_attrs(event))
        span = cm.__enter__()
        self._tool_spans[tool_id or name].append((cm, span))

    def on_tool_finish(self, name: str, result: Any = None, *, tool_id: str = "", success: bool = True, error: str = "", duration_s: float | None = None) -> None:
        event = normalize_event("tool.completed", name, result=result, success=success, error=error)
        entries = self._tool_spans[tool_id or name]
        if entries:
            cm, span = entries.pop(0)
            set_redacted_attribute(span, "tool.result_summary", event.output_summary)
            set_redacted_attribute(span, "tool.success", str(success).lower())
            if duration_s is not None:
                set_redacted_attribute(span, "tool.duration_ms", int(duration_s * 1000))
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "tool failed"))
            cm.__exit__(None, None, None)
            return
        with trace_tool(self.tracer, event.name, result_summary=event.output_summary, success=success, **self._event_attrs(event)) as span:
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "tool failed"))

    def on_memory_read(self, name: str, result: Any = None, *, arguments: Any = None, success: bool = True, error: str = "") -> None:
        self._record_generic(normalize_event("memory.read", name, arguments=arguments, result=result, success=success, error=error))

    def on_memory_write(self, name: str, result: Any = None, *, arguments: Any = None, success: bool = True, error: str = "") -> None:
        self._record_generic(normalize_event("memory.write", name, arguments=arguments, result=result, success=success, error=error))

    def on_skill_call(self, name: str, result: Any = None, *, arguments: Any = None, success: bool = True, error: str = "") -> None:
        event = normalize_event("skill.call", name, arguments=arguments, result=result, success=success, error=error)
        with trace_skill(self.tracer, event.name, input_summary=event.input_summary, output_summary=event.output_summary, success=success, **self._event_attrs(event)) as span:
            if not success:
                span.set_status(Status(StatusCode.ERROR, error or "skill failed"))

    def on_guardrail(self, name: str, result: Any = None, *, arguments: Any = None, blocked: bool = False, error: str = "") -> None:
        self._record_generic(normalize_event("guardrail.blocked" if blocked else "guardrail.checked", name, arguments=arguments, result=result, success=not blocked, error=error))

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
        semantic_kind: str = "INTERNAL",
        input_summary: Any = "",
        output_summary: Any = "",
        attributes: dict[str, Any] | None = None,
        success: bool = True,
        error: str = "",
    ) -> None:
        """Record a framework event that has no dedicated AgentScope helper."""
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
        with record_final_answer(self.tracer, "agent.answer", summary=output, output=output, **self._span_attrs):
            pass
        self._close_open_llms()
        self._close_open_tools()
        latency_ms = int((time.monotonic() - self._started_at) * 1000)
        result = self.runtime.finish_run(output=output, metrics=metrics, latency_ms=latency_ms)
        self._close_root(None, None, None)
        self.run = None
        return result

    def fail_run(self, error: str, *, attributes: dict[str, Any] | None = None) -> bool:
        self._ensure_active()
        self._close_open_llms(error)
        self._close_open_tools(error)
        if self._root_span is not None:
            self._root_span.set_status(Status(StatusCode.ERROR, error))
        result = self.runtime.fail_run(error=error, attributes=attributes or {})
        self._close_root(None, RuntimeError, RuntimeError(error))
        self.run = None
        return result

    def _record_generic(self, event: NormalizedEvent) -> None:
        with self.tracer.start_as_current_span(event.name) as span:
            set_redacted_attribute(span, "agentscope.span.kind", event.semantic_kind)
            set_redacted_attribute(span, "openinference.span.kind", event.semantic_kind)
            set_redacted_attribute(span, "event.input_summary", event.input_summary)
            set_redacted_attribute(span, "event.output_summary", event.output_summary)
            for key, value in self._event_attrs(event).items():
                set_redacted_attribute(span, key, value)
            if not event.success:
                span.set_status(Status(StatusCode.ERROR, event.error or "external agent event failed"))

    def _event_attrs(self, event: NormalizedEvent) -> dict[str, Any]:
        return {**self._span_attrs, **event.attributes}

    def _close_open_tools(self, error: str = "") -> None:
        for entries in self._tool_spans.values():
            for cm, span in entries:
                if error:
                    span.set_status(Status(StatusCode.ERROR, error))
                cm.__exit__(None, None, None)
        self._tool_spans.clear()

    def _close_open_llms(self, error: str = "") -> None:
        for entries in self._llm_spans.values():
            for cm, span in entries:
                if error:
                    span.set_status(Status(StatusCode.ERROR, error))
                cm.__exit__(None, None, None)
        self._llm_spans.clear()

    def _close_root(self, exc_type: Any, exc: Any, traceback: Any) -> None:
        if self._root_cm is not None:
            self._root_cm.__exit__(exc_type, exc, traceback)
        self._root_cm = None
        self._root_span = None

    def _ensure_active(self) -> None:
        if self.run is None or self._root_span is None:
            raise RuntimeError("adapter session has no active run")

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
