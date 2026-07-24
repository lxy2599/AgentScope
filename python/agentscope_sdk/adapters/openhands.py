"""Non-invasive observability adapter for the OpenHands Software Agent SDK."""

from __future__ import annotations

import hashlib
import json
import logging
import time
from typing import Any

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
    ) -> None:
        self.session = session
        self.capture_reasoning = capture_reasoning
        self.capture_system_prompt = capture_system_prompt
        self._started_at = 0.0
        self._first_token_seen = False
        self._final_output = ""
        self._fatal_error = ""
        self._event_ids: set[str] = set()
        self._llm_response_ids: set[str] = set()
        self._tool_ids: set[str] = set()
        self._token_parts: list[str] = []
        self._usage: dict[str, int] = {}

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
            getattr(conversation, "id", "")
            or getattr(getattr(conversation, "state", None), "id", "")
            or ""
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
                ttft_ms = max(0, int((time.monotonic() - self._started_at) * 1000))
                self.session.on_external_event(
                    "llm.first_token",
                    semantic_kind="LLM",
                    attributes={"llm.ttft_ms": ttft_ms},
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
        reasoning = str(getattr(event, "reasoning_content", "") or "")
        if reasoning:
            self.session.on_thought(
                "reasoning",
                reasoning,
                capture_content=self.capture_reasoning,
            )
        skills = [
            str(item)
            for item in (getattr(event, "activated_skills", None) or [])
        ]
        for skill in skills:
            self.session.on_skill_call(
                skill,
                "activated",
                arguments={"source": "OpenHands MessageEvent"},
            )

        response_id = str(getattr(event, "llm_response_id", "") or "")
        if source == "agent":
            self._record_llm_response(response_id, content)
            self._final_output = content or self._final_output
        else:
            self.session.on_external_event(
                "openhands.message.user",
                semantic_kind="INTERNAL",
                input_summary=content,
                attributes=self._identity(event),
            )

    def _on_action(self, event: Any) -> None:
        name = str(getattr(event, "tool_name", "") or "unknown")
        tool_id = str(getattr(event, "tool_call_id", "") or name)
        response_id = str(getattr(event, "llm_response_id", "") or "")
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

    def _on_tool_error(self, event: Any) -> None:
        name = str(getattr(event, "tool_name", "") or "unknown")
        tool_id = str(getattr(event, "tool_call_id", "") or name)
        error = str(
            getattr(event, "error", "")
            or getattr(event, "rejection_reason", "")
            or "OpenHands tool failed"
        )
        self.session.on_tool_finish(
            name,
            {"error": error},
            tool_id=tool_id,
            success=False,
            error=error,
        )
        self._tool_ids.discard(tool_id)

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
        prompt = str(
            getattr(getattr(event, "system_prompt", None), "text", "") or ""
        )
        dynamic_context = str(
            getattr(getattr(event, "dynamic_context", None), "text", "") or ""
        )
        tools = [
            str(getattr(tool, "name", "") or "")
            for tool in (getattr(event, "tools", None) or [])
        ]
        prompt_hash = hashlib.sha256(prompt.encode("utf-8")).hexdigest()
        output = prompt if self.capture_system_prompt else ""
        self.session.on_external_event(
            "openhands.system_prompt",
            semantic_kind="LLM",
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
        try:
            payload = json.loads(str(getattr(event, "log_data", "") or "{}"))
        except (TypeError, ValueError):
            pass
        usage = payload.get("usage") if isinstance(payload, dict) else {}
        if isinstance(usage, dict):
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
            }
        self.session.on_external_event(
            "openhands.llm_completion",
            semantic_kind="LLM",
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
        self.session.on_llm_call(
            f"openhands.{key}",
            output_summary=output,
            provider=str(self.session.metadata.get("model_provider", "") or ""),
            model=str(self.session.metadata.get("model_name", "") or ""),
            input_tokens=self._usage.get("input_tokens"),
            output_tokens=self._usage.get("output_tokens"),
        )

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
        raw = (
            getattr(tool_call, "arguments", None)
            or getattr(function, "arguments", None)
            or {}
        )
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
