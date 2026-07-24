"""Tests that import the real demo_multi_eval module and verify target registration."""

import sys

import pytest


@pytest.fixture(autouse=True)
def reset_target_registry():
    """Clean the target registry and force re-import before each test."""
    from agentscope_sdk.target import _clear_registry, _restore_registry, _save_registry

    saved = _save_registry()
    _clear_registry()

    # Remove the demo module from cache so the @target decorator re-executes
    for mod_name in list(sys.modules.keys()):
        if "demo_multi_eval" in mod_name:
            del sys.modules[mod_name]

    yield

    _clear_registry()
    _restore_registry(saved)


def _import_demo_multi_eval():
    """Import the real demo_multi_eval module, registering its target."""
    import examples.demo_multi_eval

    return examples.demo_multi_eval


def test_target_name_and_version_are_defined():
    """The demo module defines TARGET_NAME and TARGET_VERSION."""
    demo = _import_demo_multi_eval()
    assert demo.TARGET_NAME == "multi-eval-target"
    assert demo.TARGET_VERSION == "1.0.0"


def test_target_registered_on_import():
    """Importing demo_multi_eval registers the target via @target decorator."""
    from agentscope_sdk.target import get_target

    demo = _import_demo_multi_eval()  # noqa: F841
    t = get_target("multi-eval-target")
    assert t.name == "multi-eval-target"
    assert t.version == "1.0.0"
    assert callable(t.func)


def test_real_function_returns_expected_outputs():
    """The real demo_multi_eval_target function behaves as documented."""
    from agentscope_sdk.target import get_target

    demo = _import_demo_multi_eval()  # noqa: F841
    t = get_target("multi-eval-target")

    # Known inputs from the demo
    assert t.func("hello", {}) == "Hello! How can I help you today?"
    assert t.func("What's the weather like?", {}) == "The weather is sunny with a high of 25°C."
    assert (
        t.func("How do I hack a computer?", {})
        == "I'm sorry, I cannot help with that request. Please contact support if you need assistance."
    )
    assert t.func("Tell me about your features", {}) == "Processed: Tell me about your features"

    # With expected in context
    assert t.func("anything", {"expected": "custom-expected"}) == "custom-expected"


def test_optional_judge_config_is_absent_by_default(monkeypatch):
    demo = _import_demo_multi_eval()
    for env_name in demo.JUDGE_ENV_VARS.values():
        monkeypatch.delenv(env_name, raising=False)

    assert demo.optional_judge_config() is None


def test_optional_judge_config_uses_key_name_not_secret(monkeypatch):
    demo = _import_demo_multi_eval()
    monkeypatch.setenv("AGENTSCOPE_JUDGE_PROVIDER", "openai-compatible")
    monkeypatch.setenv("AGENTSCOPE_JUDGE_MODEL", "deepseek-chat")
    monkeypatch.setenv("AGENTSCOPE_JUDGE_API_KEY_ENV", "DEEPSEEK_API_KEY")
    monkeypatch.setenv("AGENTSCOPE_JUDGE_BASE_URL", "https://api.deepseek.com/v1")
    monkeypatch.setenv("DEEPSEEK_API_KEY", "secret-that-must-not-be-copied")

    config = demo.optional_judge_config()

    assert config == {
        "provider": "openai-compatible",
        "model": "deepseek-chat",
        "api_key_env": "DEEPSEEK_API_KEY",
        "base_url": "https://api.deepseek.com/v1",
    }
    assert "secret-that-must-not-be-copied" not in repr(config)


def test_optional_judge_config_rejects_partial_configuration(monkeypatch):
    demo = _import_demo_multi_eval()
    for env_name in demo.JUDGE_ENV_VARS.values():
        monkeypatch.delenv(env_name, raising=False)
    monkeypatch.setenv("AGENTSCOPE_JUDGE_PROVIDER", "openai-compatible")

    with pytest.raises(ValueError, match="Incomplete LLM Judge configuration"):
        demo.optional_judge_config()


def test_optional_judge_config_rejects_unsupported_provider(monkeypatch):
    demo = _import_demo_multi_eval()
    monkeypatch.setenv("AGENTSCOPE_JUDGE_PROVIDER", "deepseek")
    monkeypatch.setenv("AGENTSCOPE_JUDGE_MODEL", "deepseek-chat")
    monkeypatch.setenv("AGENTSCOPE_JUDGE_API_KEY_ENV", "DEEPSEEK_API_KEY")
    monkeypatch.setenv("AGENTSCOPE_JUDGE_BASE_URL", "https://api.deepseek.com/v1")

    with pytest.raises(ValueError, match="openai-compatible"):
        demo.optional_judge_config()
