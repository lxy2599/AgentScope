#!/usr/bin/env python3
"""Export Terminal-Bench task metadata for AgentScope dataset import.

The task source stays outside the application database. This exporter keeps the
database payload small and safe: it stores the instruction, stable task ID,
resource metadata, and a verifier overview, but never copies verifier source or
expected answers into the API payload.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import tomllib
from pathlib import Path
from typing import Any


def _read_toml(path: Path) -> dict[str, Any]:
    with path.open("rb") as stream:
        value = tomllib.load(stream)
    if not isinstance(value, dict):
        raise ValueError(f"{path} must contain a TOML table")
    return value


def _first_command(test_script: str) -> str:
    candidates: list[str] = []
    for raw_line in test_script.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or line.startswith(("set ", "mkdir ", "rm ", "echo ", "printf ")):
            continue
        if line.startswith(("then", "else", "fi", "function ", "{")) or re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", line):
            continue
        normalized = re.sub(r"\s+", " ", line)
        command = re.sub(r"^(?:if|while)\s+", "", normalized)
        candidates.append(command)
        if any(token in command for token in ("pytest", "test_outputs", "test_state", "test_scoring", "npm ", "pnpm ", "cargo test")):
            return command.removesuffix("; then")[:500]
    if candidates:
        return candidates[0][:500]
    return "由题目镜像执行官方 verifier"


def _dockerfile_dependencies(path: Path) -> list[str]:
    if not path.is_file():
        return []
    dependencies: list[str] = []
    for raw_line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if re.search(r"\b(apt-get|apt|pip|uv|npm|pnpm|yarn|mamba|conda|cargo)\s+(install|pip install|add|fetch|build)", line, re.IGNORECASE):
            line = re.sub(r"\s+", " ", line)
            dependencies.append(line[:300])
    return dependencies[:20]


def _verifier_overview(task_dir: Path) -> dict[str, Any]:
    test_dir = task_dir / "tests"
    scripts = sorted(test_dir.glob("test.sh"))
    script_text = scripts[0].read_text(encoding="utf-8", errors="replace") if scripts else ""
    files = sorted(str(path.relative_to(task_dir)) for path in test_dir.iterdir() if path.is_file()) if test_dir.is_dir() else []
    checks = ["执行题目官方 verifier 入口", "生成 CTRF 测试报告", "读取 reward.txt 或 reward.json 作为官方得分"]
    if "pytest" in script_text:
        checks.insert(1, "运行 pytest 测试并依据退出码判定")
    if "npm" in script_text or "pnpm" in script_text:
        checks.insert(1, "运行题目定义的 Node.js 测试或构建流程")
    return {
        "entrypoint": "tests/test.sh",
        "command": _first_command(script_text),
        "files": files[:100],
        "checks": checks,
        "dependencies": _dockerfile_dependencies(test_dir / "Dockerfile"),
        "runtime_install_policy": "static verifier dependencies must be baked into tests/Dockerfile; task-defined dynamic inputs remain task logic",
    }


def export_cases(tasks_root: Path) -> list[dict[str, Any]]:
    if not tasks_root.is_dir():
        raise ValueError(f"tasks root does not exist: {tasks_root}")
    cases: list[dict[str, Any]] = []
    for task_file in sorted(tasks_root.glob("*/task.toml")):
        task_dir = task_file.parent
        document = _read_toml(task_file)
        task = document.get("task")
        metadata = document.get("metadata")
        verifier = document.get("verifier")
        agent = document.get("agent")
        environment = document.get("environment")
        if not isinstance(task, dict) or not isinstance(metadata, dict):
            continue
        task_id = task_dir.name
        instruction_path = task_dir / "instruction.md"
        if not instruction_path.is_file():
            continue
        instruction = instruction_path.read_text(encoding="utf-8")
        safe_metadata: dict[str, Any] = {
            "task_id": task_id,
            "task_name": task.get("name", f"terminal-bench/{task_id}"),
            "description": task.get("description"),
            "authors": task.get("authors"),
            "category": metadata.get("category"),
            "subcategory": metadata.get("subcategory"),
            "tags": metadata.get("tags", []),
            "task_schema_version": document.get("schema_version"),
            "artifacts": task.get("artifacts"),
            "expert_time_estimate_hours": metadata.get("expert_time_estimate_hours"),
            "expert_time_estimate_min": metadata.get("expert_time_estimate_min"),
            "junior_time_estimate_hours": metadata.get("junior_time_estimate_hours"),
            "junior_time_estimate_min": metadata.get("junior_time_estimate_min"),
            "verifier_timeout_sec": verifier.get("timeout_sec") if isinstance(verifier, dict) else None,
            "verifier_environment_mode": verifier.get("environment_mode") if isinstance(verifier, dict) else None,
            "agent_timeout_sec": agent.get("timeout_sec") if isinstance(agent, dict) else None,
            "environment": {
                key: environment.get(key)
                for key in ("cpus", "memory_mb", "storage_mb", "gpus", "build_timeout_sec", "mcp_servers")
                if isinstance(environment, dict) and key in environment
            },
            "verifier": _verifier_overview(task_dir),
        }
        safe_metadata = {key: value for key, value in safe_metadata.items() if value not in (None, [], {})}
        cases.append({"input": instruction, "metadata": safe_metadata})
    return cases


def main() -> int:
    parser = argparse.ArgumentParser(description="Export Terminal-Bench task cases without verifier answers")
    parser.add_argument("tasks_root", type=Path, help="path to Terminal-Bench tasks/ directory")
    parser.add_argument("--output", type=Path, required=True, help="JSON output path")
    args = parser.parse_args()
    try:
        cases = export_cases(args.tasks_root)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps({"schema_version": "agentlab.terminal-bench-cases.v1", "cases": cases}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(f"exported {len(cases)} Terminal-Bench case(s) to {args.output}", file=sys.stderr)
        return 0
    except (OSError, ValueError, tomllib.TOMLDecodeError) as exc:
        print(f"export_terminal_bench_cases: error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
