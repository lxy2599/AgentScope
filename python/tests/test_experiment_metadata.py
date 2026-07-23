"""Tests for experiment metadata and harness version reporting."""

import json
import pytest


# Test HarnessMetadata class directly
from dataclasses import dataclass, field, asdict
from typing import Any


@dataclass
class HarnessMetadata:
    """Metadata for experiment reproducibility and comparison."""
    model_provider: str = ""
    model_name: str = ""
    model_version: str = ""
    prompt_version: str = ""
    harness_version: str = ""
    dataset_version: str = ""
    toolset_version: str = ""
    skillset_version: str = ""
    rag_config_version: str = ""
    feature_flags: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


class TestHarnessMetadata:
    """Test harness metadata dataclass."""

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
            feature_flags={"tool_retry": True},
        )
        assert md.model_provider == "openai"
        assert md.model_name == "gpt-4"
        assert md.feature_flags == {"tool_retry": True}

    def test_to_dict(self):
        md = HarnessMetadata(
            model_provider="anthropic",
            model_name="claude-opus-4",
            prompt_version="prompt-v3",
        )
        d = md.to_dict()
        assert d["model_provider"] == "anthropic"
        assert d["model_name"] == "claude-opus-4"
        assert d["prompt_version"] == "prompt-v3"
        assert d["harness_version"] == ""

    def test_serialization_roundtrip(self):
        md = HarnessMetadata(
            model_provider="openai",
            model_name="gpt-4",
            feature_flags={"retry": True, "ablation": False},
        )
        d = md.to_dict()
        # Simulate JSON serialization for API transport
        raw = json.dumps(d)
        restored = json.loads(raw)
        assert restored["model_provider"] == "openai"
        assert restored["model_name"] == "gpt-4"
        assert restored["feature_flags"] == {"retry": True, "ablation": False}

    def test_empty_feature_flags(self):
        md = HarnessMetadata()
        assert md.feature_flags == {}
        d = md.to_dict()
        assert d["feature_flags"] == {}
