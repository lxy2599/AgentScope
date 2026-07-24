"""Tests for target registry."""

import pytest


@pytest.fixture(autouse=True)
def reset_registry():
    """Clear the target registry before and after each test."""
    from agentscope_sdk.target import _clear_registry, _save_registry, _restore_registry

    saved = _save_registry()
    _clear_registry()
    yield
    _clear_registry()
    _restore_registry(saved)


def test_target_decorator_registers_function():
    """Using @target decorator should register the function in the global registry."""
    from agentscope_sdk import target, get_target

    @target(name="test-agent", version="1.0")
    def my_agent(case_input, context):
        return f"processed: {case_input}"

    registered = get_target("test-agent")
    assert registered.name == "test-agent"
    assert registered.version == "1.0"
    assert registered.func is my_agent


def test_get_target_returns_name_version_func():
    """get_target should return a RegisteredTarget with proper fields."""
    from agentscope_sdk import target, get_target, RegisteredTarget

    @target(name="support-agent", version="2026-07-13")
    def support_agent(case_input, context):
        return case_input

    registered = get_target("support-agent")
    assert isinstance(registered, RegisteredTarget)
    assert registered.name == "support-agent"
    assert registered.version == "2026-07-13"
    assert registered.func is support_agent


def test_duplicate_name_raises_error():
    """Registering two targets with the same name should raise TargetRegistrationError."""
    from agentscope_sdk import target, TargetRegistrationError

    @target(name="dup-agent", version="1.0")
    def agent_v1(case_input, context):
        return "v1"

    with pytest.raises(TargetRegistrationError, match="dup-agent"):

        @target(name="dup-agent", version="2.0")
        def agent_v2(case_input, context):
            return "v2"


def test_get_target_not_found_raises_error():
    """get_target for a non-existent name should raise TargetNotFoundError."""
    from agentscope_sdk import get_target, TargetNotFoundError

    with pytest.raises(TargetNotFoundError, match="nonexistent"):
        get_target("nonexistent")


def test_target_called_with_input_and_context():
    """The registered function should receive case_input and context."""
    from agentscope_sdk import target, get_target

    @target(name="echo", version="1.0")
    def echo_agent(case_input, context):
        return f"{case_input}|{context.get('job_id', '')}"

    registered = get_target("echo")
    result = registered.func("hello", {"job_id": "job-1"})
    assert result == "hello|job-1"
