#!/usr/bin/env python3
r"""端到端评测演示脚本。

该脚本模拟一个完整的评测流程：
1. 注册/登录用户
2. 创建项目
3. 创建 Project Token（用于 runner API）
4. 创建数据集并添加测试用例
5. 创建实验
6. 使用 Python runner 签出并执行测试用例
7. 提交评测结果
8. 输出可通过 UI 访问的资源 ID

使用方法：
  # 1. 先确保后端在运行: make dev
  # 2. 运行此脚本:
  python python/examples/demo_eval.py [--server http://127.0.0.1:8080]

不依赖真实 LLM key，使用本地 mock target 和 rule evaluator。
"""

from __future__ import annotations

import argparse
import os
import sys
import time
import uuid
from typing import Any

import httpx

# ---- Configuration ----

BASE_URL = "http://127.0.0.1:8080"
DEMO_EMAIL = f"demo-{uuid.uuid4().hex[:8]}@example.com"
DEMO_PASSWORD = "demo-password-123"
DEMO_DISPLAY_NAME = "Demo User"


# ---- HTTP helpers ----

def session_client(base_url: str) -> httpx.Client:
    """Create an httpx client with cookie persistence."""
    return httpx.Client(base_url=base_url, follow_redirects=True)


def register_or_login(client: httpx.Client) -> str:
    """Try to register a new demo user; fall back to login if already exists.

    Returns user_id.
    """
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
        print(f"  [user] Registered: {data['email']} (id={data['id']})")
        return data["id"]

    if resp.status_code == 409:
        # Already exists — login instead
        resp = client.post(
            "/api/auth/login",
            json={"email": DEMO_EMAIL, "password": DEMO_PASSWORD},
        )
        if resp.status_code == 200:
            data = resp.json()
            print(f"  [user] Logged in: {data['email']} (id={data['id']})")
            return data["id"]
        print(f"  [user] Login failed: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)

    print(f"  [user] Register failed: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def create_project(client: httpx.Client, name: str) -> dict[str, Any]:
    """Create a project. Returns project dict."""
    resp = client.post("/api/projects", json={"name": name, "description": "Demo project"})
    if resp.status_code == 201:
        proj = resp.json()
        print(f"  [project] Created: {proj['name']} (id={proj['id']})")
        return proj
    print(f"  [project] Failed: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def create_project_token(client: httpx.Client, project_id: str, desc: str) -> str:
    """Create a project token. Returns the plain token string."""
    resp = client.post(
        f"/api/projects/{project_id}/tokens",
        json={"description": desc, "scopes": ["otlp:write", "runner:write"]},
    )
    if resp.status_code == 201:
        data = resp.json()
        plain = data["plain_token"]
        print(f"  [token] Created: prefix={data['token']['token_prefix']}")
        return plain
    print(f"  [token] Failed: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def create_dataset(client: httpx.Client, project_id: str, name: str) -> dict[str, Any]:
    """Create a dataset. Returns dataset dict."""
    resp = client.post(
        f"/api/projects/{project_id}/datasets",
        json={"name": name, "description": "Demo dataset for evaluation"},
    )
    if resp.status_code == 201:
        ds = resp.json()
        print(f"  [dataset] Created: {ds['name']} (id={ds['id']})")
        return ds
    print(f"  [dataset] Failed: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def add_case(client: httpx.Client, dataset_id: str, input_data: str, expected: str) -> dict[str, Any]:
    """Add a test case to a dataset. Returns case dict."""
    resp = client.post(
        f"/api/datasets/{dataset_id}/cases",
        json={"input": input_data, "expected": expected},
    )
    if resp.status_code == 201:
        case = resp.json()
        print(f"  [case] Added: input={input_data[:40]}... (id={case['id']})")
        return case
    print(f"  [case] Failed: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def create_experiment(
    client: httpx.Client,
    project_id: str,
    dataset_id: str,
    target_name: str,
    case_ids: list[str],
) -> dict[str, Any]:
    """Create an experiment. Returns experiment dict."""
    resp = client.post(
        f"/api/projects/{project_id}/experiments",
        json={
            "dataset_id": dataset_id,
            "target_name": target_name,
            "case_ids": case_ids,
        },
    )
    if resp.status_code == 201:
        exp = resp.json()
        print(f"  [experiment] Created: target={exp['target_name']} (id={exp['id']})")
        return exp
    print(f"  [experiment] Failed: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def list_experiments(client: httpx.Client, project_id: str) -> list[dict[str, Any]]:
    """List experiments for a project."""
    resp = client.get(f"/api/projects/{project_id}/experiments")
    if resp.status_code == 200:
        return resp.json()
    return []


def main() -> None:
    parser = argparse.ArgumentParser(description="AgentScope 端到端评测演示")
    parser.add_argument("--server", default=BASE_URL, help=f"后端地址 (默认: {BASE_URL})")
    parser.add_argument("--runner-name", default="demo-runner", help="Runner 名称")
    args = parser.parse_args()

    base_url = args.server
    runner_name = args.runner_name

    # Compute frontend URL: replace backend port 8080 with 5173
    fe_url = base_url.replace(":8080", ":5173")

    print("=" * 60)
    print("AgentScope 端到端演示脚本")
    print("=" * 60)
    print()

    # ---- Step 1: Auth ----
    print("[1/7] 用户注册/登录")
    client = session_client(base_url)
    user_id = register_or_login(client)

    # ---- Step 2: Create project ----
    print("[2/7] 创建项目")
    project_name = f"demo-project-{uuid.uuid4().hex[:6]}"
    project = create_project(client, project_name)
    project_id = project["id"]

    # ---- Step 3: Create project token ----
    print("[3/7] 创建 Project Token")
    plain_token = create_project_token(client, project_id, "demo-runner")

    # ---- Step 4: Create dataset & cases ----
    print("[4/7] 创建数据集和测试用例")
    dataset_name = f"demo-dataset-{uuid.uuid4().hex[:6]}"
    dataset = create_dataset(client, project_id, dataset_name)
    dataset_id = dataset["id"]

    # Add test cases with simple Q&A format
    cases_data = [
        ("What is the capital of France?", "Paris"),
        ("What is 2+2?", "4"),
        ("What is the color of the sky?", "blue"),
        ("Who wrote Romeo and Juliet?", "Shakespeare"),
    ]
    case_ids: list[str] = []
    for inp, exp in cases_data:
        case = add_case(client, dataset_id, inp, exp)
        case_ids.append(case["id"])

    print(f"  [cases] Total: {len(case_ids)} cases")

    # ---- Step 5: Create experiment ----
    print("[5/7] 创建实验")
    target_name = "demo-target"
    experiment = create_experiment(client, project_id, dataset_id, target_name, case_ids)
    experiment_id = experiment["id"]

    # ---- Step 6: Run evaluation via Python SDK ----
    print("[6/7] 执行评测")

    # Register mock target with Python SDK
    sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
    from agentscope_sdk.target import target

    @target(name=target_name, version="1.0.0")
    def demo_target(case_input: str, context: dict[str, Any]) -> str:
        """Mock target that returns a canned response based on input."""
        answers = {
            "What is the capital of France?": "Paris",
            "What is 2+2?": "4",
            "What is the color of the sky?": "blue",
            "Who wrote Romeo and Juliet?": "Shakespeare",
            "default": "I don't know.",
        }
        return answers.get(case_input.strip(), answers["default"])

    # Run the runner loop
    from agentscope_sdk.client import RunnerClient
    from agentscope_sdk.runner import run_once

    runner_client = RunnerClient(
        server_url=base_url,
        project_token=plain_token,
        runner_name=runner_name,
    )

    print("  [runner] Starting evaluation...")
    processed = 0
    while True:
        result = run_once(runner_client)
        if not result:
            if processed == 0:
                print("  [runner] No jobs available, waiting...")
                time.sleep(2)
                continue
            break
        processed += 1
        print(f"  [runner] Processed job {processed}/{len(case_ids)}")
        if processed >= len(case_ids):
            break

    if processed == 0:
        print("  [runner] Warning: No jobs were processed. Check server logs.")
    else:
        print(f"  [runner] Evaluation complete. {processed} jobs processed.")

    # ---- Step 7: Show results ----
    print("[7/7] 查看结果")
    print()

    # List experiments to see status
    exps = list_experiments(client, project_id)
    our_exp = next((e for e in exps if e["id"] == experiment_id), experiment)
    print(f"  实验状态: {our_exp['status']} (通过: {our_exp['passed_cases']}/{our_exp['total_cases']})")

    # Fetch results
    resp = client.get(f"/api/projects/{project_id}/experiments/{experiment_id}/results")
    if resp.status_code == 200:
        results = resp.json()
        print(f"  实验结果数量: {len(results)}")
        for r in results:
            status_icon = "✓" if r["passed"] else "✗"
            print(f"    {status_icon} score={r['score']:.2f} reason={r['reason'][:60]}")
    else:
        print(f"  (无法获取结果: {resp.status_code})")

    print()
    print("=" * 60)
    print("演示完成！以下是可通过浏览器访问的资源：")
    print("=" * 60)
    print()
    print(f"  前端地址:     {fe_url}")
    print(f"  项目:         {fe_url}/projects/{project_id}")
    print(f"  实验详情:     {fe_url}/experiments/{experiment_id}")
    print(f"  API 项目:     {base_url}/api/projects/{project_id}")
    print(f"  API 数据集:   {base_url}/api/projects/{project_id}/datasets")
    print(f"  API 实验:     {base_url}/api/projects/{project_id}/experiments")
    print(f"  API 结果:     {base_url}/api/projects/{project_id}/experiments/{experiment_id}/results")
    print()
    print("  Token (用于 runner):")
    print(f"    {plain_token}")
    print()
    print(f"  提示：打开 {fe_url}，使用 demo 邮箱和密码登录后可以看到项目下的结果。")


if __name__ == "__main__":
    main()
