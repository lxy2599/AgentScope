"""Tests for rule evaluators."""

import pytest


class TestExactMatch:
    def test_equals_expected_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "exact"}, actual="hello", expected="hello")
        assert result.passed is True
        assert result.score == 1.0
        assert result.reason

    def test_does_not_equal_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "exact"}, actual="hello", expected="world")
        assert result.passed is False
        assert result.score == 0.0
        assert result.reason

    def test_exact_match_alias(self):
        """exact_match should work as an alias for exact."""
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "exact_match"}, actual="hello", expected="hello")
        assert result.passed is True
        assert result.score == 1.0


class TestContains:
    def test_contains_value_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "contains", "value": "hello"}, actual="hello world")
        assert result.passed is True
        assert result.score == 1.0

    def test_does_not_contain_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "contains", "value": "hello"}, actual="goodbye world")
        assert result.passed is False
        assert result.score == 0.0


class TestNotContains:
    def test_does_not_contain_value_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "not_contains", "value": "error"}, actual="all good")
        assert result.passed is True
        assert result.score == 1.0

    def test_contains_value_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "not_contains", "value": "error"}, actual="an error occurred")
        assert result.passed is False
        assert result.score == 0.0


class TestRegex:
    def test_matches_pattern_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "regex", "pattern": r"order-\d+"}, actual="order-42 confirmed")
        assert result.passed is True
        assert result.score == 1.0

    def test_does_not_match_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule({"type": "regex", "pattern": r"order-\d+"}, actual="no order here")
        assert result.passed is False
        assert result.score == 0.0


class TestNumericThreshold:
    def test_field_meets_threshold_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "numeric_threshold", "field": "score", "op": ">=", "value": 0.8},
            actual={"score": 0.95},
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_field_below_threshold_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "numeric_threshold", "field": "score", "op": ">=", "value": 0.8},
            actual={"score": 0.5},
        )
        assert result.passed is False
        assert result.score == 0.0


class TestLatencyThreshold:
    def test_latency_within_limit_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "latency_threshold", "max_ms": 1000},
            metrics={"latency_ms": 500},
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_latency_exceeds_limit_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "latency_threshold", "max_ms": 1000},
            metrics={"latency_ms": 1500},
        )
        assert result.passed is False
        assert result.score == 0.0


class TestEvaluatorConfigError:
    def test_unknown_type_raises_error(self):
        from agentscope_sdk import evaluate_rule, EvaluatorConfigError

        with pytest.raises(EvaluatorConfigError, match="unknown_type"):
            evaluate_rule({"type": "unknown_type"}, actual="test")


class TestToolSequence:
    def test_tools_in_order_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_sequence", "tools": ["search", "read", "respond"]},
            trace={
                "tool_calls": [
                    {"tool_name": "search"},
                    {"tool_name": "read"},
                    {"tool_name": "respond"},
                ]
            },
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_tools_out_of_order_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_sequence", "tools": ["search", "respond"]},
            trace={
                "tool_calls": [
                    {"tool_name": "respond"},
                    {"tool_name": "search"},
                ]
            },
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_missing_tool_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_sequence", "tools": ["search", "read"]},
            trace={
                "tool_calls": [
                    {"tool_name": "search"},
                ]
            },
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_trace_data(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_sequence", "tools": ["search"]},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_tool_calls_in_trace(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_sequence", "tools": ["search"]},
            trace={"tool_calls": []},
        )
        assert result.passed is False
        assert result.score == 0.0


class TestRequiredToolCalled:
    def test_required_tool_called_passes(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "required_tool_called", "tool_name": "search"},
            trace={
                "tool_calls": [
                    {"tool_name": "search"},
                    {"tool_name": "respond"},
                ]
            },
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_required_tool_not_called_fails(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "required_tool_called", "tool_name": "search"},
            trace={
                "tool_calls": [
                    {"tool_name": "respond"},
                ]
            },
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_trace_data(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "required_tool_called", "tool_name": "search"},
        )
        assert result.passed is False

    def test_no_tool_calls_in_trace(self):
        from agentscope_sdk import evaluate_rule

        result = evaluate_rule(
            {"type": "required_tool_called", "tool_name": "search"},
            trace={"tool_calls": []},
        )
        assert result.passed is False



def test_contains_empty_value_raises_error():
    """contains with empty value should raise EvaluatorConfigError."""
    from agentscope_sdk.evaluators import evaluate_rule, EvaluatorConfigError
    with pytest.raises(EvaluatorConfigError):
        evaluate_rule({"type": "contains", "value": ""}, actual="anything")


def test_contains_valid_value_passes():
    """contains with valid value should pass when found."""
    from agentscope_sdk.evaluators import evaluate_rule
    result = evaluate_rule({"type": "contains", "value": "hello"}, actual="hello world")
    assert result.passed
    assert result.score == 1.0


def test_contains_no_match_fails():
    """contains should fail when value not found."""
    from agentscope_sdk.evaluators import evaluate_rule
    result = evaluate_rule({"type": "contains", "value": "xyz"}, actual="hello world")
    assert not result.passed
    assert result.score == 0.0


def _make_trace(retrieval_calls: list[dict]) -> dict:
    return {"retrieval_calls": retrieval_calls}


class TestRetrievalContains:
    """Tests for retrieval_contains evaluator."""

    def test_document_id_hit_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule
        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "DOC-001"},
            trace=_make_trace([
                {"document_ids": ["DOC-001", "DOC-002"], "query_text": "refund", "knowledge_base": "policies"},
            ]),
        )
        assert result.passed is True
        assert result.score == 1.0
        assert "DOC-001" in result.reason

    def test_query_text_hit_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule
        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "refund"},
            trace=_make_trace([
                {"document_ids": ["DOC-003"], "query_text": "refund policy", "knowledge_base": "policies"},
            ]),
        )
        assert result.passed is True
        assert result.score == 1.0
        assert "refund" in result.reason

    def test_knowledge_base_hit_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule
        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "policies"},
            trace=_make_trace([
                {"document_ids": ["DOC-004"], "query_text": "return", "knowledge_base": "company-policies"},
            ]),
        )
        assert result.passed is True
        assert result.score == 1.0
        assert "policies" in result.reason

    def test_no_match_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule
        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "NONEXISTENT"},
            trace=_make_trace([
                {"document_ids": ["DOC-001"], "query_text": "refund", "knowledge_base": "policies"},
            ]),
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_trace_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule
        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "anything"},
            trace=None,
        )
        assert result.passed is False
        assert result.score == 0.0
        assert "No trace data" in result.reason

    def test_empty_retrieval_calls_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule
        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "anything"},
            trace=_make_trace([]),
        )
        assert result.passed is False
        assert result.score == 0.0
        assert "No retrieval calls" in result.reason

    def test_empty_value_raises_error(self):
        from agentscope_sdk.evaluators import evaluate_rule, EvaluatorConfigError
        with pytest.raises(EvaluatorConfigError):
            evaluate_rule(
                {"type": "retrieval_contains", "value": ""},
                trace=_make_trace([{"document_ids": ["X"]}]),
            )
