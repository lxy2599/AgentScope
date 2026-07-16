"""Tests for judge JSON parsing and provider abstraction."""

import os
from unittest.mock import patch

import httpx
import pytest


class TestParseJudgeResponse:
    """Tests for parse_judge_response."""

    def test_valid_json_with_pass_true(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response(
            '{"score":0.8,"pass":true,"reason":"mostly correct"}'
        )
        assert result.score == 0.8
        assert result.passed is True
        assert result.reason == "mostly correct"

    def test_valid_json_with_passed_field(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response(
            '{"score":0.5,"passed":false,"reason":"partially correct"}'
        )
        assert result.score == 0.5
        assert result.passed is False
        assert result.reason == "partially correct"

    def test_passed_field_overrides_pass(self):
        """When both pass and passed are present, 'passed' wins."""
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response(
            '{"score":0.9,"pass":false,"passed":true,"reason":"good"}'
        )
        assert result.passed is True

    def test_score_zero_is_valid(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response(
            '{"score":0.0,"pass":false,"reason":"completely wrong"}'
        )
        assert result.score == 0.0
        assert result.passed is False

    def test_score_one_is_valid(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response(
            '{"score":1.0,"pass":true,"reason":"perfect"}'
        )
        assert result.score == 1.0
        assert result.passed is True

    def test_score_below_zero_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response(
                '{"score":-0.1,"pass":false,"reason":"below range"}'
            )

    def test_score_above_one_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response(
                '{"score":1.5,"pass":true,"reason":"above range"}'
            )

    def test_missing_score_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response('{"pass":true,"reason":"no score"}')

    def test_missing_pass_and_passed_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="pass"):
            parse_judge_response('{"score":0.5,"reason":"no pass field"}')

    def test_missing_reason_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="reason"):
            parse_judge_response('{"score":0.5,"pass":true}')

    def test_non_json_response_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError):
            parse_judge_response("not json at all")

    def test_empty_string_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError):
            parse_judge_response("")

    def test_parsing_error_does_not_return_passed_false(self):
        """Parsing failure must raise, not silently return passed=False."""
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError):
            parse_judge_response("this is invalid JSON")


class TestJudgeResultDataclass:
    def test_default_values(self):
        from agentscope_sdk.judges import JudgeResult

        r = JudgeResult(score=0.0, passed=False, reason="")
        assert r.score == 0.0
        assert r.passed is False
        assert r.reason == ""

    def test_immutable_fields(self):
        """JudgeResult should be a frozen dataclass."""
        from dataclasses import FrozenInstanceError
        from agentscope_sdk.judges import JudgeResult

        r = JudgeResult(score=0.5, passed=True, reason="ok")
        with pytest.raises(FrozenInstanceError):
            r.score = 0.9


class TestJudgeProviderBase:
    def test_provider_has_evaluate_method(self):
        from agentscope_sdk.judges import JudgeProvider

        assert hasattr(JudgeProvider, "evaluate")

    def test_provider_evaluate_not_implemented(self):
        """Base provider evaluate should raise NotImplementedError."""
        from agentscope_sdk.judges import JudgeProvider, JudgeRequest

        provider = JudgeProvider()
        request = JudgeRequest(
            input="test",
            rubric="be correct",
        )
        with pytest.raises(NotImplementedError):
            provider.evaluate(request)


