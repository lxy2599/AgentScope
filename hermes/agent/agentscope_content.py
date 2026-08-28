"""Bounded, redacted content helpers for Hermes' native AgentScope spans.

The native integration is metadata-only by default.  When an operator
explicitly enables ``AGENTSCOPE_CAPTURE_CONTENT``, these helpers make captured
LLM and tool payloads useful for debugging without exporting reasoning,
credentials, or unbounded data.
"""

from __future__ import annotations

import json
import os
import re
from collections.abc import Mapping
from typing import Any

_CAPTURE_VALUES = frozenset({"1", "true", "yes", "on"})
_MAX_CONTENT_BYTES = 3_840
_SENSITIVE_KEY_RE = re.compile(
    r"(?:api[_-]?key|authorization|token|secret|password|credential|"
    r"access[_-]?key|private[_-]?key|bearer)",
    re.IGNORECASE,
)
_TEXT_SECRET_PATTERNS = (
    # Headers and common key/value formats.
    (re.compile(r"(?i)(bearer\s+)[^\s\"']+"), r"\1[REDACTED]"),
    (
        re.compile(
            r"(?i)\b(api[_-]?key|token|secret|password|credential|"
            r"access[_-]?key|private[_-]?key)\b(\s*[:=]\s*)"
            r"([^\s,;\"']+)"
        ),
        r"\1\2[REDACTED]",
    ),
    # Provider/project credential forms that can appear in plain tool output.
    (
        re.compile(r"\b(?:sk|ark|ascope)[_-][A-Za-z0-9_-]{20,}\b", re.IGNORECASE),
        "[REDACTED]",
    ),
)


def capture_content_enabled(agent: Any) -> bool:
    """Return the explicit operator opt-in, defaulting to safe L0 mode."""
    configured = getattr(agent, "_agentscope_capture_content", None)
    if configured is not None:
        return bool(configured)
    return (
        str(os.getenv("AGENTSCOPE_CAPTURE_CONTENT", "")).strip().lower()
        in _CAPTURE_VALUES
    )


def capture_attributes(agent: Any, attributes: Mapping[str, Any]) -> dict[str, Any]:
    """Add the UI-facing capture marker only when content was actually sent."""
    result = dict(attributes)
    if capture_content_enabled(agent):
        result["agentscope.content_captured"] = True
    return result


def trace_content(value: Any, *, max_bytes: int = _MAX_CONTENT_BYTES) -> str:
    """Serialize arbitrary payloads for a span after recursive redaction.

    This is intentionally not a generic audit log: it is UTF-8 bounded and
    omits unexpected object internals.  The AgentScope SDK redactor remains a
    second, independent safeguard before export.
    """
    try:
        serialized = json.dumps(
            _sanitize(value),
            ensure_ascii=False,
            separators=(",", ":"),
            default=_unknown_value,
        )
    except Exception:
        serialized = _redact_text(str(value))
    return _truncate_utf8(_redact_text(serialized), max_bytes)


def user_turn_content(value: Any) -> str:
    """Capture a redacted user turn without JSON-quoting ordinary text."""
    if isinstance(value, str):
        return _truncate_utf8(_redact_text(value), _MAX_CONTENT_BYTES)
    return trace_content(value)


def llm_input_content(messages: Any) -> str:
    """Capture the useful request transcript while never retaining system prompt text."""
    if not isinstance(messages, (list, tuple)):
        return trace_content(messages)

    # Retain the freshest request context.  The system prompt can be large and
    # is application-owned instruction text rather than task evidence, so only
    # its length is recorded.  This also avoids it crowding out user/tool data.
    selected = list(messages[-8:])
    normalized: list[Any] = []
    omitted = max(0, len(messages) - len(selected))
    if omitted:
        normalized.append({"_omitted_messages": omitted})
    for message in selected:
        item = _as_mapping(message)
        if item and str(item.get("role", "")).lower() == "system":
            content = item.get("content")
            normalized.append({
                "role": "system",
                "content": f"[system prompt omitted: {len(str(content or ''))} chars]",
            })
        else:
            normalized.append(message)
    return trace_content(normalized)


