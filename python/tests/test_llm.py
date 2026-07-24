"""Tests for the LLM client."""

from __future__ import annotations

import os
from unittest.mock import MagicMock, patch

import httpx
import pytest


class TestLLMConfig:
    """Tests for LLM configuration from environment variables."""

    def test_missing_all_env_vars_raises_error(self) -> None:
        """When all env vars are missing, LLMConfigError should list all missing vars."""
        from agentscope_sdk.llm import LLMClient, LLMConfigError

        with patch.dict(os.environ, {}, clear=True):
            with pytest.raises(LLMConfigError) as exc:
                LLMClient()
            msg = str(exc.value)
            assert "AGENTSCOPE_LLM_PROVIDER" in msg
            assert "AGENTSCOPE_LLM_BASE_URL" in msg
            assert "AGENTSCOPE_LLM_API_KEY" in msg
            assert "AGENTSCOPE_LLM_MODEL" in msg

    def test_missing_api_key_alone(self) -> None:
        """When only API key is missing, error should mention only that var."""
        from agentscope_sdk.llm import LLMClient, LLMConfigError

        env = {
            "AGENTSCOPE_LLM_PROVIDER": "deepseek",
            "AGENTSCOPE_LLM_BASE_URL": "https://api.deepseek.com/v1",
            "AGENTSCOPE_LLM_MODEL": "deepseek-chat",
        }
        with patch.dict(os.environ, env, clear=True):
            with pytest.raises(LLMConfigError) as exc:
                LLMClient()
            assert "AGENTSCOPE_LLM_API_KEY" in str(exc.value)

    def test_all_env_vars_set_success(self) -> None:
        """When all env vars are set, LLMClient should initialize without error."""
        from agentscope_sdk.llm import LLMClient

        env = {
            "AGENTSCOPE_LLM_PROVIDER": "deepseek",
            "AGENTSCOPE_LLM_BASE_URL": "https://api.deepseek.com/v1",
            "AGENTSCOPE_LLM_API_KEY": "sk-test123",
            "AGENTSCOPE_LLM_MODEL": "deepseek-chat",
        }
        with patch.dict(os.environ, env, clear=True):
            client = LLMClient()
            assert client.provider == "deepseek"
            assert client.base_url == "https://api.deepseek.com/v1"
            assert client.model == "deepseek-chat"

    def test_constructor_params_override_env(self) -> None:
        """Constructor parameters should take precedence over env vars."""
        from agentscope_sdk.llm import LLMClient

        env = {
            "AGENTSCOPE_LLM_PROVIDER": "deepseek",
            "AGENTSCOPE_LLM_BASE_URL": "https://api.deepseek.com/v1",
            "AGENTSCOPE_LLM_API_KEY": "sk-env",
            "AGENTSCOPE_LLM_MODEL": "deepseek-chat",
        }
        with patch.dict(os.environ, env, clear=True):
            client = LLMClient(api_key="sk-constructor-value")
            assert client.provider == "deepseek"

    def test_provider_model_base_url_properties(self) -> None:
        """Properties should return the configured values."""
        from agentscope_sdk.llm import LLMClient

        env = {
            "AGENTSCOPE_LLM_PROVIDER": "glm",
            "AGENTSCOPE_LLM_BASE_URL": "https://open.bigmodel.cn/api/paas/v4",
            "AGENTSCOPE_LLM_API_KEY": "glm-key",
            "AGENTSCOPE_LLM_MODEL": "glm-4.7-flash",
        }
        with patch.dict(os.environ, env, clear=True):
            client = LLMClient()
            assert client.provider == "glm"
            assert client.model == "glm-4.7-flash"
            assert client.base_url == "https://open.bigmodel.cn/api/paas/v4"


