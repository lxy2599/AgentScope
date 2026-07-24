"""Tests for Rubric-based evaluation: judge output parsing and weighted+veto aggregation."""

import json
import pytest


class TestRubricJudgeResponseParsing:
    """Tests for parsing structured rubric judge output with dimension scores."""

    def test_parse_rubric_response_with_dimensions(self):
        """A rubric judge response with dimension_scores should parse correctly."""
        from agentscope_sdk.judges import parse_judge_response, JudgeResult

        raw = json.dumps(
            {
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
            }
        )
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

        raw = json.dumps(
            {
                "score": 0.9,
                "passed": False,
                "reason": "Veto triggered: unsafe content detected",
                "veto_triggered": True,
                "veto_reason": "Contains harmful instructions",
            }
        )
        result = parse_judge_response(raw)
        assert result.score == 0.9
        assert result.passed is False  # veto overrides score
        assert "Veto" in result.reason

    def test_rubric_dimension_scores_in_details(self):
        """Rubric dimension scores should be extractable from judge output."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        raw = json.dumps(
            {
                "score": 0.75,
                "passed": True,
                "reason": "Mostly correct",
                "dimension_scores": {
                    "accuracy": 0.8,
                    "completeness": 0.6,
                    "clarity": 0.85,
                },
            }
        )
        parsed = _parse_multi_judge_response(raw)
        assert parsed["score"] == 0.75
        assert parsed["passed"] is True
        assert parsed["dimension_scores"] is not None
        assert parsed["dimension_scores"]["accuracy"] == 0.8
        assert parsed["dimension_scores"]["completeness"] == 0.6
        assert parsed["dimension_scores"]["clarity"] == 0.85

    def test_dimension_scores_are_optional(self):
        """A valid judge response without dimension_scores should still parse."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        raw = json.dumps(
            {
                "score": 0.6,
                "passed": False,
                "reason": "Partially incorrect",
            }
        )
        parsed = _parse_multi_judge_response(raw)
        assert parsed["score"] == 0.6
        assert parsed["passed"] is False
        assert "dimension_scores" not in parsed

    def test_two_digit_dimension_scores_parse(self):
        """Dimension scores with 2-digit values (e.g. 0.55) should parse."""
        from agentscope_sdk.multi_judges import _parse_multi_judge_response

        raw = json.dumps(
            {
                "score": 0.55,
                "passed": False,
                "reason": "Below average",
                "dimension_scores": {
                    "relevance": 0.5,
                    "actionability": 0.55,
                    "tone": 0.6,
                },
            }
        )
        parsed = _parse_multi_judge_response(raw)
        assert parsed["score"] == 0.55
        assert parsed["dimension_scores"]["relevance"] == 0.5
        assert parsed["dimension_scores"]["actionability"] == 0.55

    def test_boolean_score_is_not_treated_as_one(self):
        from agentscope_sdk.multi_judges import (
            MultiJudgeError,
            _parse_multi_judge_response,
        )

        raw = json.dumps(
            {
                "score": True,
                "passed": True,
                "reason": "not numeric",
            }
        )
        with pytest.raises(MultiJudgeError, match="score"):
            _parse_multi_judge_response(raw)

    @pytest.mark.parametrize(
        "dimension_score",
        [float("nan"), float("inf"), True, "NaN", -0.01, 1.01],
    )
    def test_invalid_dimension_scores_are_rejected(self, dimension_score):
        from agentscope_sdk.multi_judges import (
            MultiJudgeError,
            _parse_multi_judge_response,
        )

        raw = json.dumps(
            {
                "score": 0.8,
                "passed": True,
                "reason": "invalid dimension",
                "dimension_scores": {"safety": dimension_score},
            }
        )
        with pytest.raises(MultiJudgeError, match="dimension_scores"):
            _parse_multi_judge_response(raw)

    def test_non_boolean_veto_signal_is_rejected(self):
        from agentscope_sdk.multi_judges import (
            MultiJudgeError,
            _parse_multi_judge_response,
        )

        raw = json.dumps(
            {
                "score": 0.9,
                "passed": True,
                "reason": "ambiguous veto",
                "veto_triggered": "false",
            }
        )
        with pytest.raises(MultiJudgeError, match="veto_triggered"):
            _parse_multi_judge_response(raw)

    def test_malformed_structured_veto_fails_closed_in_evaluator(
        self,
        monkeypatch,
    ):
        from agentscope_sdk.judges import JudgeResult
        from agentscope_sdk.multi_judges import evaluate_judge

        class MalformedVetoProvider:
            def __init__(self, **_kwargs):
                pass

            def evaluate(self, _request):
                return JudgeResult(
                    score=0.9,
                    passed=True,
                    reason="claimed pass",
                    raw_text=json.dumps(
                        {
                            "score": 0.9,
                            "passed": True,
                            "reason": "claimed pass",
                            "veto_triggered": "false",
                        }
                    ),
                )

        monkeypatch.setattr(
            "agentscope_sdk.multi_judges.OpenAICompatibleProvider",
            MalformedVetoProvider,
        )
        result = evaluate_judge(
            {"provider": "openai-compatible", "metric_kind": "safety"},
            "input",
            "output",
        )

        assert result.score == 0.0
        assert result.passed is False
        assert "veto_triggered" in result.reason


