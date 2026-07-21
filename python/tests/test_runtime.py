"""Tests for Agent Runtime (agent registration and heartbeat)."""

import json
import time
import unittest
from unittest.mock import MagicMock, patch, PropertyMock
from dataclasses import dataclass


@dataclass
class MockRun:
    id: str = "test-run-id-1234"
    status: str = "running"
    output: str = ""
    trace_id: str = "00000000000000000000000000000001"


class TestRuntimeTraceIDPayload(unittest.TestCase):
    """Test that runtime.py sends trace_id in all run lifecycle payloads."""

    def setUp(self):
        from agentscope_sdk.runtime import Runtime
        self.rt = Runtime(
            agent_id="agent-1",
            agent_name="test-agent",
            instance_id="inst-1",
            client=MagicMock(),
            _base_url="http://localhost:8080",
            _project_token="test-token",
        )
        self.rt._current_run = MockRun()

    @patch("httpx.Client")
    def test_update_run_status_includes_trace_id(self, mock_client_class):
        """update_run_status payload includes trace_id from current run."""
        mock_client = MagicMock()
        mock_client_class.return_value.__enter__.return_value = mock_client
        mock_response = MagicMock()
        mock_response.json.return_value = {"status": "running"}
        mock_client.post.return_value = mock_response

        result = self.rt.update_run_status("waiting_llm", message="thinking")

        call_kwargs = mock_client.post.call_args[1]
        payload = call_kwargs.get("json", {})
        self.assertEqual(payload.get("trace_id"), "00000000000000000000000000000001",
                         "update_run_status payload should include trace_id")
        self.assertEqual(payload.get("status"), "waiting_llm")
        self.assertTrue(result)

    @patch("httpx.Client")
    def test_finish_run_includes_trace_id(self, mock_client_class):
        """finish_run payload includes trace_id."""
        mock_client = MagicMock()
        mock_client_class.return_value.__enter__.return_value = mock_client
        mock_response = MagicMock()
        mock_response.json.return_value = {"status": "completed"}
        mock_client.post.return_value = mock_response

        self.rt.finish_run(output="done", latency_ms=100)

        call_kwargs = mock_client.post.call_args[1]
        payload = call_kwargs.get("json", {})
        self.assertEqual(payload.get("trace_id"), "00000000000000000000000000000001",
                         "finish_run payload should include trace_id")
        self.assertEqual(payload.get("latency_ms"), 100)

    @patch("httpx.Client")
    def test_fail_run_includes_trace_id(self, mock_client_class):
        """fail_run payload includes trace_id."""
        mock_client = MagicMock()
        mock_client_class.return_value.__enter__.return_value = mock_client
        mock_response = MagicMock()
        mock_response.json.return_value = {"status": "failed"}
        mock_client.post.return_value = mock_response

        self.rt.fail_run(error="something broke")

        call_kwargs = mock_client.post.call_args[1]
        payload = call_kwargs.get("json", {})
        self.assertEqual(payload.get("trace_id"), "00000000000000000000000000000001",
                         "fail_run payload should include trace_id")
        self.assertIn("something broke", payload.get("error", ""))


if __name__ == "__main__":
    unittest.main()