class TestLLMGenerateResponse:
    """Tests for the generate_response method."""

    def test_sends_request_to_chat_completions(self) -> None:
        """The request should be sent to the /chat/completions endpoint."""
        from agentscope_sdk.llm import LLMClient

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            mock_response = MagicMock(spec=httpx.Response)
            mock_response.is_success = True
            mock_response.status_code = 200
            mock_response.json.return_value = {
                "choices": [{"message": {"content": "Paris"}}],
                "usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
            }
            mock_post.return_value = mock_response

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-test",
                model="deepseek-chat",
            )
            client.generate_response([{"role": "user", "content": "What is the capital of France?"}])

            mock_post.assert_called_once()
            call_args = mock_post.call_args
            # URL is the first positional argument
            assert call_args[0][0] == "https://api.deepseek.com/v1/chat/completions"

    def test_sends_auth_header(self) -> None:
        """The request should include an Authorization header."""
        from agentscope_sdk.llm import LLMClient

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            mock_response = MagicMock(spec=httpx.Response)
            mock_response.is_success = True
            mock_response.status_code = 200
            mock_response.json.return_value = {
                "choices": [{"message": {"content": "4"}}],
                "usage": {},
            }
            mock_post.return_value = mock_response

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-secret-key-123",
                model="deepseek-chat",
            )
            client.generate_response([{"role": "user", "content": "2+2?"}])

            call_kwargs = mock_post.call_args.kwargs
            headers = call_kwargs.get("headers", {})
            assert headers.get("Authorization") == "Bearer sk-secret-key-123"

    def test_returns_content_and_metadata(self) -> None:
        """A successful response should return LLMResponse with content and metadata."""
        from agentscope_sdk.llm import LLMClient

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            mock_response = MagicMock(spec=httpx.Response)
            mock_response.is_success = True
            mock_response.status_code = 200
            mock_response.json.return_value = {
                "choices": [{"message": {"content": "Shakespeare"}}],
                "usage": {"prompt_tokens": 15, "completion_tokens": 3, "total_tokens": 18},
            }
            mock_post.return_value = mock_response

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-test",
                model="deepseek-chat",
            )
            result = client.generate_response([{"role": "user", "content": "Who wrote Romeo and Juliet?"}])

            assert result.content == "Shakespeare"
            assert result.provider == "deepseek"
            assert result.model == "deepseek-chat"
            assert result.prompt_tokens == 15
            assert result.completion_tokens == 3
            assert result.total_tokens == 18
            assert result.latency_ms >= 0

    def test_error_response_raises_without_key(self) -> None:
        """A non-2xx response should raise LLMResponseError without leaking the key."""
        from agentscope_sdk.llm import LLMClient, LLMResponseError

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            mock_response = MagicMock(spec=httpx.Response)
            mock_response.is_success = False
            mock_response.status_code = 401
            mock_response.reason_phrase = "Unauthorized"
            mock_response.json.return_value = {"error": {"message": "invalid api key"}}
            mock_post.return_value = mock_response

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-bad-key",
                model="deepseek-chat",
            )
            with pytest.raises(LLMResponseError) as exc:
                client.generate_response([{"role": "user", "content": "test"}])

            msg = str(exc.value)
            assert "401" in msg
            assert "deepseek" in msg
            # Ensure the key itself is NOT in the error message
            assert "sk-bad-key" not in msg

    def test_timeout_raises_clear_error(self) -> None:
        """A timeout should raise LLMResponseError with a clear message."""
        from agentscope_sdk.llm import LLMClient, LLMResponseError

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            import httpx as real_httpx

            mock_post.side_effect = real_httpx.TimeoutException("timed out", request=MagicMock())

            client = LLMClient(
                provider="glm",
                base_url="https://open.bigmodel.cn/api/paas/v4",
                api_key="glm-key",
                model="glm-4.7-flash",
            )
            with pytest.raises(LLMResponseError) as exc:
                client.generate_response([{"role": "user", "content": "test"}])

            assert "timed out" in str(exc.value).lower()

    def test_connection_error_raises_clear_error(self) -> None:
        """A connection error should raise LLMResponseError."""
        from agentscope_sdk.llm import LLMClient, LLMResponseError

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            import httpx as real_httpx

            mock_post.side_effect = real_httpx.ConnectError("Connection refused")

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-test",
                model="deepseek-chat",
            )
            with pytest.raises(LLMResponseError) as exc:
                client.generate_response([{"role": "user", "content": "test"}])

            assert "Connection refused" in str(exc.value)

    def test_passes_temperature(self) -> None:
        """temperature and other kwargs should be included in the request body."""
        from agentscope_sdk.llm import LLMClient

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            mock_response = MagicMock(spec=httpx.Response)
            mock_response.is_success = True
            mock_response.status_code = 200
            mock_response.json.return_value = {
                "choices": [{"message": {"content": "blue"}}],
                "usage": {},
            }
            mock_post.return_value = mock_response

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-test",
                model="deepseek-chat",
            )
            client.generate_response([{"role": "user", "content": "test"}], temperature=0.0)

            call_kwargs = mock_post.call_args.kwargs
            payload = call_kwargs["json"]
            assert payload["temperature"] == 0.0
            assert payload["model"] == "deepseek-chat"
            assert len(payload["messages"]) == 1

    def test_latency_ms_is_measured(self) -> None:
        """latency_ms should be a positive integer measuring elapsed wall-clock time."""
        from agentscope_sdk.llm import LLMClient

        with patch("agentscope_sdk.llm.httpx.post") as mock_post:
            mock_response = MagicMock(spec=httpx.Response)
            mock_response.is_success = True
            mock_response.status_code = 200
            mock_response.json.return_value = {
                "choices": [{"message": {"content": "Paris"}}],
                "usage": {},
            }
            mock_post.return_value = mock_response

            client = LLMClient(
                provider="deepseek",
                base_url="https://api.deepseek.com/v1",
                api_key="sk-test",
                model="deepseek-chat",
            )
            result = client.generate_response([{"role": "user", "content": "test"}])
            assert result.latency_ms >= 0
            assert isinstance(result.latency_ms, int)
