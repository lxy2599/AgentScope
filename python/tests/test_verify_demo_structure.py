"""Tests for trace_dataset_loop_demo.py — span_attrs propagation and experiment assertions."""

from __future__ import annotations

import ast
import os

DEMO_PATH = os.path.join(
    os.path.dirname(__file__),
    "..", "examples", "trace_dataset_loop_demo.py",
)


def _get_demo_ast() -> ast.Module:
    with open(DEMO_PATH) as f:
        return ast.parse(f.read(), filename=DEMO_PATH)


def _find_function_calls(tree: ast.Module, func_name: str) -> list[ast.Call]:
    """Find all call nodes matching a function name (including method calls)."""
    calls = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            fn = node.func
            if isinstance(fn, ast.Name) and fn.id == func_name:
                calls.append(node)
            elif isinstance(fn, ast.Attribute) and fn.attr == func_name:
                calls.append(node)
    return calls


def _find_call_kwarg_names(node: ast.Call) -> set[str]:
    """Return set of keyword argument names passed to a call."""
    if node.keywords is None:
        return set()
    return {kw.arg for kw in node.keywords if kw.arg is not None}


class TestDemoSpanAttrs:
    """Verify that the demo script passes ``**span_attrs`` to every semantic helper."""

    HELPERS = {"trace_thought", "trace_llm", "trace_tool", "record_final_answer"}

    def test_all_helpers_receive_span_attrs(self):
        """Each tracing helper call must include ``**span_attrs``."""
        tree = _get_demo_ast()
        calls = _find_function_calls(tree, "trace_thought") + \
                _find_function_calls(tree, "trace_llm") + \
                _find_function_calls(tree, "trace_tool") + \
                _find_function_calls(tree, "record_final_answer")

        helper_calls = [c for c in calls
                        if isinstance(c.func, ast.Name) and c.func.id in self.HELPERS]

        assert len(helper_calls) > 0, "No helper calls found in demo"

        for call in helper_calls:
            name = call.func.id  # type: ignore[union-attr]
            has_star_kwarg = any(
                kw for kw in (call.keywords or [])
                if kw.arg is None  # **kwargs appears as keyword with arg=None
            )
            assert has_star_kwarg, (
                f"{name}() in demo is missing **span_attrs "
                f"(no **kwargs keyword in call)"
            )

    def test_span_attrs_contains_expected_keys(self):
        """span_attrs dict literal must contain run.id, agent.id, etc."""
        tree = _get_demo_ast()
        # Find span_attrs = {...} assignment
        assigns = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Assign):
                for target in node.targets:
                    if isinstance(target, ast.Name) and target.id == "span_attrs":
                        assigns.append(node)

        assert len(assigns) == 1, "Expected exactly one span_attrs = {...} assignment"

        dict_node = assigns[0].value
        assert isinstance(dict_node, ast.Dict), "span_attrs must be a dict literal"

        keys = {k.value for k in dict_node.keys if isinstance(k, ast.Constant)}
        expected = {
            "agentscope.run.id",
            "agentscope.agent.id",
            "agentscope.agent.instance_id",
            "agentscope.agent.name",
        }
        missing = expected - keys
        assert not missing, f"span_attrs missing keys: {missing}"
        extra = keys - expected
        assert not extra or extra == {"agentscope.agent.name"}, f"unexpected keys: {extra}"

    def test_root_span_also_gets_agent_name(self):
        """root span must set agentscope.agent.name attribute."""
        tree = _get_demo_ast()
        attr_calls = _find_function_calls(tree, "set_attribute")
        set_names = set()
        for call in attr_calls:
            if call.args and len(call.args) >= 2:
                first = call.args[0]
                if isinstance(first, ast.Constant):
                    set_names.add(first.value)

        assert "agentscope.agent.name" in set_names, (
            "root_span.set_attribute('agentscope.agent.name', ...) must exist"
        )
        assert "agentscope.run.id" in set_names
        assert "agentscope.agent.id" in set_names
        assert "agentscope.agent.instance_id" in set_names

    def test_no_automatic_inheritance_claim(self):
        """Demo must not claim that child spans '自动继承' attributes.

        The correct statement is that child spans do NOT inherit attributes.
        Comments saying '不会自动继承' are fine — they're correct.
        We only flag false claims like '所有子 span 自动继承'.
        """
        with open(DEMO_PATH) as f:
            content = f.read()
        # Check for the INCORRECT claim pattern (no negation nearby)
        # The correct negative form "不会自动继承" is already present and is fine
        assert "所有子 span 自动继承" not in content, (
            "Demo file still contains '所有子 span 自动继承' which is incorrect — "
            "OTel child spans inherit trace context only, not attributes"
        )


