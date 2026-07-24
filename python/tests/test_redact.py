"""Tests for the AgentScope trace redaction module."""

from __future__ import annotations

from agentscope_sdk.redact import (
    is_sensitive_key,
    maybe_redact,
    redact_pii,
)


class TestIsSensitiveKey:
    def test_detects_api_key(self) -> None:
        assert is_sensitive_key("api_key")
        assert is_sensitive_key("llm.api_key")
        assert is_sensitive_key("provider.api_key")

    def test_detects_token(self) -> None:
        assert is_sensitive_key("token")
        assert is_sensitive_key("auth.token")
        assert is_sensitive_key("access_token")

    def test_detects_password(self) -> None:
        assert is_sensitive_key("password")
        assert is_sensitive_key("db.password")
        assert is_sensitive_key("passwd")

    def test_detects_secret(self) -> None:
        assert is_sensitive_key("secret")
        assert is_sensitive_key("client_secret")

    def test_detects_authorization(self) -> None:
        assert is_sensitive_key("Authorization")
        assert is_sensitive_key("http.authorization")
        assert is_sensitive_key("bearer")

    def test_non_sensitive_key(self) -> None:
        assert not is_sensitive_key("model")
        assert not is_sensitive_key("llm.provider")
        assert not is_sensitive_key("input_summary")


class TestRedactPII:
    def test_redacts_email(self) -> None:
        assert redact_pii("user@example.com") == "[EMAIL_REDACTED]"
        assert redact_pii("contact me at user@example.com") == "contact me at [EMAIL_REDACTED]"

    def test_redacts_phone(self) -> None:
        assert redact_pii("13800138000") == "[PHONE_REDACTED]"

    def test_redacts_id_card(self) -> None:
        assert redact_pii("110101199001011234") == "[ID_CARD_REDACTED]"

    def test_preserves_normal_text(self) -> None:
        assert redact_pii("hello world") == "hello world"
        assert redact_pii("12345") == "12345"


class TestMaybeRedact:
    def test_redacts_sensitive_key(self) -> None:
        assert maybe_redact("api_key", "sk-abc123") == "[REDACTED]"
        assert maybe_redact("password", "hunter2") == "[REDACTED]"
        assert maybe_redact("auth.token", "eyJhbGci") == "[REDACTED]"

    def test_redacts_pii_in_value(self) -> None:
        assert maybe_redact("user_input", "my email is user@example.com") == "my email is [EMAIL_REDACTED]"

    def test_preserves_non_sensitive(self) -> None:
        assert maybe_redact("model", "gpt-4") == "gpt-4"

    def test_handles_numeric_value(self) -> None:
        assert maybe_redact("input_tokens", 50) == 50

    def test_handles_bool_value(self) -> None:
        assert maybe_redact("success", True) is True
