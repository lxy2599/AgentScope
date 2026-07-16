"""Tests for the demo_eval script."""

from __future__ import annotations

import os
import sys
from unittest.mock import Mock

import httpx
import pytest

# Make the parent directory available so we can import examples/demo_eval
_test_dir = os.path.dirname(os.path.abspath(__file__))
_examples_dir = os.path.join(_test_dir, "..", "examples")
if _examples_dir not in sys.path:
    sys.path.insert(0, _examples_dir)

import demo_eval  # noqa: E402


def _fake_response(status_code: int, json_data: dict) -> Mock:
    """Build a mock httpx.Response."""
    resp = Mock(spec=httpx.Response)
    resp.status_code = status_code
    resp.json.return_value = json_data
    resp.text = str(json_data)
    return resp


class TestRegisterOrLogin:
    """register_or_login handles register success, login fallback, and errors."""

    def test_register_success(self) -> None:
        """Should return user_id when register returns 201."""
        client = Mock(spec=httpx.Client)
        client.post.return_value = _fake_response(201, {"id": "u1", "email": "a@b.c"})

        uid = demo_eval.register_or_login(client)
        assert uid == "u1"

        # Verify register was called
        call_args = client.post.call_args
        assert call_args is not None
        assert call_args[0][0] == "/api/auth/register"

    def test_register_conflict_then_login(self) -> None:
        """Should log in when register returns 409."""
        client = Mock(spec=httpx.Client)
        # First call = register → 409
        # Second call = login → 200
        client.post.side_effect = [
            _fake_response(409, {"error": "exists"}),
            _fake_response(200, {"id": "u1", "email": "a@b.c"}),
        ]

        uid = demo_eval.register_or_login(client)
        assert uid == "u1"

        # Verify both calls were made
        assert client.post.call_count == 2
        assert client.post.call_args_list[0][0][0] == "/api/auth/register"
        assert client.post.call_args_list[1][0][0] == "/api/auth/login"

    def test_register_fails_unknown(self) -> None:
        """Should raise SystemExit when register fails with an unexpected status."""
        client = Mock(spec=httpx.Client)
        client.post.return_value = _fake_response(500, {"error": "server error"})

        with pytest.raises(SystemExit):
            demo_eval.register_or_login(client)

    def test_login_fails_after_409(self) -> None:
        """Should raise SystemExit when login fails after register conflict."""
        client = Mock(spec=httpx.Client)
        client.post.side_effect = [
            _fake_response(409, {"error": "exists"}),
            _fake_response(401, {"error": "invalid credentials"}),
        ]

        with pytest.raises(SystemExit):
            demo_eval.register_or_login(client)


def test_session_client() -> None:
    """session_client creates a client with the right base URL."""
    client = demo_eval.session_client("http://localhost:8080")
    assert client.base_url == "http://localhost:8080"
    assert client.follow_redirects is True
