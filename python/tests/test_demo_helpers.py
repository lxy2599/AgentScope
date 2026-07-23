"""Tests for demo helpers — ensures token scopes and step numbering stay consistent.

Uses file-based inspection rather than module import to avoid PYTHONPATH issues.
"""

from __future__ import annotations

import ast
import os


def _demo_source() -> str:
    """Read the demo source file."""
    test_dir = os.path.dirname(os.path.abspath(__file__))
    # tests/ is at python/tests/, demo is at python/examples/
    demo_path = os.path.join(test_dir, "..", "examples", "real_agent_demo.py")
    with open(demo_path) as f:
        return f.read()


def test_demo_token_scopes_include_agent_register():
    """The demo project token must include 'agent:register' scope."""
    source = _demo_source()
    assert '"agent:register"' in source, (
        "create_project_token() must include 'agent:register' scope. "
        "Current scopes list: ['otlp:write', 'runner:write']"
    )


def test_demo_target_name_defined_before_agent_init():
    """target_name must be defined before calling agent_init()."""
    source = _demo_source()
    lines = source.splitlines()

    agent_init_line = None
    target_name_def = None

    for i, line in enumerate(lines):
        if "agent_name=" in line and "target_name" in line:
            # line 219-ish: agent_name=target_name
            agent_init_line = i
        if 'target_name = "real-agent"' in line:
            target_name_def = i

    assert target_name_def is not None, (
        "target_name = 'real-agent' must be defined in main()."
    )
    assert agent_init_line is not None, (
        "agent_init() must be called in main() with agent_name=target_name."
    )
    assert target_name_def < agent_init_line, (
        f"target_name defined at line {target_name_def + 1} but used at line {agent_init_line + 1}. "
        "Move target_name definition before agent_init()."
    )


def test_step_numbering_consistent():
    """Step numbering must be consistent throughout the demo."""
    source = _demo_source()
    import re

    steps = re.findall(r'\[(\d+)/(\d+)\]', source)
    assert steps, "No step markers found in demo file"

    # All steps should use the same denominator
    denominators = set(d for _, d in steps)
    assert len(denominators) == 1, (
        f"Step numbering inconsistent: found denominators {denominators}. "
        "All steps must use the same N in [step/N]."
    )

    total_steps = int(list(denominators)[0])
    step_numbers = [int(s) for s, _ in steps]
    expected = list(range(1, total_steps + 1))

    assert step_numbers == expected, (
        f"Step numbers {step_numbers} don't match expected {expected}. "
        "Step numbering must be 1/N, 2/N, ..., N/N without gaps or duplicates."
    )


def test_enterprise_demo_reads_clickhouse_connection_from_environment():
    source = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "examples", "enterprise_agentops_demo.py")
    with open(source) as file:
        content = file.read()

    assert 'AGENTSCOPE_CLICKHOUSE_PORT' in content
    assert 'AGENTSCOPE_CLICKHOUSE_DATABASE' in content
