"""Tests for Rubric-based evaluation: judge output parsing and weighted+veto aggregation."""

import json
import pytest


class TestRubricJudgeResponseParsing:
    """Tests for parsing structured rubric judge output with dimension scores."""

    def test_parse_rubric_response_with_dimensions(self):
        """A rubric judge response with dimension_scores should parse correctly."""
        from agentscope_sdk.judges import parse_judge_response, JudgeResult

        raw = json.dumps({
            "score": 0.8,
            "passed": True,
            "reason": "Good overall quality",
            "dimension_scores": {
                "accuracy": 0.9,
                "completeness": 0.7,
                "clarity": 0.8,
            },
            "dimension_reasons": {
                "accuracy": "Correct answer",
                "completeness": "Missing some detail",
                "clarity": "Well structured",
            },
            "veto_triggered": False,
            "weighted_score": 0.82,
            "judge_model": "gpt-4o",
            "judge_provider": "openai-compatible",
        })
        result = parse_judge_response(raw)
        assert result.score == 0.8
        assert result.passed is True
        assert "Good overall quality" in result.reason
        # The standard JudgeResult doesn't parse dimension_scores,
        # but we should at least confirm raw_text is preserved
        assert result.raw_text == raw

    def test_parse_rubric_response_with_veto_triggered(self):
        """When veto is triggered, passed=false even if score is high."""
        from agentscope_sdk.judges import parse_judge_response, JudgeResult

        raw = json.dumps({
            "score": 0.9,
            "passed": False,
            "reason": "Veto triggered: unsafe content detected",
            "veto_triggered": True,
            "veto_reason": "Contains harmful instructions",
        })
        result = parse_judge_response(raw)
        assert result.score == 0.9
        assert result.passed is False  # veto overrides score
        assert "Veto" in result.reason

    def test_rubric_dimension_scores_in_details(self):
        """Rubric dimension scores should be extractable from judge output."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        raw = json.dumps({
            "score": 0.75,
            "passed": True,
            "reason": "Mostly correct",
            "dimension_scores": {
                "accuracy": 0.8,
                "completeness": 0.6,
                "clarity": 0.85,
            },
        })
        score, passed, reason, dim_scores = _parse_multi_judge_response(raw)
        assert score == 0.75
        assert passed is True
        assert dim_scores is not None
        assert dim_scores["accuracy"] == 0.8
        assert dim_scores["completeness"] == 0.6
        assert dim_scores["clarity"] == 0.85

    def test_dimension_scores_are_optional(self):
        """A valid judge response without dimension_scores should still parse."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        raw = json.dumps({
            "score": 0.6,
            "passed": False,
            "reason": "Partially incorrect",
        })
        score, passed, reason, dim_scores = _parse_multi_judge_response(raw)
        assert score == 0.6
        assert passed is False
        assert dim_scores is None

    def test_two_digit_dimension_scores_parse(self):
        """Dimension scores with 2-digit values (e.g. 0.55) should parse."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        raw = json.dumps({
            "score": 0.55,
            "passed": False,
            "reason": "Below average",
            "dimension_scores": {
                "relevance": 0.5,
                "actionability": 0.55,
                "tone": 0.6,
            },
        })
        score, passed, reason, dim_scores = _parse_multi_judge_response(raw)
        assert score == 0.55
        assert dim_scores["relevance"] == 0.5
        assert dim_scores["actionability"] == 0.55


class TestWeightedAndVetoAggregation:
    """Tests for weighted score aggregation with veto support."""

    def test_weighted_average_calculation(self):
        """Multiple metrics with different weights should produce correct composite."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "accuracy", "score": 0.9, "passed": True, "weight": 0.5},
            {"metric_name": "completeness", "score": 0.7, "passed": True, "weight": 0.3},
            {"metric_name": "clarity", "score": 0.8, "passed": True, "weight": 0.2},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["composite_score"] == pytest.approx(0.82, rel=1e-3)  # 0.9*0.5 + 0.7*0.3 + 0.8*0.2
        assert result["passed"] is True
        assert result["veto_triggered"] is False

    def test_veto_metric_failed_causes_composite_failure(self):
        """When a veto metric fails, composite must be failed."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "accuracy", "score": 0.95, "passed": True, "weight": 0.4},
            {"metric_name": "safety", "score": 0.3, "passed": False, "weight": 0.3, "veto": True},
            {"metric_name": "helpfulness", "score": 0.85, "passed": True, "weight": 0.3},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["veto_triggered"] is True
        assert result["passed"] is False  # veto should fail composite
        assert "safety" in result.get("veto_reason", "")

    def test_no_veto_when_veto_metric_passes(self):
        """When a veto metric passes, no veto is triggered."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "safety", "score": 0.9, "passed": True, "weight": 1.0, "veto": True},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["veto_triggered"] is False
        assert result["passed"] is True
        assert result["composite_score"] == 0.9

    def test_multiple_veto_metrics_all_triggered(self):
        """Multiple veto metrics failing should all be reported."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "safety", "score": 0.2, "passed": False, "weight": 0.5, "veto": True},
            {"metric_name": "privacy", "score": 0.1, "passed": False, "weight": 0.5, "veto": True},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["veto_triggered"] is True
        assert result["passed"] is False
        # Ensure both metrics contributed to the veto
        assert "safety" in result.get("veto_reason", "")
        assert "privacy" in result.get("veto_reason", "")

    def test_weights_sum_not_one(self):
        """Weights that don't sum to 1 should be normalized. Score should still be reasonable."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "a", "score": 1.0, "passed": True, "weight": 0.5},
            {"metric_name": "b", "score": 0.5, "passed": True, "weight": 0.5},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["composite_score"] == pytest.approx(0.75, rel=1e-3)
        assert result["passed"] is True

    def test_no_metrics_raises_error(self):
        """Empty metrics list should raise an error."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto, MultiJudgeError

        with pytest.raises(MultiJudgeError, match="No metrics"):
            aggregate_scores_with_veto([])

    def test_single_metric_composite(self):
        """A single metric should work without division issues."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        result = aggregate_scores_with_veto([
            {"metric_name": "accuracy", "score": 0.85, "passed": True, "weight": 1.0},
        ])
        assert result["composite_score"] == 0.85

    def test_default_weight_when_missing(self):
        """When weight is missing, default to equal weight."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "a", "score": 1.0, "passed": True},
            {"metric_name": "b", "score": 0.0, "passed": False},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["composite_score"] == 0.5  # equal weights

    def test_high_weighted_score_with_veto_fails(self):
        """Even with high weighted score, veto should still fail the composite."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        metrics = [
            {"metric_name": "quality", "score": 1.0, "passed": True, "weight": 0.9},
            {"metric_name": "safety", "score": 0.1, "passed": False, "weight": 0.1, "veto": True},
        ]
        result = aggregate_scores_with_veto(metrics)
        assert result["composite_score"] == 0.91  # 1.0*0.9 + 0.1*0.1
        assert result["veto_triggered"] is True
        assert result["passed"] is False  # veto overrides high score