class TestOpenAICompatibleProvider:
    """Tests for OpenAI-compatible provider."""

    VALID_JUDGE_RESPONSE = (
        '{"score":0.9,"pass":true,"reason":"good answer"}'
    )

    @pytest.fixture
    def mock_env(self, monkeypatch):
        monkeypatch.setenv("OPENAI_API_KEY", "sk-fake-key-for-testing")

    def _mock_transport(self, response_text: str, status_code: int = 200):
        """Create a MockTransport that returns the given JSON response."""

        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                status_code=status_code,
                json={"choices": [{"message": {"content": response_text}}]},
            )

        return httpx.MockTransport(handler)

    def test_default_temperature_is_zero(self, mock_env):
        from agentscope_sdk.judges import OpenAICompatibleProvider

        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
        )
        assert provider.temperature == 0.0

    def test_evaluate_success(self, mock_env):
        from agentscope_sdk.judges import JudgeRequest, OpenAICompatibleProvider

        transport = self._mock_transport(self.VALID_JUDGE_RESPONSE)
        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
            transport=transport,
        )

        result = provider.evaluate(
            JudgeRequest(input="What is 2+2?", rubric="must be correct")
        )
        assert result.score == 0.9
        assert result.passed is True
        assert "good answer" in result.reason

    def test_evaluate_uses_chat_completions_endpoint(self, mock_env):
        from agentscope_sdk.judges import JudgeRequest, OpenAICompatibleProvider

        captured = {}

        def handler(request: httpx.Request) -> httpx.Response:
            captured["url"] = str(request.url)
            captured["body"] = request.read()
            return httpx.Response(
                status_code=200,
                json={"choices": [{"message": {"content": self.VALID_JUDGE_RESPONSE}}]},
            )

        transport = httpx.MockTransport(handler)
        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
            transport=transport,
        )

        provider.evaluate(JudgeRequest(input="test", rubric="test"))
        assert "/chat/completions" in captured["url"]
        body = captured["body"]
        assert b"gpt-4o" in body
        assert b"test" in body
        assert b"response_format" in body

    def test_evaluate_sends_auth_header(self, mock_env):
        from agentscope_sdk.judges import JudgeRequest, OpenAICompatibleProvider

        captured = {}

        def handler(request: httpx.Request) -> httpx.Response:
            captured["auth"] = request.headers.get("Authorization", "")
            return httpx.Response(
                status_code=200,
                json={"choices": [{"message": {"content": self.VALID_JUDGE_RESPONSE}}]},
            )

        transport = httpx.MockTransport(handler)
        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
            transport=transport,
        )

        provider.evaluate(JudgeRequest(input="test", rubric="test"))
        assert "Bearer " in captured["auth"]

    def test_api_key_env_empty_raises_error(self):
        from agentscope_sdk.judges import OpenAICompatibleProvider

        with pytest.raises(ValueError, match="api_key_env"):
            OpenAICompatibleProvider(
                model="gpt-4o",
                api_key_env="",
                base_url="https://api.openai.com/v1",
            )

    def test_missing_environment_variable_raises_error(self):
        """Error must not contain the key value."""
        from agentscope_sdk.judges import JudgeRequest, OpenAICompatibleProvider

        with patch.dict(os.environ, {}, clear=True):
            provider = OpenAICompatibleProvider(
                model="gpt-4o",
                api_key_env="NONEXISTENT_KEY",
                base_url="https://api.openai.com/v1",
            )
            with pytest.raises(ValueError) as excinfo:
                provider.evaluate(JudgeRequest(input="x", rubric="y"))

            error_msg = str(excinfo.value)
            assert "NONEXISTENT_KEY" not in error_msg
            assert "API key" in error_msg or "environment" in error_msg

    def test_api_error_preserves_error_message(self, mock_env):
        from agentscope_sdk.judges import JudgeRequest, OpenAICompatibleProvider

        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                status_code=401,
                json={"error": {"message": "Incorrect API key provided"}},
            )

        transport = httpx.MockTransport(handler)
        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
            transport=transport,
        )

        with pytest.raises(httpx.HTTPStatusError):
            provider.evaluate(JudgeRequest(input="x", rubric="y"))

    def test_no_api_key_parameter(self):
        """Provider must not accept a direct 'api_key' parameter."""
        from agentscope_sdk.judges import OpenAICompatibleProvider

        with pytest.raises(TypeError):
            OpenAICompatibleProvider(
                model="gpt-4o",
                api_key="sk-raw-key",
                api_key_env="OPENAI_API_KEY",
                base_url="https://api.openai.com/v1",
            )

    def test_requires_api_key_env(self):
        """Provider must require api_key_env."""
        from agentscope_sdk.judges import OpenAICompatibleProvider

        with pytest.raises(TypeError):
            OpenAICompatibleProvider(
                model="gpt-4o",
                base_url="https://api.openai.com/v1",
            )