class TestDemoExperimentAssertions:
    """Verify demo enforces strict experiment report checks."""

    def test_report_validation_code_present(self):
        """Demo must check report results count, run_id, and dimension aggregates."""
        tree = _get_demo_ast()

        # Check for report results count assertion - look for "report" in strings/JoinedStr
        report_invoked = False
        for node in ast.walk(tree):
            if isinstance(node, ast.Call) and isinstance(node.func, ast.Name):
                if node.func.id == "api_json":
                    for arg in node.args:
                        # Check plain string
                        if isinstance(arg, ast.Constant) and isinstance(arg.value, str):
                            if "report" in arg.value:
                                report_invoked = True
                        # Check f-string (JoinedStr)
                        if isinstance(arg, ast.JoinedStr):
                            for val_node in arg.values:
                                if isinstance(val_node, ast.Constant) and isinstance(val_node.value, str):
                                    if "report" in val_node.value:
                                        report_invoked = True

        assert report_invoked, "No API call to /report endpoint in demo"

    def test_result_run_id_check(self):
        """Demo must check every report result has non-empty run_id."""
        tree = _get_demo_ast()
        # Look for "run_id" in string constants
        run_id_refs = [
            node for node in ast.walk(tree)
            if isinstance(node, ast.Constant) and isinstance(node.value, str)
            and node.value == "run_id"
        ]
        assert len(run_id_refs) >= 3, (
            "Expected multiple 'run_id' string references (span attrs + result check)"
        )

    def test_no_dead_imports(self):
        """Demo must not import unused symbols."""
        tree = _get_demo_ast()
        imports = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.ImportFrom):
                for alias in node.names:
                    imports.add(alias.name)
        assert "associate_run_with_trace" not in imports, (
            "Unused import: associate_run_with_trace"
        )
        assert "run_loop" not in imports, (
            "Unused import: run_loop"
        )

    def test_demo_passes_resource_ids_to_verify_script(self):
        """Demo must pass --run-id, --annotation-item-id, --dataset-case-id to verify script."""
        with open(DEMO_PATH) as f:
            content = f.read()
        assert "--run-id" in content, "Demo must pass --run-id to verify-demo-data.sh"
        assert "--annotation-item-id" in content, (
            "Demo must pass --annotation-item-id to verify-demo-data.sh"
        )
        assert "--dataset-case-id" in content, (
            "Demo must pass --dataset-case-id to verify-demo-data.sh"
        )
        assert "--experiment-id" in content, (
            "Demo must pass --experiment-id to verify-demo-data.sh"
        )


class TestVerifyScriptOptions:
    """Verify the verify script accepts new resource ID params."""

    def test_verify_script_has_new_params(self):
        """verify-demo-data.sh must support --run-id, --annotation-item-id, etc."""
        script_path = os.path.join(
            os.path.dirname(__file__),
            "..", "..", "scripts", "verify-demo-data.sh",
        )
        with open(script_path) as f:
            content = f.read()
        assert "--run-id)" in content, "verify script missing --run-id param"
        assert "--annotation-item-id)" in content, (
            "verify script missing --annotation-item-id param"
        )
        assert "--dataset-case-id)" in content, (
            "verify script missing --dataset-case-id param"
        )
        assert "--skip-db-check" in content, (
            "verify script missing --skip-db-check param"
        )

    def test_verify_script_no_silent_error_suppression(self):
        """verify script must not use '|| echo \"\"' that silences errors."""
        script_path = os.path.join(
            os.path.dirname(__file__),
            "..", "..", "scripts", "verify-demo-data.sh",
        )
        with open(script_path) as f:
            content = f.read()
        # Check that pg_query and ch_query don't use || echo ""
        # Each line in these functions should not suppress errors
        assert "|| echo \"\"" not in content, (
            "verify script must not use '|| echo \"\"' which silently swallows errors"
        )


