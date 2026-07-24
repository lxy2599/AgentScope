"""Tests for judge JSON parsing and provider abstraction."""

import os
from unittest.mock import patch

import httpx
import pytest


class TestParseJudgeResponse:
    """Tests for parse_judge_response."""

    def test_valid_json_with_pass_true(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response('{"score":0.8,"pass":true,"reason":"mostly correct"}')
        assert result.score == 0.8
        assert result.passed is True
        assert result.reason == "mostly correct"

    def test_valid_json_with_passed_field(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response('{"score":0.5,"passed":false,"reason":"partially correct"}')
        assert result.score == 0.5
        assert result.passed is False
        assert result.reason == "partially correct"

    def test_passed_field_overrides_pass(self):
        """When both pass and passed are present, 'passed' wins."""
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response('{"score":0.9,"pass":false,"passed":true,"reason":"good"}')
        assert result.passed is True

    def test_score_zero_is_valid(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response('{"score":0.0,"pass":false,"reason":"completely wrong"}')
        assert result.score == 0.0
        assert result.passed is False

    def test_score_one_is_valid(self):
        from agentscope_sdk.judges import parse_judge_response

        result = parse_judge_response('{"score":1.0,"pass":true,"reason":"perfect"}')
        assert result.score == 1.0
        assert result.passed is True

    def test_score_below_zero_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response('{"score":-0.1,"pass":false,"reason":"below range"}')

    def test_score_above_one_raises_error(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response('{"score":1.5,"pass":true,"reason":"above range"}')

    @pytest.mark.parametrize("score", ["NaN", "Infinity", "-Infinity"])
    def test_non_finite_score_raises_error(self, score):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response(f'{{"score":{score},"pass":true,"reason":"not finite"}}')

    def test_boolean_score_is_not_treated_as_one(self):
        from agentscope_sdk.judges import JudgeResponseError, parse_judge_response

        with pytest.raises(JudgeResponseError, match="score"):
            parse_judge_response('{"score":true,"pass":true,"reason":"not numeric"}')

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

    VALID_JUDGE_RESPONSE = '{"score":0.9,"pass":true,"reason":"good answer"}'

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

        result = provider.evaluate(JudgeRequest(input="What is 2+2?", rubric="must be correct"))
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
                api_key_env="OPENAI_API_KEY",
                base_url="https://api.openai.com/v1",
            )
            with pytest.raises(ValueError) as excinfo:
                provider.evaluate(JudgeRequest(input="x", rubric="y"))

            error_msg = str(excinfo.value)
            assert "OPENAI_API_KEY" not in error_msg
            assert "API key" in error_msg or "environment" in error_msg

    def test_rejects_unapproved_secret_and_destination_before_request(
        self,
        monkeypatch,
    ):
        """Evaluator config cannot turn an arbitrary process secret into SSRF."""
        from agentscope_sdk.judges import OpenAICompatibleProvider

        monkeypatch.setenv("AWS_SECRET_ACCESS_KEY", "must-never-leave")
        called = False

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal called
            called = True
            return httpx.Response(200, json={})

        with pytest.raises(ValueError, match="security policy"):
            OpenAICompatibleProvider(
                model="gpt-4o",
                api_key_env="AWS_SECRET_ACCESS_KEY",
                base_url="https://collector.attacker.example/v1",
                transport=httpx.MockTransport(handler),
            )
        assert called is False

    def test_custom_provider_requires_operator_allowlists(self, monkeypatch):
        from agentscope_sdk.judges import (
            JudgeRequest,
            OpenAICompatibleProvider,
        )

        monkeypatch.setenv(
            "AGENTSCOPE_JUDGE_ALLOWED_API_KEY_ENVS",
            "DEEPSEEK_API_KEY",
        )
        monkeypatch.setenv(
            "AGENTSCOPE_JUDGE_ALLOWED_BASE_URLS",
            "https://api.deepseek.com",
        )
        monkeypatch.setenv("DEEPSEEK_API_KEY", "operator-approved-key")

        def handler(request: httpx.Request) -> httpx.Response:
            assert request.headers["Authorization"] == "Bearer operator-approved-key"
            return httpx.Response(
                200,
                json={"choices": [{"message": {"content": self.VALID_JUDGE_RESPONSE}}]},
            )

        provider = OpenAICompatibleProvider(
            model="deepseek-chat",
            api_key_env="DEEPSEEK_API_KEY",
            base_url="https://api.deepseek.com/v1",
            transport=httpx.MockTransport(handler),
        )
        assert provider.evaluate(
            JudgeRequest(input="x", rubric="y"),
        ).passed

    def test_insecure_custom_origin_requires_explicit_opt_in(self, monkeypatch):
        from agentscope_sdk.judges import OpenAICompatibleProvider

        monkeypatch.setenv(
            "AGENTSCOPE_JUDGE_ALLOWED_BASE_URLS",
            "http://127.0.0.1:11434",
        )
        with pytest.raises(ValueError, match="HTTPS"):
            OpenAICompatibleProvider(
                model="local-model",
                api_key_env="OPENAI_API_KEY",
                base_url="http://127.0.0.1:11434/v1",
            )

    def test_base_url_policy_is_rechecked_before_sending_secret(
        self,
        mock_env,
    ):
        from agentscope_sdk.judges import (
            JudgeRequest,
            OpenAICompatibleProvider,
        )

        called = False

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal called
            called = True
            return httpx.Response(
                200,
                json={"choices": [{"message": {"content": self.VALID_JUDGE_RESPONSE}}]},
            )

        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
            transport=httpx.MockTransport(handler),
        )
        provider.base_url = "https://collector.attacker.example/v1"

        with pytest.raises(ValueError, match="security policy"):
            provider.evaluate(JudgeRequest(input="x", rubric="y"))
        assert called is False

    @pytest.mark.parametrize(
        ("field", "value"),
        [
            ("temperature", float("nan")),
            ("temperature", float("inf")),
            ("max_tokens", True),
            ("max_tokens", 0),
            ("max_tokens", float("inf")),
        ],
    )
    def test_invalid_request_numeric_config_is_rejected(
        self,
        field,
        value,
    ):
        from agentscope_sdk.judges import OpenAICompatibleProvider

        kwargs = {
            "model": "gpt-4o",
            "api_key_env": "OPENAI_API_KEY",
            "base_url": "https://api.openai.com/v1",
            field: value,
        }
        with pytest.raises(ValueError, match=field):
            OpenAICompatibleProvider(**kwargs)

    def test_whitespace_only_api_key_is_rejected_before_request(
        self,
        monkeypatch,
    ):
        from agentscope_sdk.judges import (
            JudgeRequest,
            OpenAICompatibleProvider,
        )

        monkeypatch.setenv("OPENAI_API_KEY", "   ")
        called = False

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal called
            called = True
            return httpx.Response(200, json={})

        provider = OpenAICompatibleProvider(
            model="gpt-4o",
            api_key_env="OPENAI_API_KEY",
            base_url="https://api.openai.com/v1",
            transport=httpx.MockTransport(handler),
        )
        with pytest.raises(ValueError, match="empty"):
            provider.evaluate(JudgeRequest(input="x", rubric="y"))
        assert called is False

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

    VALID_JUDGE_RESPONSE = '{"score":0.7,"pass":true,"reason":"decent answer"}'

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

        result = provider.evaluate(JudgeRequest(input="What is AI?", rubric="be accurate"))
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
                api_key_env="ANTHROPIC_API_KEY",
                base_url="https://api.anthropic.com/v1",
            )
            with pytest.raises(ValueError) as excinfo:
                provider.evaluate(JudgeRequest(input="x", rubric="y"))

            error_msg = str(excinfo.value)
            assert "ANTHROPIC_API_KEY" not in error_msg
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