class TestAnthropicCompatibleProvider:
    """Tests for Anthropic-compatible provider."""

    VALID_JUDGE_RESPONSE = (
        '{"score":0.7,"pass":true,"reason":"decent answer"}'
    )

    @pytest.fixture
    def mock_env(self, monkeypatch):
        monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-fake-key-for-testing")

    def _mock_transport(self, response_text: str, status_code: int = 200):
        """Create a MockTransport that returns the given JSON response."""

        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                status_code=status_code,
                json={
                    "content": [{"type": "text", "text": response_text}],
                },
            )

        return httpx.MockTransport(handler)

    def test_default_temperature_is_zero(self, mock_env):
        from agentscope_sdk.judges import AnthropicCompatibleProvider

        provider = AnthropicCompatibleProvider(
            model="claude-sonnet-4-20250514",
            api_key_env="ANTHROPIC_API_KEY",
            base_url="https://api.anthropic.com/v1",
        )
        assert provider.temperature == 0.0

    def test_evaluate_success(self, mock_env):
        from agentscope_sdk.judges import (
            AnthropicCompatibleProvider,
            JudgeRequest,
        )

        transport = self._mock_transport(self.VALID_JUDGE_RESPONSE)
        provider = AnthropicCompatibleProvider(
            model="claude-sonnet-4-20250514",
            api_key_env="ANTHROPIC_API_KEY",
            base_url="https://api.anthropic.com/v1",
            transport=transport,
        )

        result = provider.evaluate(
            JudgeRequest(input="What is AI?", rubric="be accurate")
        )
        assert result.score == 0.7
        assert result.passed is True
        assert "decent answer" in result.reason

    def test_evaluate_uses_messages_endpoint(self, mock_env):
        from agentscope_sdk.judges import (
            AnthropicCompatibleProvider,
            JudgeRequest,
        )

        captured = {}

        def handler(request: httpx.Request) -> httpx.Response:
            captured["url"] = str(request.url)
            captured["body"] = request.read()
            return httpx.Response(
                status_code=200,
                json={
                    "content": [{"type": "text", "text": self.VALID_JUDGE_RESPONSE}],
                },
            )

        transport = httpx.MockTransport(handler)
        provider = AnthropicCompatibleProvider(
            model="claude-sonnet-4-20250514",
            api_key_env="ANTHROPIC_API_KEY",
            base_url="https://api.anthropic.com/v1",
            transport=transport,
        )

        provider.evaluate(JudgeRequest(input="test", rubric="test"))
        assert "/messages" in captured["url"]
        body = captured["body"]
        assert b"claude-sonnet-4-20250514" in body
        assert b"test" in body

    def test_evaluate_sends_auth_header(self, mock_env):
        from agentscope_sdk.judges import (
            AnthropicCompatibleProvider,
            JudgeRequest,
        )

        captured = {}

        def handler(request: httpx.Request) -> httpx.Response:
            captured["auth"] = request.headers.get("x-api-key", "")
            return httpx.Response(
                status_code=200,
                json={
                    "content": [{"type": "text", "text": self.VALID_JUDGE_RESPONSE}],
                },
            )

        transport = httpx.MockTransport(handler)
        provider = AnthropicCompatibleProvider(
            model="claude-sonnet-4-20250514",
            api_key_env="ANTHROPIC_API_KEY",
            base_url="https://api.anthropic.com/v1",
            transport=transport,
        )

        provider.evaluate(JudgeRequest(input="test", rubric="test"))
        assert captured["auth"] != ""

    def test_api_key_env_empty_raises_error(self):
        from agentscope_sdk.judges import AnthropicCompatibleProvider

        with pytest.raises(ValueError, match="api_key_env"):
            AnthropicCompatibleProvider(
                model="claude-sonnet-4-20250514",
                api_key_env="",
                base_url="https://api.anthropic.com/v1",
            )

    def test_missing_environment_variable_raises_error(self):
        """Error must not contain any key value."""
        from agentscope_sdk.judges import (
            AnthropicCompatibleProvider,
            JudgeRequest,
        )

        with patch.dict(os.environ, {}, clear=True):
            provider = AnthropicCompatibleProvider(
                model="claude-sonnet-4-20250514",
                api_key_env="NONEXISTENT_KEY",
                base_url="https://api.anthropic.com/v1",
            )
            with pytest.raises(ValueError) as excinfo:
                provider.evaluate(JudgeRequest(input="x", rubric="y"))

            error_msg = str(excinfo.value)
            assert "NONEXISTENT_KEY" not in error_msg
            assert "API key" in error_msg or "environment" in error_msg

    def test_api_error_preserves_error_message(self, mock_env):
        from agentscope_sdk.judges import (
            AnthropicCompatibleProvider,
            JudgeRequest,
        )

        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                status_code=401,
                json={"error": {"message": "Invalid API key"}},
            )

        transport = httpx.MockTransport(handler)
        provider = AnthropicCompatibleProvider(
            model="claude-sonnet-4-20250514",
            api_key_env="ANTHROPIC_API_KEY",
            base_url="https://api.anthropic.com/v1",
            transport=transport,
        )

        with pytest.raises(httpx.HTTPStatusError):
            provider.evaluate(JudgeRequest(input="x", rubric="y"))

    def test_no_api_key_parameter(self):
        """Provider must not accept a direct 'api_key' parameter."""
        from agentscope_sdk.judges import AnthropicCompatibleProvider

        with pytest.raises(TypeError):
            AnthropicCompatibleProvider(
                model="claude-sonnet-4-20250514",
                api_key="sk-ant-raw-key",
                api_key_env="ANTHROPIC_API_KEY",
                base_url="https://api.anthropic.com/v1",
            )

    def test_requires_api_key_env(self):
        """Provider must require api_key_env."""
        from agentscope_sdk.judges import AnthropicCompatibleProvider

        with pytest.raises(TypeError):
            AnthropicCompatibleProvider(
                model="claude-sonnet-4-20250514",
                base_url="https://api.anthropic.com/v1",
            )
