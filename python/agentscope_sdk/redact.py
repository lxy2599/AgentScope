"""Sensitive data redaction utilities for AgentScope tracing.

Provides helpers to detect and redact sensitive information (API keys, tokens,
passwords, PII) from span attributes before they are exported.
"""

from __future__ import annotations

import re
from typing import Any

# Sensitive attribute key substrings (case-insensitive, dot-separated key matching).
SENSITIVE_KEY_PARTS: set[str] = {
    "authorization",
    "api_key",
    "apikey",
    "token",
    "secret",
    "password",
    "passwd",
    "credential",
    "access_key",
    "private_key",
    "auth_token",
    "bearer",
}

# PII redaction patterns.
_PII_PATTERNS: list[tuple[re.Pattern[str], str]] = [
    (re.compile(r"\b[\w.+-]+@[\w-]+\.[\w.-]+\b"), "[EMAIL_REDACTED]"),
    (re.compile(r"\b1[3-9]\d{9}\b"), "[PHONE_REDACTED]"),
    (re.compile(r"\b\d{17}[\dXx]\b"), "[ID_CARD_REDACTED]"),
]


def is_sensitive_key(key: str) -> bool:
    """Return ``True`` if the attribute key indicates a sensitive field.

    Matching is case-insensitive and considers the last dot-separated segment
    of the key (e.g. ``"llm.api_key"`` triggers because ``api_key`` is in the
    sensitive set).
    """
    parts = key.lower().replace("-", "_").split(".")
    for part in parts:
        # Check the whole segment
        if part in SENSITIVE_KEY_PARTS:
            return True
        # Check sub-parts separated by underscore
        for sub in part.split("_"):
            if sub in SENSITIVE_KEY_PARTS:
                return True
    return False


def redact_pii(value: str) -> str:
    """Replace common PII patterns in *value* with placeholders.

    Currently handles:
    - Email addresses
    - Chinese mobile phone numbers (11 digits starting with 1)
    - Chinese 18-digit ID numbers

    Returns the redacted string.
    """
    result = value
    for pattern, replacement in _PII_PATTERNS:
        result = pattern.sub(replacement, result)
    return result


def maybe_redact(key: str, value: Any) -> Any:
    """Redact a value if its key or content suggests sensitivity.

    - If *key* matches a sensitive field name, the entire value is replaced
      with ``"[REDACTED]"``.
    - If *value* is a string, PII patterns (email, phone, ID) are redacted
      regardless of key name.

    Args:
        key: The attribute key (e.g. ``"llm.api_key"``).
        value: The attribute value to potentially redact.

    Returns:
        The redacted value (``"[REDACTED]"``, a PII-redacted string, or the
        original value).
    """
    if is_sensitive_key(key):
        return "[REDACTED]"
    if isinstance(value, str):
        return redact_pii(value)
    return value