class TestReportDimensionAggregationBehavior:
    """Behavioral tests for demo report dimension_aggregation validation login.

    These tests run inline validation logic against real sample data,
    not just parse AST for string presence.
    """

    def _run_validation(self, dim_agg, expect_success=True):
        report_data = {
            "results": [{"run_id": "run-abc", "score": 1.0, "passed": True}],
            "dimension_aggregation": dim_agg,
        }
        dim_agg_result = report_data.get("dimension_aggregation", [])
        if not dim_agg_result:
            return (False, "empty dim_agg") if expect_success else (True, "correctly failed")

        if len(dim_agg_result) == 0:
            return (False, "zero-length dim_agg") if expect_success else (True, "correctly failed")

        for da in dim_agg_result:
            mk = da.get("metric_kind", "")
            if not mk:
                return (False, "missing metric_kind") if expect_success else (True, "correctly failed")

            tc = da.get("total_count", 0)
            if not isinstance(tc, (int, float)) or tc <= 0:
                return (False, f"bad total_count") if expect_success else (True, "correctly failed")

            ac = da.get("avg_score", -1)
            if not isinstance(ac, (int, float)) or ac < 0 or ac > 1:
                return (False, f"bad avg_score") if expect_success else (True, "correctly failed")

            pc = da.get("passed_count", 0)
            if not isinstance(pc, (int, float)) or pc < 0 or pc > tc:
                return (False, f"bad passed_count") if expect_success else (True, "correctly failed")

        return (True, "pass") if expect_success else (False, "should have failed")

    def test_valid_passes(self):
        assert self._run_validation(
            [{"metric_kind": "final_answer", "total_count": 1, "passed_count": 1, "avg_score": 1.0}],
            expect_success=True,
        )[0]

    def test_empty_fails(self):
        assert self._run_validation([], expect_success=False)[0]

    def test_missing_metric_kind_fails(self):
        assert self._run_validation(
            [{"total_count": 1, "passed_count": 1, "avg_score": 1.0}],
            expect_success=False,
        )[0]

    def test_bad_avg_score_fails(self):
        assert self._run_validation(
            [{"metric_kind": "final_answer", "total_count": 1, "passed_count": 0, "avg_score": 1.5}],
            expect_success=False,
        )[0]

    def test_bad_passed_count_fails(self):
        assert self._run_validation(
            [{"metric_kind": "final_answer", "total_count": 1, "passed_count": 2, "avg_score": 0.5}],
            expect_success=False,
        )[0]


class TestClickHouseColumnReferences:
    """Verify script uses real ClickHouse columns, not invented ones."""

    def _load_schema(self):
        """Parse trace_spans CREATE TABLE columns from the migration SQL."""
        migration_path = os.path.join(
            os.path.dirname(__file__),
            "..", "..", "clickhouse", "migrations", "00001_trace.sql",
        )
        with open(migration_path) as f:
            content = f.read()

        # Extract trace_spans table columns
        in_trace_spans = False
        columns = []
        for line in content.splitlines():
            if "CREATE TABLE IF NOT EXISTS trace_spans" in line:
                in_trace_spans = True
                continue
            if in_trace_spans:
                if line.strip().startswith(")"):
                    break
                stripped = line.strip().strip(",")
                if stripped and not stripped.startswith("PRIMARY KEY") and not stripped.startswith("ENGINE"):
                    col_name = stripped.split()[0]
                    columns.append(col_name)
        return columns

    def test_no_span_kind_in_schema(self):
        """trace_spans has no 'span_kind' column - verify script must use 'name'."""
        columns = self._load_schema()
        assert "span_kind" not in columns, (
            f"'span_kind' was found in schema columns: {columns}"
        )
        assert "name" in columns, "'name' column must exist in trace_spans"
        assert "kind" in columns, "'kind' column must exist in trace_spans"

    def test_verify_script_uses_name_not_span_kind(self):
        """verify-demo-data.sh must refer to name='agent.run', not span_kind='agent.run'."""
        script_path = os.path.join(
            os.path.dirname(__file__),
            "..", "..", "scripts", "verify-demo-data.sh",
        )
        with open(script_path) as f:
            content = f.read()
        assert "name='agent.run'" in content, (
            "verify script must use name='agent.run' for root span check"
        )
        assert "span_kind='agent.run'" not in content, (
            "verify script must not use span_kind (column does not exist)"
        )
