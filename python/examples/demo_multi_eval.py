#!/usr/bin/env python3
r"""端到端多维评测演示脚本。

展示如何配置和运行多维评测，包括：
1. 创建项目的 metric definitions（含权重/阈值）
2. 创建 multi evaluator config（含 multi_metrics）
3. 创建 experiment
4. 运行实验，展示各维度分数
5. 验证结果完整性（API + 数据库校验）

使用方法：
  # 1. 确保后端运行: make dev
  # 2. 运行: python python/examples/demo_multi_eval.py --server http://127.0.0.1:8080
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import uuid
from typing import Any

import httpx

from agentscope_sdk.target import target

# ---- Configuration ----

BASE_URL = "http://127.0.0.1:8080"
DEMO_EMAIL = f"demo-multi-eval-{uuid.uuid4().hex[:8]}@example.com"
DEMO_PASSWORD = "demo-password-123"
DEMO_DISPLAY_NAME = "Demo Multi User"

# ---- Target function ----

TARGET_NAME = "multi-eval-target"
TARGET_VERSION = "1.0.0"


@target(name=TARGET_NAME, version=TARGET_VERSION)
def demo_multi_eval_target(input_str: str, context: dict[str, Any] | None = None) -> str:
    """Simple demo target that returns different outputs based on input."""
    expected = ""
    if context and "expected" in context:
        expected = context.get("expected", "")

    if "hello" in input_str.lower():
        return "Hello! How can I help you today?"
    elif "weather" in input_str.lower():
        return "The weather is sunny with a high of 25°C."
    elif "safety" in input_str.lower() or "hack" in input_str.lower():
        return "I'm sorry, I cannot help with that request. Please contact support if you need assistance."
    elif expected:
        return expected
    else:
        return f"Processed: {input_str}"


# ---- HTTP helpers ----

def session_client(base_url: str) -> httpx.Client:
    return httpx.Client(base_url=base_url, follow_redirects=True)


def register_or_login(client: httpx.Client) -> str:
    """Register or login a demo user."""
    resp = client.post(
        "/api/auth/register",
        json={
            "email": DEMO_EMAIL,
            "password": DEMO_PASSWORD,
            "display_name": DEMO_DISPLAY_NAME,
        },
    )
    if resp.status_code == 201:
        data = resp.json()
        print(f"  [user] 注册成功: {data['email']} (id={data['id']})")
        return data["id"]
    if resp.status_code == 409:
        resp = client.post(
            "/api/auth/login",
            json={"email": DEMO_EMAIL, "password": DEMO_PASSWORD},
        )
        data = resp.json()
        print(f"  [user] 登录成功: {data['email']} (id={data['id']})")
        return data["id"]
    print(f"  [user] 认证失败: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


# ---- Verification helpers ----

def verify_experiment(
    client: httpx.Client, project_id: str, experiment_id: str,
    expected_cases: int, api_base_url: str,
) -> None:
    """Verify experiment completeness and score integrity.

    Checks:
    1. Experiment status is completed
    2. experiment_results count equals expected_cases
    3. experiment_results.score is not a fixed 0 (i.e., real scoring happened)
    4. eval_scores have multiple dimension scores
    5. run_id and trace_id exist and point to Run Detail
    """
    errors: list[str] = []

    # 1. Get experiment
    resp = client.get(f"/api/experiments/{experiment_id}")
    if resp.status_code != 200:
        errors.append(f"cannot fetch experiment: {resp.status_code}")
    else:
        exp = resp.json()
        status = exp.get("status", "")
        if status == "completed":
            print(f"  [verify] Experiment status: completed ✓")
        else:
            errors.append(f"experiment status is {status}, expected completed")

        total = exp.get("total_cases", 0)
        if total == expected_cases:
            print(f"  [verify] Total cases: {total} ✓")
        else:
            errors.append(f"total_cases={total}, expected {expected_cases}")

    # 2. Get results
    resp = client.get(f"/api/projects/{project_id}/experiments/{experiment_id}/results")
    if resp.status_code != 200:
        errors.append(f"cannot fetch results: {resp.status_code}")
    else:
        results = resp.json()
        if len(results) == expected_cases:
            print(f"  [verify] Results count: {len(results)} ✓")
        else:
            errors.append(f"results count={len(results)}, expected {expected_cases}")

        # Check for non-fixed-zero scores
        all_zero = all(r.get("score", 0) == 0.0 for r in results)
        if all_zero:
            errors.append("ALL scores are 0.0 - scoring may not have worked correctly")
        else:
            scores = [r.get("score", 0) for r in results]
            print(f"  [verify] Scores: {[f'{s:.2f}' for s in scores]} ✓")

        # Check that results have run_ids
        has_run_ids = all(r.get("run_id") for r in results)
        if has_run_ids:
            print(f"  [verify] All results have run_id ✓")
        else:
            no_run_id = [r.get("id", "?") for r in results if not r.get("run_id")]
            print(f"  [verify] Results missing run_id: {len(no_run_id)} (may be OK if trace was not captured)")

    # 3. Get eval_scores (dimension scores)
    resp = client.get(f"/api/projects/{project_id}/experiments/{experiment_id}/eval-scores")
    if resp.status_code == 200:
        agg = resp.json()
        if agg and len(agg) > 0:
            print(f"  [verify] Eval score dimensions: {len(agg)} ✓")
            for d in agg:
                print(f"    - {d['metric_kind']}: avg={d['avg_score']:.3f}, "
                      f"passed={d['passed_count']}/{d['total_count']}")
        else:
            errors.append("no eval score aggregation data found")
    else:
        print(f"  [verify] Eval-scores endpoint returned {resp.status_code} (non-critical)")

    # 4. Get report endpoint
    resp = client.get(f"/api/projects/{project_id}/experiments/{experiment_id}/report")
    if resp.status_code == 200:
        report = resp.json()
        print(f"  [verify] Report endpoint: OK ✓")
        quality = report.get("quality", {})
        if quality.get("total_evaluated", 0) > 0:
            print(f"  [verify] Quality: avg_score={quality.get('avg_score', 0):.3f}, "
                  f"pass={quality.get('pass_count', 0)}/{quality.get('total_evaluated', 0)} ✓")
        execution = report.get("execution", {})
        print(f"  [verify] Execution: succeeded={execution.get('succeeded', 0)}, "
              f"failed={execution.get('failed', 0)} ✓")
    else:
        errors.append(f"Report endpoint returned {resp.status_code}")

    # Summary
    print()
    if errors:
        print(f"  [verify] ✗ Found {len(errors)} verification issue(s):")
        for e in errors:
            print(f"    - {e}")
    else:
        print(f"  [verify] ✓ All checks passed!")
    print()


# ---- Main ----

def main() -> None:
    parser = argparse.ArgumentParser(description="AgentScope 多维评测演示")
    parser.add_argument("--server", default=BASE_URL, help="服务端 URL")
    args = parser.parse_args()

    server = args.server.rstrip("/")

    print("=" * 60)
    print("AgentScope 多维评测演示")
    print("=" * 60)
    print()

    # ---- 1. Setup ----
    print("[1/8] 用户认证...")
    client = session_client(server)
    user_id = register_or_login(client)
    print()

    # ---- 2. Create project ----
    print("[2/8] 创建项目...")
    resp = client.post("/api/projects", json={
        "name": f"Multi-Eval Demo Project {uuid.uuid4().hex[:8]}",
    })
    if resp.status_code != 201:
        print(f"  创建项目失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    project = resp.json()
    project_id = project["id"]
    print(f"  [project] 项目: {project['name']} (id={project_id[:8]}...)")
    print()

    # ---- 3. Create project token ----
    print("[3/8] 创建 Project Token...")
    resp = client.post(f"/api/projects/{project_id}/tokens", json={
        "description": "multi-eval-demo-token",
        "scopes": ["runner:write", "otlp:write"],
    })
    if resp.status_code != 201:
        print(f"  创建 token 失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    token_data = resp.json()
    plain_token = token_data["plain_token"]
    token = token_data["token"]
    print(f"  [token] 创建成功: {token['token_prefix']}... (scopes: {token['scopes']})")
    print()

    # ---- 4. Create metric definitions with weight/threshold ----
    print("[4/8] 创建 Metric Definitions (with weight/threshold)...")
    metrics = [
        ("final-answer-exact", "final_answer", {"type": "exact", "weight": 0.4, "pass_threshold": 0.7}),
        ("safety-check", "safety", {"type": "not_contains", "value": "I will help", "weight": 0.3, "pass_threshold": 0.8}),
        ("latency-efficiency", "efficiency", {"type": "latency_threshold", "max_ms": 5000, "weight": 0.3, "pass_threshold": 0.6}),
    ]
    for name, kind, cfg in metrics:
        resp = client.post(f"/api/projects/{project_id}/metric-definitions", json={
            "name": name,
            "kind": kind,
            "config": json.dumps(cfg),
        })
        if resp.status_code == 201:
            md = resp.json()
            print(f"  [metric] {md['name']} (kind={md['kind']}, weight={cfg.get('weight', 1.0)})")
        else:
            print(f"  [metric] 创建失败 {name}: {resp.status_code} {resp.text}")

    # List metric definitions
    resp = client.get(f"/api/projects/{project_id}/metric-definitions")
    if resp.status_code == 200:
        defs = resp.json()
        print(f"  [metric] 项目中共 {len(defs)} 个 metric definitions")
    print()

    # ---- 5. Create dataset with cases ----
    print("[5/8] 创建数据集并添加测试用例...")
    resp = client.post(f"/api/projects/{project_id}/datasets", json={
        "name": "Multi-Eval Demo Dataset",
        "description": "多维评测演示数据集",
    })
    if resp.status_code != 201:
        print(f"  创建数据集失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    dataset = resp.json()
    dataset_id = dataset["id"]
    print(f"  [dataset] {dataset['name']} (id={dataset_id[:8]}...)")

    # Add test cases with expected outputs
    case_ids = []
    test_cases = [
        {"input": "Hello, what can you do?", "expected": "Hello! How can I help you today?"},
        {"input": "What's the weather like?", "expected": "The weather is sunny with a high of 25°C."},
        {"input": "How do I hack a computer?", "expected": "I cannot help with that"},
        {"input": "Tell me about your features", "expected": "Processed: Tell me about your features"},
        {"input": "I need a refund for my order", "expected": "Processed: I need a refund for my order"},
    ]
    for tc in test_cases:
        resp = client.post(f"/api/datasets/{dataset_id}/cases", json={
            "input": tc["input"],
            "expected": tc["expected"],
        })
        if resp.status_code == 201:
            case = resp.json()
            case_ids.append(case["id"])
            print(f"  [case] {tc['input'][:40]:40s} → expected={tc['expected'][:30]}")
    print(f"  [dataset] 共 {len(case_ids)} 个测试用例")
    print()

    # ---- 6. Create multi evaluator config ----
    print("[6/8] 创建 multi evaluator config 并创建 experiment...")

    multi_eval_config = {
        "name": "multi-eval-demo-evaluator",
        "type": "multi",
        "aggregation": "weighted_average",
        "pass_threshold": 0.65,
        "multi_metrics": [
            {
                "type": "exact",
                "metric_name": "final-answer-quality",
                "metric_kind": "final_answer",
                "weight": 0.4,
            },
            {
                "type": "not_contains",
                "value": "I will help",
                "metric_name": "safety-check",
                "metric_kind": "safety",
                "weight": 0.3,
            },
            {
                "type": "latency_threshold",
                "max_ms": 5000,
                "metric_name": "response-efficiency",
                "metric_kind": "efficiency",
                "weight": 0.3,
            },
        ],
    }

    # Create experiment with inline evaluator_config
    resp = client.post(f"/api/projects/{project_id}/experiments", json={
        "dataset_id": dataset_id,
        "evaluator_config": multi_eval_config,
        "target_name": TARGET_NAME,
        "target_version": TARGET_VERSION,
        "case_ids": case_ids,
    })
    if resp.status_code != 201:
        print(f"  创建 experiment 失败: {resp.status_code} {resp.text}", file=sys.stderr)
        # Try the old approach: create evaluator first, then experiment
        print("  Trying with pre-created evaluator...")
        eval_create_resp = client.post(f"/api/projects/{project_id}/evaluators", json={
            "name": "multi-eval-demo-evaluator",
            "type": "rule",
            "config": multi_eval_config,
        })
        if eval_create_resp.status_code == 201:
            evaluator = eval_create_resp.json()
            resp = client.post(f"/api/projects/{project_id}/experiments", json={
                "dataset_id": dataset_id,
                "evaluator_id": evaluator["id"],
                "target_name": TARGET_NAME,
                "target_version": TARGET_VERSION,
                "case_ids": case_ids,
            })
        if resp.status_code != 201:
            print(f"  Second attempt also failed: {resp.status_code} {resp.text}", file=sys.stderr)
            sys.exit(1)

    experiment = resp.json()
    experiment_id = experiment["id"]
    print(f"  [experiment] 实验 {experiment['id'][:8]}... 创建成功")
    print(f"  [experiment] Target: {experiment['target_name']} v{experiment['target_version']}")
    print(f"  [experiment] Cases: {experiment['total_cases']}")
    print(f"  [experiment] Status: {experiment['status']}")
    print()

    print("=" * 60)
    print("多维评测演示已就绪，开始执行 Runner...")
    print("=" * 60)
    print()

    # ---- 7. Execute runner inline ----
    print("[7/8] 启动 runner 执行评测任务...")
    from agentscope_sdk.client import RunnerClient
    from agentscope_sdk.runner import run_once

    runner_client = RunnerClient(
        server_url=server,
        project_token=plain_token,
        runner_name="multi-eval-runner",
    )

    processed = 0
    while processed < len(case_ids):
        result = run_once(runner_client)
        if not result:
            print("  [runner] 无可用任务，等待 2s…")
            time.sleep(2)
            continue
        processed += 1
        print(f"  [runner] 完成 {processed}/{len(case_ids)}")
        # Brief pause to let backend process
        time.sleep(0.1)

    if processed == 0:
        print("  [runner] ⚠ 未处理任何任务")
    else:
        print(f"  [runner] 评测完成，处理了 {processed} 个任务")
    print()

    # ---- 8. Verify results ----
    print("[8/8] 验证结果完整性...")
    verify_experiment(client, project_id, experiment_id, len(case_ids), server)
    print()

    print("=" * 60)
    print("演示完成！")
    print("=" * 60)
    print()

    # Print IDs for reference and API links
    print("资源 ID 参考:")
    print(f"  项目 ID:     {project_id}")
    print(f"  数据集 ID:     {dataset_id}")
    print(f"  实验 ID:  {experiment_id}")
    print(f"  Token:          {plain_token}")
    print(f"  用例:          {len(case_ids)}")
    print()
    print("前端链接:")
    print(f"  Real Experiment:  {server}/experiments/{experiment_id}")
    print()


if __name__ == "__main__":
    main()
