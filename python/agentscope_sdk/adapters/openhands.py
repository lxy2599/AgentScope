"""Non-invasive observability adapter for the OpenHands Software Agent SDK."""

from __future__ import annotations

import contextlib
import hashlib
import json
import logging
import time
from collections.abc import Callable
from typing import Any

from agentscope_sdk.tracing_contract import SemanticKind

from .base import AdapterSession

OPENHANDS_UPSTREAM_COMMIT = "a375c66aaf29c8d3b6553c9164e65ed917149e81"

_logger = logging.getLogger(__name__)

_STATE_MAP = {
    "idle": "running",
    "running": "thinking",
    "paused": "waiting_user",
    "waiting_for_confirmation": "waiting_user",
    "stuck": "retrying",
}


class OpenHandsAdapter:
    """Translate public OpenHands callbacks into AgentScope runtime and spans.

    The adapter intentionally has no import-time dependency on OpenHands. Hosts pass
    ``adapter.callbacks`` and ``adapter.token_callbacks`` to the public Conversation
    constructor, so an observability failure cannot alter OpenHands classes or state.
    """

    def __init__(
        self,
        session: AdapterSession,
        *,
        capture_reasoning: bool = False,
        capture_system_prompt: bool = False,
        metrics_provider: Callable[[], Any] | None = None,
        metrics_cost_is_estimate: bool = False,
    ) -> None:
        self.session = session
        self.capture_reasoning = capture_reasoning
        self.capture_system_prompt = capture_system_prompt
        self.metrics_provider = metrics_provider
        self.metrics_cost_is_estimate = metrics_cost_is_estimate
        self._started_at = 0.0
        self._first_token_seen = False
        self._final_output = ""
        self._fatal_error = ""
        self._event_ids: set[str] = set()
        self._llm_response_ids: set[str] = set()
        self._tool_ids: set[str] = set()
        self._token_parts: list[str] = []
        self._usage: dict[str, int | float] = {}
        self._ttft_ms: int | None = None
        self._llm_turn_started_at = 0.0

    @property
    def callbacks(self) -> list[Any]:
        return [self.on_event]

    @property
    def token_callbacks(self) -> list[Any]:
        return [self.on_token]

    def start(
        self,
        prompt: str,
        *,
        conversation_id: str = "",
        task_id: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> Any:
        self.session.metadata.setdefault("adapter_name", "openhands")
        self.session.metadata.setdefault("adapter_version", "1")
        self.session.metadata.setdefault("framework", "openhands")
        self.session.metadata.setdefault(
            "openhands.upstream_commit",
            OPENHANDS_UPSTREAM_COMMIT,
        )
        self._started_at = time.monotonic()
        self._llm_turn_started_at = self._started_at
        return self.session.start_run(
            prompt,
            conversation_id=conversation_id,
            session_id=conversation_id,
            task_id=task_id,
            metadata=metadata,
        )

    def run(
        self,
        conversation: Any,
        prompt: str,
        *,
        task_id: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> str:
        """Execute one real OpenHands turn and close the linked AgentScope run."""
        conversation_id = str(
            getattr(conversation, "id", "") or getattr(getattr(conversation, "state", None), "id", "") or ""
        )
        self.start(
            prompt,
            conversation_id=conversation_id,
            task_id=task_id,
            metadata=metadata,
        )
        try:
            conversation.send_message(prompt)
            conversation.run()
            if self._fatal_error:
                raise RuntimeError(self._fatal_error)
            return self.finish()
        except Exception as exc:
            if self.session.run is not None:
                self.session.fail_run(
                    str(exc),
                    attributes={
                        "framework": "openhands",
                        "openhands.upstream_commit": OPENHANDS_UPSTREAM_COMMIT,
                    },
                )
            raise

    def finish(self, output: str | None = None) -> str:
        final_output = output if output is not None else self._final_output
        if not final_output:
            final_output = "".join(self._token_parts).strip()
        self.session.finish_run(final_output)
        return final_output

    def fail(self, error: str | None = None) -> None:
        self.session.fail_run(
            error or self._fatal_error or "OpenHands conversation failed",
            attributes={
                "framework": "openhands",
                "openhands.upstream_commit": OPENHANDS_UPSTREAM_COMMIT,
            },
        )

    def on_event(self, event: Any) -> None:
        """Public Conversation callback. All adapter exceptions are contained."""
        try:
            self._handle_event(event)
        except Exception:
            _logger.debug(
                "AgentScope OpenHands event callback failed: %s",
                type(event).__name__,
                exc_info=True,
            )

    def on_token(self, chunk: Any) -> None:
        """Public token callback with TTFT capture and exception containment."""
        try:
            parts = self._token_content(chunk)
            if not parts:
                return
            if not self._first_token_seen:
                self._first_token_seen = True
                self._ttft_ms = max(
                    0,
                    int((time.monotonic() - self._started_at) * 1000),
                )
            self._token_parts.extend(parts)
        except Exception:
            _logger.debug("AgentScope OpenHands token callback failed", exc_info=True)

    def _handle_event(self, event: Any) -> None:
        event_id = str(getattr(event, "id", "") or "")
        if event_id and event_id in self._event_ids:
            return
        if event_id:
            self._event_ids.add(event_id)

        event_name = type(event).__name__
        if event_name == "MessageEvent":
            self._on_message(event)
        elif event_name == "ActionEvent":
            self._on_action(event)
        elif event_name == "ObservationEvent":
            self._on_observation(event)
        elif event_name in {"AgentErrorEvent", "UserRejectObservation"}:
            self._on_tool_error(event)
        elif event_name == "ConversationErrorEvent":
            self._on_conversation_error(event)
        elif event_name == "ConversationStateUpdateEvent":
            self._on_state_update(event)
        elif event_name in {"PauseEvent", "InterruptEvent"}:
            self.session.on_state_update(
                "waiting_user",
                message=f"OpenHands {event_name}",
                attributes=self._identity(event),
            )
        elif event_name == "SystemPromptEvent":
            self._on_system_prompt(event)
        elif event_name in {
            "Condensation",
            "CondensationRequest",
            "CondensationSummaryEvent",
        }:
            self._on_condensation(event)
        elif event_name == "LLMCompletionLogEvent":
            self._on_llm_completion(event)
        else:
            self.session.on_external_event(
                f"openhands.event.{event_name}",
                attributes=self._identity(event),
            )

    def _on_message(self, event: Any) -> None:
        source = str(getattr(event, "source", "") or "unknown")
        content = self._message_text(event)
        response_id = str(getattr(event, "llm_response_id", "") or "")
        if source == "agent":
            self._ensure_turn(response_id)
        reasoning = str(getattr(event, "reasoning_content", "") or "")
        if reasoning:
            self.session.on_thought(
                "reasoning",
                reasoning,
                capture_content=self.capture_reasoning,
            )
        skills = [str(item) for item in (getattr(event, "activated_skills", None) or [])]
        for skill in skills:
            self.session.on_skill_call(
                skill,
                "activated",
                arguments={"source": "OpenHands MessageEvent"},
            )

        if source == "agent":
            self._record_llm_response(response_id, content)
            self._final_output = content or self._final_output
        else:
            self.session.on_external_event(
                "openhands.message.user",
                semantic_kind=SemanticKind.INTERNAL,
                input_summary=content,
                attributes=self._identity(event),
            )

    def _on_action(self, event: Any) -> None:
        name = str(getattr(event, "tool_name", "") or "unknown")
        tool_id = str(getattr(event, "tool_call_id", "") or name)
        response_id = str(getattr(event, "llm_response_id", "") or "")
        self._ensure_turn(response_id or f"action.{tool_id}")
        thought = self._text_parts(getattr(event, "thought", None))
        reasoning = str(getattr(event, "reasoning_content", "") or "")
        if thought or reasoning:
            self.session.on_thought(
                "tool_planning",
                "\n".join(value for value in (thought, reasoning) if value),
                capture_content=self.capture_reasoning,
            )
        self._record_llm_response(response_id, thought or f"call {name}")
        arguments = self._public_dump(getattr(event, "action", None))
        if not arguments:
            arguments = self._tool_call_arguments(getattr(event, "tool_call", None))
        if name.lower() == "finish":
            final_message = self._result_text(arguments)
            if final_message:
                self._final_output = final_message
        risk = str(getattr(event, "security_risk", "") or "")
        self.session.on_tool_start(
            name,
            arguments,
            tool_id=tool_id,
            tool_type="openhands",
            side_effect_risk=risk,
        )
        self._tool_ids.add(tool_id)

    def _on_observation(self, event: Any) -> None:
        name = str(getattr(event, "tool_name", "") or "unknown")
        tool_id = str(getattr(event, "tool_call_id", "") or name)
        if tool_id not in self._tool_ids:
            _logger.debug("Ignoring unmatched OpenHands tool observation for tool_id=%s", tool_id)
            return
        result = self._public_dump(getattr(event, "observation", None))
        success, error = self._observation_status(result)
        if name.lower() == "finish":
            final_message = self._result_text(result)
            if final_message:
                self._final_output = final_message
        self.session.on_tool_finish(
            name,
            result,
            tool_id=tool_id,
            success=success,
            error=error,
        )
        self._tool_ids.discard(tool_id)
        self._llm_turn_started_at = time.monotonic()
        if not self._tool_ids:
            self.session.finish_turn("continue")

    def _on_tool_error(self, event: Any) -> None:
        name = str(getattr(event, "tool_name", "") or "unknown")
        tool_id = str(getattr(event, "tool_call_id", "") or name)
        if tool_id not in self._tool_ids:
            _logger.debug("Ignoring unmatched OpenHands tool error for tool_id=%s", tool_id)
            return
        error = str(getattr(event, "error", "") or getattr(event, "rejection_reason", "") or "OpenHands tool failed")
        self.session.on_tool_finish(
            name,
            {"error": error},
            tool_id=tool_id,
            success=False,
            error=error,
        )
        self._tool_ids.discard(tool_id)
        self._llm_turn_started_at = time.monotonic()
        if not self._tool_ids:
            self.session.finish_turn("error", error=error)

    def _ensure_turn(self, turn_id: str = "") -> None:
        if self.session.run is None or self.session.has_active_turn:
            return
        self.session.start_turn(f"openhands.turn.{turn_id}" if turn_id else "")

    def _on_conversation_error(self, event: Any) -> None:
        code = str(getattr(event, "code", "") or "conversation_error")
        detail = str(getattr(event, "detail", "") or code)
        self._fatal_error = detail
        self.session.on_external_event(
            "openhands.conversation_error",
            attributes={**self._identity(event), "error.code": code},
            success=False,
            error=detail,
        )

    def _on_state_update(self, event: Any) -> None:
        key = str(getattr(event, "key", "") or "")
        value = getattr(event, "value", "")
        state = ""
        if key == "execution_status":
            state = str(value)
        elif key == "full_state" and isinstance(value, dict):
            state = str(value.get("execution_status") or "")
        mapped = _STATE_MAP.get(state)
        if mapped:
            self.session.on_state_update(
                mapped,
                message=f"OpenHands state: {state}",
                attributes={**self._identity(event), "openhands.state": state},
            )
        else:
            self.session.on_external_event(
                "openhands.state_update",
                attributes={
                    **self._identity(event),
                    "openhands.state.key": key,
                    "openhands.state.value": self._summary(value, 512),
                },
            )

    def _on_system_prompt(self, event: Any) -> None:
        prompt = str(getattr(getattr(event, "system_prompt", None), "text", "") or "")
        dynamic_context = str(getattr(getattr(event, "dynamic_context", None), "text", "") or "")
        tools = [str(getattr(tool, "name", "") or "") for tool in (getattr(event, "tools", None) or [])]
        prompt_hash = hashlib.sha256(prompt.encode("utf-8")).hexdigest()
        output = prompt if self.capture_system_prompt else ""
        self.session.on_external_event(
            "openhands.system_prompt",
            semantic_kind=SemanticKind.INTERNAL,
            output_summary=output,
            attributes={
                **self._identity(event),
                "prompt.sha256": prompt_hash,
                "prompt.captured": self.capture_system_prompt,
                "prompt.dynamic_context_present": bool(dynamic_context),
                "prompt.tool_names": [name for name in tools if name],
            },
        )

    def _on_condensation(self, event: Any) -> None:
        summary = str(getattr(event, "summary", "") or type(event).__name__)
        forgotten = getattr(event, "forgotten_event_ids", None) or []
        self.session.on_thought(
            "context_condensation",
            summary,
            capture_content=self.capture_reasoning,
        )
        self.session.on_external_event(
            "openhands.condensation",
            attributes={
                **self._identity(event),
                "openhands.forgotten_event_count": len(forgotten),
            },
        )

    def _on_llm_completion(self, event: Any) -> None:
        payload: dict[str, Any] = {}
        with contextlib.suppress(TypeError, ValueError):
            payload = json.loads(str(getattr(event, "log_data", "") or "{}"))
        usage = payload.get("usage") if isinstance(payload, dict) else {}
        if isinstance(usage, dict):
            details = usage.get("prompt_tokens_details")
            if not isinstance(details, dict):
                details = {}
            self._usage = {
                "input_tokens": self._int_value(
                    usage,
                    "prompt_tokens",
                    "input_tokens",
                ),
                "output_tokens": self._int_value(
                    usage,
                    "completion_tokens",
                    "output_tokens",
                ),
                "total_tokens": self._int_value(usage, "total_tokens"),
                "cache_read_tokens": self._int_value(
                    usage,
                    "cache_read_input_tokens",
                    "cached_tokens",
                )
                or self._int_value(details, "cached_tokens"),
                "cache_write_tokens": self._int_value(
                    usage,
                    "cache_creation_input_tokens",
                    "cache_write_tokens",
                ),
            }
        actual_cost = payload.get("response_cost") or payload.get("cost")
        if isinstance(actual_cost, (int, float)):
            self._usage["actual_cost"] = float(actual_cost)
        self.session.on_external_event(
            "openhands.llm_completion",
            semantic_kind=SemanticKind.INTERNAL,
            attributes={
                **self._identity(event),
                "llm.model": str(getattr(event, "model_name", "") or ""),
                "llm.usage_id": str(getattr(event, "usage_id", "") or ""),
                **self._usage,
            },
        )

    def _record_llm_response(self, response_id: str, output: str) -> None:
        key = response_id or f"response-{len(self._llm_response_ids) + 1}"
        if key in self._llm_response_ids:
            return
        self._llm_response_ids.add(key)
        usage = dict(self._usage)
        native_duration = None
        if self.metrics_provider is not None:
            metrics_usage, native_duration = self._metrics_for_response(key)
            usage.update(metrics_usage)
        observed_duration = native_duration
        if self._llm_turn_started_at > 0:
            observed_duration = observed_duration or max(0.001, time.monotonic() - self._llm_turn_started_at)
        self.session.on_llm_call(
            f"openhands.{key}",
            output_summary=output,
            provider=str(self.session.metadata.get("model_provider", "") or ""),
            model=str(self.session.metadata.get("model_name", "") or ""),
            input_tokens=self._nonnegative_int(usage.get("input_tokens")),
            output_tokens=self._nonnegative_int(usage.get("output_tokens")),
            total_tokens=self._nonnegative_int(usage.get("total_tokens")),
            cache_read_tokens=self._nonnegative_int(usage.get("cache_read_tokens")),
            cache_write_tokens=self._nonnegative_int(usage.get("cache_write_tokens")),
            ttft_ms=self._ttft_ms,
            estimated_cost=usage.get("estimated_cost"),
            actual_cost=usage.get("actual_cost"),
            duration_s=observed_duration,
        )
        self._ttft_ms = None
        self._llm_turn_started_at = 0.0

    def _metrics_for_response(self, response_id: str) -> tuple[dict[str, int | float], float | None]:
        try:
            metrics = self.metrics_provider() if self.metrics_provider else None
        except Exception:
            _logger.debug("OpenHands metrics provider failed", exc_info=True)
            return {}, None
        if metrics is None:
            return {}, None

        token_usages = list(getattr(metrics, "token_usages", None) or [])
        usage_index = next(
            (
                index
                for index, item in enumerate(token_usages)
                if str(getattr(item, "response_id", "") or "") == response_id
            ),
            len(token_usages) - 1,
        )
        usage: dict[str, int | float] = {}
        if usage_index >= 0:
            item = token_usages[usage_index]
            input_tokens = self._nonnegative_int(getattr(item, "prompt_tokens", None))
            output_tokens = self._nonnegative_int(getattr(item, "completion_tokens", None))
            if input_tokens is not None:
                usage["input_tokens"] = input_tokens
            if output_tokens is not None:
                usage["output_tokens"] = output_tokens
            if input_tokens is not None or output_tokens is not None:
                usage["total_tokens"] = (input_tokens or 0) + (output_tokens or 0)
            for source in ("cache_read_tokens", "cache_write_tokens"):
                value = self._nonnegative_int(getattr(item, source, None))
                if value is not None:
                    usage[source] = value

        costs = list(getattr(metrics, "costs", None) or [])
        if costs:
            cost_index = usage_index if 0 <= usage_index < len(costs) else -1
            value = getattr(costs[cost_index], "cost", None)
            if isinstance(value, (int, float)) and value >= 0:
                key = "estimated_cost" if self.metrics_cost_is_estimate else "actual_cost"
                usage[key] = float(value)

        latencies = list(getattr(metrics, "response_latencies", None) or [])
        latency = next(
            (
                getattr(item, "latency", None)
                for item in latencies
                if str(getattr(item, "response_id", "") or "") == response_id
            ),
            None,
        )
        if not isinstance(latency, (int, float)) and latencies:
            latency = getattr(latencies[-1], "latency", None)
        duration = float(latency) if isinstance(latency, (int, float)) and latency > 0 else None
        return usage, duration

    @staticmethod
    def _nonnegative_int(value: Any) -> int | None:
        if isinstance(value, bool):
            return None
        if isinstance(value, (int, float)) and value >= 0:
            return int(value)
        return None

    @staticmethod
    def _identity(event: Any) -> dict[str, Any]:
        return {
            "openhands.event.type": type(event).__name__,
            "openhands.event.id": str(getattr(event, "id", "") or ""),
            "openhands.event.source": str(getattr(event, "source", "") or ""),
        }

    @classmethod
    def _message_text(cls, event: Any) -> str:
        message = getattr(event, "llm_message", None)
        return cls._text_parts(getattr(message, "content", None))

    @staticmethod
    def _text_parts(value: Any) -> str:
        if value is None:
            return ""
        if isinstance(value, str):
            return value
        if not isinstance(value, (list, tuple)):
            value = [value]
        parts: list[str] = []
        for item in value:
            text = getattr(item, "text", None)
            if text is None and isinstance(item, dict):
                text = item.get("text")
            if text is not None:
                parts.append(str(text))
            elif isinstance(item, str):
                parts.append(item)
        return "\n".join(parts)

    @staticmethod
    def _public_dump(value: Any) -> Any:
        if value is None:
            return {}
        dump = getattr(value, "model_dump", None)
        if callable(dump):
            try:
                return dump(mode="json", exclude_none=True)
            except TypeError:
                return dump()
        if isinstance(value, (dict, list, tuple, str, int, float, bool)):
            return value
        return {"type": type(value).__name__}

    @classmethod
    def _tool_call_arguments(cls, tool_call: Any) -> Any:
        if tool_call is None:
            return {}
        function = getattr(tool_call, "function", None)
        raw = getattr(tool_call, "arguments", None) or getattr(function, "arguments", None) or {}
        if isinstance(raw, str):
            try:
                return json.loads(raw)
            except ValueError:
                return raw
        return cls._public_dump(raw)

    @staticmethod
    def _observation_status(result: Any) -> tuple[bool, str]:
        if not isinstance(result, dict):
            return True, ""
        error = result.get("error") or result.get("error_message")
        exit_code = result.get("exit_code")
        failed = bool(result.get("is_error")) or bool(error)
        if isinstance(exit_code, int) and exit_code != 0:
            failed = True
            error = error or f"exit code {exit_code}"
        return not failed, str(error or "")

    @classmethod
    def _result_text(cls, value: Any) -> str:
        if isinstance(value, str):
            return value
        if not isinstance(value, dict):
            return ""
        for key in ("message", "output", "text"):
            item = value.get(key)
            if isinstance(item, str) and item:
                return item
        return cls._text_parts(value.get("content"))

    @staticmethod
    def _token_content(chunk: Any) -> list[str]:
        result: list[str] = []
        for choice in getattr(chunk, "choices", None) or []:
            delta = getattr(choice, "delta", None)
            if delta is None:
                continue
            for field in ("reasoning_content", "content"):
                value = getattr(delta, field, None)
                if isinstance(value, str) and value:
                    result.append(value)
        return result

    @staticmethod
    def _summary(value: Any, limit: int) -> str:
        if isinstance(value, str):
            return value[:limit]
        try:
            return json.dumps(value, ensure_ascii=False, sort_keys=True)[:limit]
        except (TypeError, ValueError):
            return str(value)[:limit]

    @staticmethod
    def _int_value(values: dict[str, Any], *keys: str) -> int:
        for key in keys:
            value = values.get(key)
            if isinstance(value, int):
                return value
        return 0