def llm_output_content(response: Any) -> str:
    """Extract visible assistant text/tool calls without reasoning fields."""
    choices = _get_value(response, "choices", []) or []
    if choices:
        choice = choices[0]
        message = _get_value(choice, "message", {}) or {}
        result: dict[str, Any] = {"content": _get_value(message, "content", "") or ""}
        tool_calls = _get_value(message, "tool_calls", None)
        if tool_calls:
            result["tool_calls"] = [
                {
                    "id": _get_value(call, "id", ""),
                    "name": _get_value(_get_value(call, "function", {}), "name", ""),
                    "arguments": _get_value(
                        _get_value(call, "function", {}), "arguments", ""
                    ),
                }
                for call in tool_calls
            ]
        finish_reason = _get_value(choice, "finish_reason", "")
        if finish_reason:
            result["finish_reason"] = finish_reason
        return trace_content(result)

    # OpenAI Responses-compatible providers return ``output`` items rather
    # than Chat Completions ``choices``. The previous fallback turned every
    # valid GPT-5/Codex response into ``[no choices]``, making trace output
    # appear uncollected even though the agent loop had usable data.
    output_items = _get_value(response, "output", None)
    if isinstance(output_items, (list, tuple)):
        text_parts: list[str] = []
        tool_calls: list[dict[str, Any]] = []
        for item in output_items:
            item_type = str(_get_value(item, "type", "") or "").strip().lower()
            if item_type == "message":
                for part in _get_value(item, "content", []) or []:
                    if str(_get_value(part, "type", "") or "").strip().lower() != "output_text":
                        continue
                    text = _get_value(part, "text", "")
                    if isinstance(text, str) and text:
                        text_parts.append(text)
            elif item_type == "function_call":
                tool_calls.append(
                    {
                        "id": _get_value(item, "call_id", "") or _get_value(item, "id", ""),
                        "name": _get_value(item, "name", ""),
                        "arguments": _get_value(item, "arguments", ""),
                    }
                )
            # Reasoning items may contain encrypted provider payloads. They
            # are intentionally omitted from trace content.
        result = {}
        if text_parts:
            result["content"] = "".join(text_parts)
        output_text = _get_value(response, "output_text", "")
        if not text_parts and isinstance(output_text, str) and output_text:
            result["content"] = output_text
        if tool_calls:
            result["tool_calls"] = tool_calls
        status = _get_value(response, "status", "")
        if status:
            result["status"] = status
        if result:
            return trace_content(result)

    output_text = _get_value(response, "output_text", "")
    if isinstance(output_text, str) and output_text:
        return trace_content({"content": output_text})
    return trace_content({"response": "[no visible output]"})


def _sanitize(value: Any, *, depth: int = 0) -> Any:
    if depth >= 12:
        return "[TRUNCATED_DEPTH]"
    if isinstance(value, Mapping):
        return {
            str(key): "[REDACTED]"
            if _SENSITIVE_KEY_RE.search(str(key))
            else _sanitize(item, depth=depth + 1)
            for key, item in value.items()
        }
    if isinstance(value, (list, tuple)):
        return [_sanitize(item, depth=depth + 1) for item in value]
    if isinstance(value, bytes):
        return f"[binary data: {len(value)} bytes]"
    if isinstance(value, str):
        return _redact_text(value)
    if value is None or isinstance(value, (bool, int, float)):
        return value
    mapping = _as_mapping(value)
    if mapping is not None:
        return _sanitize(mapping, depth=depth + 1)
    return _redact_text(str(value))


def _as_mapping(value: Any) -> Mapping[str, Any] | None:
    if isinstance(value, Mapping):
        return value
    for method_name in ("model_dump", "to_dict", "dict"):
        method = getattr(value, method_name, None)
        if callable(method):
            try:
                result = method()
            except Exception:
                continue
            if isinstance(result, Mapping):
                return result
    data = getattr(value, "__dict__", None)
    return data if isinstance(data, Mapping) else None


def _get_value(value: Any, name: str, default: Any = None) -> Any:
    if isinstance(value, Mapping):
        return value.get(name, default)
    return getattr(value, name, default)


def _unknown_value(value: Any) -> str:
    return _redact_text(str(value))


def _redact_text(value: str) -> str:
    for pattern, replacement in _TEXT_SECRET_PATTERNS:
        value = pattern.sub(replacement, value)

    # Tool-call arguments are often a JSON document stored inside an outer
    # JSON field. Redact that nested document too, rather than relying on a
    # quote-sensitive text pattern to catch every secret key spelling.
    stripped = value.strip()
    if (stripped.startswith("{") and stripped.endswith("}")) or (
        stripped.startswith("[") and stripped.endswith("]")
    ):
        try:
            parsed = json.loads(stripped)
        except (TypeError, ValueError):
            pass
        else:
            return json.dumps(
                _sanitize(parsed), ensure_ascii=False, separators=(",", ":")
            )
    return value


def _truncate_utf8(value: str, max_bytes: int) -> str:
    encoded = value.encode("utf-8")
    if len(encoded) <= max_bytes:
        return value
    suffix = "…[truncated]"
    limit = max(0, max_bytes - len(suffix.encode("utf-8")))
    return encoded[:limit].decode("utf-8", errors="ignore") + suffix