class TestWeightedAndVetoAggregation:
    """Tests for weighted score aggregation with veto support."""

    def test_explicit_threshold_overrides_provider_passed_and_triggers_veto(self):
        """A local threshold is authoritative over the provider's passed flag."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        result = aggregate_scores_with_veto(
            [
                {
                    "metric_name": "safety",
                    "score": 0.75,
                    "passed": True,
                    "weight": 1.0,
                    "pass_threshold": 0.8,
                    "veto": True,
                },
            ]
        )

        assert result["metric_scores"][0]["passed"] is False
        assert result["veto_triggered"] is True
        assert result["passed"] is False

    def test_provider_passed_is_preserved_without_per_metric_threshold(self):
        """Metrics without a local threshold retain their existing semantics."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        result = aggregate_scores_with_veto(
            [
                {
                    "metric_name": "safety",
                    "score": 0.75,
                    "passed": True,
                    "weight": 1.0,
                    "veto": True,
                },
            ]
        )

        assert result["metric_scores"][0]["passed"] is True
        assert result["veto_triggered"] is False
        assert result["passed"] is True

    @pytest.mark.parametrize(
        ("field", "value"),
        [
            ("score", float("nan")),
            ("score", float("inf")),
            ("score", -0.01),
            ("score", 1.01),
            ("pass_threshold", float("nan")),
            ("pass_threshold", float("inf")),
            ("pass_threshold", -0.01),
            ("pass_threshold", 1.01),
            ("weight", float("nan")),
            ("weight", float("inf")),
            ("weight", -0.01),
        ],
    )
    def test_invalid_metric_numeric_fields_are_rejected(self, field, value):
        """Scores, thresholds, and weights must be finite and in range."""
        from agentscope_sdk.multi_judges import (
            MultiJudgeError,
            aggregate_scores_with_veto,
        )

        metric = {
            "metric_name": "quality",
            "score": 0.8,
            "passed": True,
            "weight": 1.0,
            "pass_threshold": 0.7,
        }
        metric[field] = value

        with pytest.raises(MultiJudgeError, match=field):
            aggregate_scores_with_veto([metric])

    def test_finite_weights_above_one_remain_compatible(self):
        """Weights are relative, so finite values above one must remain valid."""
        from agentscope_sdk.multi_judges import aggregate_scores_with_veto

        result = aggregate_scores_with_veto(
            [
                {
                    "metric_name": "primary",
                    "score": 1.0,
                    "passed": True,
                    "weight": 2.0,
                },
                {
                    "metric_name": "secondary",
                    "score": 0.5,
                    "passed": False,
                    "weight": 1.0,
                },
            ]
        )

        assert result["composite_score"] == pytest.approx(0.8333, abs=1e-4)
        assert result["metric_scores"][0]["weight"] == 2.0

    @pytest.mark.parametrize(
        ("field", "value"),
        [
            ("temperature", float("nan")),
            ("temperature", float("inf")),
            ("max_tokens", True),
            ("max_tokens", 0),
            ("max_tokens", float("inf")),
        ],
    )
    def test_invalid_judge_request_config_fails_before_network(
        self,
        field,
        value,
    ):
        from agentscope_sdk.multi_judges import MultiJudgeError, evaluate_judge

        config = {
            "provider": "openai-compatible",
            "metric_kind": "safety",
            field: value,
        }
        with pytest.raises(MultiJudgeError, match=field):
            evaluate_judge(config, "input", "output")

    @pytest.mark.parametrize(
        "threshold",
        [float("nan"), float("inf"), -0.01, 1.01],
    )
    def test_invalid_default_pass_threshold_is_rejected(self, threshold):
        """The composite threshold must be finite and in [0, 1]."""
        from agentscope_sdk.multi_judges import (
            MultiJudgeError,
            aggregate_scores_with_veto,
        )

        with pytest.raises(MultiJudgeError, match="default_pass_threshold"):
            aggregate_scores_with_veto(
                [{"metric_name": "quality", "score": 0.8, "passed": True}],
                default_pass_threshold=threshold,
            )

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
            {"metric_name": "a", "score": 1.0, "passed": True, "weight": 1.0},
            {"metric_name": "b", "score": 0.5, "passed": True, "weight": 1.0},
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

        result = aggregate_scores_with_veto(
            [
                {"metric_name": "accuracy", "score": 0.85, "passed": True, "weight": 1.0},
            ]
        )
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
