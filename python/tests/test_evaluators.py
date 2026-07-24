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


# ============================================================
# contains_expected evaluator tests
# ============================================================


class TestContainsExpected:
    """Tests for contains_expected evaluator.

    The contains_expected evaluator checks if the actual output contains
    the value passed as the ``expected`` parameter (not from config).
    """

    def test_expected_contained_in_actual_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "contains_expected"},
            actual="we can refund your order today",
            expected="refund",
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_expected_not_contained_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "contains_expected"},
            actual="no such thing here",
            expected="refund",
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_expected_none_uses_config_value(self):
        """If expected is None, falls back to config's value field."""
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "contains_expected", "value": "fallback"},
            actual="this uses fallback",
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_expected_none_and_no_config_value_raises_error(self):
        from agentscope_sdk.evaluators import evaluate_rule, EvaluatorConfigError

        with pytest.raises(EvaluatorConfigError):
            evaluate_rule({"type": "contains_expected"}, actual="anything")


# ============================================================
# tool_policy evaluator tests
# ============================================================


class TestToolPolicy:
    """Tests for tool_policy evaluator.

    The tool_policy evaluator reads ``expected_tool`` from case_metadata.
    If expected_tool is set, it checks the trace tool_calls for that tool.
    If expected_tool is empty, it passes with score 1.0 (no tool required).
    """

    def test_required_tool_from_metadata_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={
                "tool_calls": [
                    {"tool_name": "order_lookup", "success": True},
                    {"tool_name": "knowledge_search", "success": True},
                ]
            },
            case_metadata={"expected_tool": "order_lookup"},
        )
        assert result.passed is True
        assert result.score == 1.0

    def test_required_tool_not_called_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={
                "tool_calls": [
                    {"tool_name": "knowledge_search", "success": True},
                ]
            },
            case_metadata={"expected_tool": "order_lookup"},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_expected_tool_fails(self):
        """When expected_tool is empty, fail because metadata is incomplete."""
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={
                "tool_calls": [
                    {"tool_name": "knowledge_search", "success": True},
                ]
            },
            case_metadata={"expected_tool": ""},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_trace_data_when_tool_expected_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace=None,
            case_metadata={"expected_tool": "order_lookup"},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_trace_data_no_required_tool_fails(self):
        """When expected_tool is empty, fail even without trace."""
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace=None,
            case_metadata={"expected_tool": ""},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_empty_trace_tool_calls_with_required_tool_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={"tool_calls": []},
            case_metadata={"expected_tool": "order_lookup"},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_no_case_metadata_fails(self):
        """When case_metadata is not provided, fail."""
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={"tool_calls": [{"tool_name": "order_lookup"}]},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_empty_case_metadata_fails(self):
        """When case_metadata is empty dict, fail."""
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={"tool_calls": [{"tool_name": "order_lookup"}]},
            case_metadata={},
        )
        assert result.passed is False
        assert result.score == 0.0

    def test_expected_tool_name_alignment_with_trace(self):
        """Verify that tool_name in trace matches expected_tool in metadata."""
        from agentscope_sdk.evaluators import evaluate_rule

        # Pass with matching name
        result = evaluate_rule(
            {"type": "tool_policy"},
            trace={"tool_calls": [{"tool_name": "order_lookup"}]},
            case_metadata={"expected_tool": "order_lookup"},
        )
        assert result.passed is True

        # Fail with mismatched name
        result2 = evaluate_rule(
            {"type": "tool_policy"},
            trace={"tool_calls": [{"tool_name": "lookup_order"}]},
            case_metadata={"expected_tool": "order_lookup"},
        )
        assert result2.passed is False


def _make_trace(retrieval_calls: list[dict]) -> dict:
    return {"retrieval_calls": retrieval_calls}


class TestRetrievalContains:
    """Tests for retrieval_contains evaluator."""

    def test_document_id_hit_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "DOC-001"},
            trace=_make_trace(
                [
                    {"document_ids": ["DOC-001", "DOC-002"], "query_text": "refund", "knowledge_base": "policies"},
                ]
            ),
        )
        assert result.passed is True
        assert result.score == 1.0
        assert "DOC-001" in result.reason

    def test_query_text_hit_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "refund"},
            trace=_make_trace(
                [
                    {"document_ids": ["DOC-003"], "query_text": "refund policy", "knowledge_base": "policies"},
                ]
            ),
        )
        assert result.passed is True
        assert result.score == 1.0
        assert "refund" in result.reason

    def test_knowledge_base_hit_passes(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "policies"},
            trace=_make_trace(
                [
                    {"document_ids": ["DOC-004"], "query_text": "return", "knowledge_base": "company-policies"},
                ]
            ),
        )
        assert result.passed is True
        assert result.score == 1.0
        assert "policies" in result.reason

    def test_no_match_fails(self):
        from agentscope_sdk.evaluators import evaluate_rule

        result = evaluate_rule(
            {"type": "retrieval_contains", "value": "NONEXISTENT"},
            trace=_make_trace(
                [
                    {"document_ids": ["DOC-001"], "query_text": "refund", "knowledge_base": "policies"},
                ]
            ),
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
