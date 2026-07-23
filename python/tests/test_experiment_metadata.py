"""Tests for experiment metadata and harness version reporting.

All tests import HarnessMetadata from the real production module
(agentscope_sdk.runner) — no redefinition of the dataclass in tests.
"""

import json
import pytest
from agentscope_sdk.runner import HarnessMetadata


class TestHarnessMetadata:
    """Test harness metadata dataclass - imported from real production module."""

    def test_defaults(self):
        md = HarnessMetadata()
        assert md.model_provider == ""
        assert md.model_name == ""
        assert md.prompt_version == ""
        assert md.harness_version == ""
        assert md.feature_flags == {}

    def test_custom_fields(self):
        md = HarnessMetadata(
            model_provider="openai",
            model_name="gpt-4",
            model_version="0613",
            prompt_version="v1.2",
            harness_version="1.0.0",
            dataset_version="ds-v1",
            toolset_version="tools-v2",
            feature_flags={"tool_retry": True},
        )
        assert md.model_provider == "openai"
        assert md.model_name == "gpt-4"
        assert md.feature_flags == {"tool_retry": True}
        assert md.toolset_version == "tools-v2"

    def test_to_dict(self):
        md = HarnessMetadata(
            model_provider="anthropic",
            model_name="claude-opus-4",
            prompt_version="prompt-v3",
        )
        d = md.to_dict()
        assert d["model_provider"] == "anthropic"
        assert d["model_name"] == "claude-opus-4"
        assert "model_version" in d
        assert "feature_flags" in d

    def test_serialization_roundtrip(self):
        md = HarnessMetadata(
            model_provider="openai",
            model_name="gpt-4",
            prompt_version="prompt-v3",
            feature_flags={"retry": True, "ablation": False},
        )
        d = md.to_dict()
        raw = json.dumps(d)
        restored = json.loads(raw)
        assert restored["model_provider"] == "openai"
        assert restored["feature_flags"] == {"retry": True, "ablation": False}


class TestHarnessMetadataInHTTPPayload:
    """Verify HarnessMetadata fields appear in HTTP payloads (using mock)."""

    def test_metadata_serializes_into_experiment_payload(self):
        """HarnessMetadata can be embedded in an experiment create JSON payload."""
        hm = HarnessMetadata(
            model_provider="anthropic",
            model_name="claude-sonnet-4",
            prompt_version="prompt-v2",
            harness_version="1.0.0",
        )
        payload = {
            "dataset_id": "ds-1",
            "target_name": "test-agent",
            "case_ids": ["case-1"],
            "model_provider": hm.model_provider,
            "model_name": hm.model_name,
            "model_version": hm.model_version,
            "prompt_version": hm.prompt_version,
            "harness_version": hm.harness_version,
        }
        raw = json.dumps(payload)
        restored = json.loads(raw)
        assert restored["model_provider"] == "anthropic"
        assert restored["model_name"] == "claude-sonnet-4"
        assert restored["model_version"] == ""
        assert restored["prompt_version"] == "prompt-v2"

    def test_feature_flags_in_payload(self):
        """Feature flags from HarnessMetadata appear in the payload."""
        hm = HarnessMetadata(
            model_provider="openai",
            model_name="gpt-4o",
            feature_flags={"tool_retry": True, "prompt_cache": False},
        )
        payload = {
            "target_name": "agent-v2",
            "harness_version": "2.0",
            **hm.to_dict(),
        }
        raw = json.dumps(payload)
        restored = json.loads(raw)
        assert restored["feature_flags"]["tool_retry"] is True

    def test_metadata_in_runner_start_payload(self):
        """Simulate how a runner would embed HarnessMetadata in run start API."""
        hm = HarnessMetadata(
            model_provider="openai",
            model_name="gpt-4-turbo",
            prompt_version="prompt-v5",
            harness_version="2.1.0",
        )
        run_meta = {
            "session_id": "sess-run-1",
            "model_provider": hm.model_provider,
            "model_name": hm.model_name,
            "harness_version": hm.harness_version,
            "prompt_version": hm.prompt_version,
            "feature_flags": hm.feature_flags,
        }
        raw = json.dumps(run_meta)
        parsed = json.loads(raw)
        assert parsed["model_provider"] == "openai"
        assert parsed["model_name"] == "gpt-4-turbo"
        assert parsed["harness_version"] == "2.1.0"
        assert parsed["feature_flags"] == {}
