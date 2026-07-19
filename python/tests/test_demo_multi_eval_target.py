"""Tests for multi-eval target registration."""

import pytest
from agentscope_sdk.target import get_target, target


@pytest.fixture(autouse=True)
def reset_target_registry():
    """Clean the target registry before each test."""
    from agentscope_sdk.target import _clear_registry, _save_registry, _restore_registry

    saved = _save_registry()
    _clear_registry()
    yield
    _clear_registry()
    _restore_registry(saved)


def test_multi_eval_target_registered_after_decorator():
    """The multi-eval target is findable after the @target decorator."""

    @target(name="multi-eval-target", version="1.0.0")
    def demo_multi_eval_target(input_str, context=None):
        return f"Processed: {input_str}"

    t = get_target("multi-eval-target")
    assert t.name == "multi-eval-target"
    assert callable(t.func)


def test_multi_eval_target_function_behavior():
    """The multi-eval target returns expected outputs for known inputs."""

    @target(name="multi-eval-target", version="1.0.0")
    def demo_multi_eval_target(input_str, context=None):
        if context and "expected" in context:
            return context["expected"]
        if "hello" in input_str.lower():
            return "Hello! How can I help you today?"
        if "weather" in input_str.lower():
            return "The weather is sunny with a high of 25°C."
        return f"Processed: {input_str}"

    t = get_target("multi-eval-target")
    assert t.func("hello", {}) == "Hello! How can I help you today?"
    assert t.func("weather?", {}) == "The weather is sunny with a high of 25°C."
    assert t.func("anything", {"expected": "expected-answer"}) == "expected-answer"
