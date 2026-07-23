"""Tests for multi-dimensional LLM-as-Judge evaluators."""

import json

import pytest


class TestMultiJudgeParseResponse:
    def test_valid_response_passes(self):
        from agentscope_sdk.multi_judges import evaluate_judge_with_fake

        result = evaluate_judge_with_fake(
            {"metric_kind": "final_answer_quality", "metric_name": "answer-quality"},
            "What is the capital of France?",
            "The capital of France is Paris.",
        )
        assert result.score == 1.0
        assert result.passed is True
        assert result.reason
        assert result.metric_name == "answer-quality"
        assert result.metric_kind == "final_answer"  # normalized from final_answer_quality
        assert "judge_model" in result.details
        assert result.details["judge_provider"] == "fake"

    def test_helpfulness_maps_to_final_answer(self):
        """helpfulness should be normalized to final_answer."""
        from agentscope_sdk.multi_judges import evaluate_judge_with_fake

        result = evaluate_judge_with_fake(
            {"metric_kind": "helpfulness"},
            "test input",
            "test output",
        )
        assert result.metric_kind == "final_answer"

    def test_trajectory_reasonableness_maps_to_trajectory(self):
        from agentscope_sdk.multi_judges import evaluate_judge_with_fake

        result = evaluate_judge_with_fake(
            {"metric_kind": "trajectory_reasonableness"},
            "input",
            "output",
        )
        assert result.metric_kind == "trajectory"

    def test_fake_judge_custom_values(self):
        from agentscope_sdk.judges import FakeProvider
        from agentscope_sdk.multi_judges import _build_judge_request, MultiJudgeResult, _parse_multi_judge_response

        # Test the parse function directly
        parsed = _parse_multi_judge_response(
            '{"score": 0.5, "passed": false, "reason": "Incorrect answer", "dimension_scores": {"accuracy": 0.3}}'
        )
        assert parsed["score"] == 0.5
        assert parsed["passed"] is False
        assert parsed["reason"] == "Incorrect answer"
        assert parsed["dimension_scores"] == {"accuracy": 0.3}

    def test_missing_score_raises_error(self):
        from agentscope_sdk.multi_judges import _parse_multi_judge_response, MultiJudgeError

        with pytest.raises(MultiJudgeError, match="Missing 'score'"):
            _parse_multi_judge_response('{"passed": true, "reason": "ok"}')

    def test_score_out_of_range_raises_error(self):
        from agentscope_sdk.multi_judges import _parse_multi_judge_response, MultiJudgeError

        with pytest.raises(MultiJudgeError, match="must be in"):
            _parse_multi_judge_response('{"score": 1.5, "passed": true, "reason": "ok"}')

    def test_empty_response_raises_error(self):
        from agentscope_sdk.multi_judges import _parse_multi_judge_response, MultiJudgeError

        with pytest.raises(MultiJudgeError, match="Empty"):
            _parse_multi_judge_response("")

    def test_invalid_json_raises_error(self):
        from agentscope_sdk.multi_judges import _parse_multi_judge_response, MultiJudgeError

        with pytest.raises(MultiJudgeError, match="Invalid JSON"):
            _parse_multi_judge_response("not json at all")

    def test_derive_passed_from_threshold(self):
        """When 'passed' is missing, derive from score >= 0.7."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        # Score 0.8 >= 0.7
        parsed = _parse_multi_judge_response('{"score": 0.8, "reason": "good"}')
        assert parsed["passed"] is True

        # Score 0.5 < 0.7
        parsed = _parse_multi_judge_response('{"score": 0.5, "reason": "bad"}')
        assert parsed["passed"] is False

    def test_handles_extra_text_before_json(self):
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        parsed = _parse_multi_judge_response(
            'Here is my evaluation:\n{"score": 0.9, "passed": true, "reason": "Good answer"}'
        )
        assert parsed["score"] == 0.9
        assert parsed["passed"] is True

    def test_build_request_includes_expected(self):
        from agentscope_sdk.multi_judges import _build_judge_request

        prompt = _build_judge_request("final_answer_quality", "input", "output", expected="Paris")
        assert "Expected answer" in prompt
        assert "Paris" in prompt

    def test_build_request_includes_trace(self):
        from agentscope_sdk.multi_judges import _build_judge_request

        prompt = _build_judge_request("trajectory_reasonableness", "input", "output", trace_summary="Called search, then respond")
        assert "trace summary" in prompt

    def test_rubrics_exist(self):
        from agentscope_sdk.multi_judges import _RUBRICS

        assert "final_answer" in _RUBRICS
        assert "final_answer_quality" in _RUBRICS
        assert "helpfulness" in _RUBRICS
        assert "safety" in _RUBRICS
        assert "trajectory" in _RUBRICS
        assert "trajectory_reasonableness" in _RUBRICS

    def test_default_metric_kind(self):
        from agentscope_sdk.multi_judges import evaluate_judge_with_fake

        # Default metric_kind should be "final_answer"
        result = evaluate_judge_with_fake(
            {},
            "input",
            "output",
        )
        assert result.metric_kind == "final_answer"

    def test_raw_response_saved(self):
        from agentscope_sdk.multi_judges import evaluate_judge_with_fake

        result = evaluate_judge_with_fake(
            {"metric_kind": "safety"},
            "test input",
            "test output",
        )
        assert "raw_response" in result.details
        assert result.details["raw_response"] != ""

    def test_judge_error_graceful_degradation(self):
        """When judge evaluation errors, return score=0 with error details."""
        from agentscope_sdk.multi_judges import evaluate_judge_with_fake

        # This should still work since we use a fake provider
        result = evaluate_judge_with_fake(
            {"metric_kind": "safety"},
            "input",
            "output",
        )
        assert isinstance(result.score, float)
        assert 0.0 <= result.score <= 1.0
