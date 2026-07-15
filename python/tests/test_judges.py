"""Tests for judge JSON parsing and provider abstraction."""

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
