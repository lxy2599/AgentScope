#!/usr/bin/env python3
r"""真实 LLM Agent 端到端评测演示。

该脚本使用真实 LLM（DeepSeek / GLM）执行评测任务，并通过
OpenTelemetry 把 trace 发送到后端 ClickHouse。

流程：
  1. 注册/登录用户
  2. 创建项目
  3. 创建 Project Token（用于 OTLP ingestion + runner API）
  4. 创建数据集并添加 4 个简单问答用例
  5. 创建实验（使用 exact 规则评测器）
  6. 使用 Python runner 运行真实 Agent 评测
  7. Agent 调用 DeepSeek / GLM 并自动采集 OTLP trace
  8. 提交评测结果（带 run_id → 可跳转 Run Detail）
  9. 输出资源链接

使用方法：
  make demo-real-agent

环境变量：
  AGENTSCOPE_LLM_PROVIDER    Provider 名称（deepseek / glm）
  AGENTSCOPE_LLM_BASE_URL    API 基础 URL
  AGENTSCOPE_LLM_API_KEY     API 密钥
  AGENTSCOPE_LLM_MODEL       模型名称

  缺失任一变量会直接报错退出，不 fallback mock。
"""

from __future__ import annotations

import argparse
import os
import sys
import time
import uuid

import httpx

from opentelemetry import trace as otel_trace

# ---- Configuration ----

BASE_URL = "http://127.0.0.1:8080"
DEMO_EMAIL = f"demo-{uuid.uuid4().hex[:8]}@example.com"
DEMO_PASSWORD = "demo-password-123"
DEMO_DISPLAY_NAME = "Demo User"


# ---- HTTP helpers ----


def session_client(base_url: str) -> httpx.Client:
    return httpx.Client(base_url=base_url, follow_redirects=True)


def register_or_login(client: httpx.Client) -> str:
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
        if resp.status_code == 200:
            data = resp.json()
            print(f"  [user] 登录成功: {data['email']} (id={data['id']})")
            return data["id"]
        print(f"  [user] 登录失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)

    print(f"  [user] 注册失败: {resp.status_code} {resp.text}", file=sys.stderr)
    sys.exit(1)


def create_project(client: httpx.Client, name: str) -> dict:
    resp = client.post("/api/projects", json={"name": name, "description": "Demo project for real agent"})
    if resp.status_code != 201:
        print(f"  [project] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    proj = resp.json()
    print(f"  [project] 创建成功: {proj['name']} (id={proj['id']})")
    return proj


def create_project_token(client: httpx.Client, project_id: str, desc: str) -> str:
    resp = client.post(
        f"/api/projects/{project_id}/tokens",
        json={"description": desc, "scopes": ["otlp:write", "runner:write"]},
    )
    if resp.status_code != 201:
        print(f"  [token] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    data = resp.json()
    print(f"  [token] 创建成功: prefix={data['token']['token_prefix']}")
    return data["plain_token"]


def create_dataset(client: httpx.Client, project_id: str, name: str) -> dict:
    resp = client.post(
        f"/api/projects/{project_id}/datasets",
        json={"name": name, "description": "Demo dataset for real agent evaluation"},
    )
    if resp.status_code != 201:
        print(f"  [dataset] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    ds = resp.json()
    print(f"  [dataset] 创建成功: {ds['name']} (id={ds['id']})")
    return ds


def add_case(client: httpx.Client, dataset_id: str, input_data: str, expected: str) -> dict:
    resp = client.post(
        f"/api/datasets/{dataset_id}/cases",
        json={"input": input_data, "expected": expected},
    )
    if resp.status_code != 201:
        print(f"  [case] 添加失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    case = resp.json()
    print(f"  [case] 添加成功: input={input_data[:40]}... (id={case['id']})")
    return case


def create_experiment(
    client: httpx.Client,
    project_id: str,
    dataset_id: str,
    target_name: str,
    case_ids: list[str],
) -> dict:
    resp = client.post(
        f"/api/projects/{project_id}/experiments",
        json={
            "dataset_id": dataset_id,
            "target_name": target_name,
            "case_ids": case_ids,
        },
    )
    if resp.status_code != 201:
        print(f"  [experiment] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    exp = resp.json()
    print(f"  [experiment] 创建成功: target={exp['target_name']} (id={exp['id']})")
    return exp


def list_experiments(client: httpx.Client, project_id: str) -> list[dict]:
    resp = client.get(f"/api/projects/{project_id}/experiments")
    if resp.status_code == 200:
        return resp.json()
    return []


# ---- Main ----


def main() -> None:
    parser = argparse.ArgumentParser(description="AgentScope 真实 LLM Agent 评测演示")
    parser.add_argument("--server", default=BASE_URL, help=f"后端地址 (默认: {BASE_URL})")
    parser.add_argument("--runner-name", default="real-agent-runner", help="Runner 名称")
    args = parser.parse_args()

    base_url = args.server
    runner_name = args.runner_name
    fe_url = base_url.replace(":8080", ":5173")

    print("=" * 60)
    print("AgentScope 真实 LLM Agent 演示")
    print("=" * 60)
    print()

    # ---- Validate LLM configuration early ----
    print("[验证] LLM 配置")
    from agentscope_sdk.llm import LLMClient, LLMConfigError

    try:
        LLMClient()
        print("  [llm] 配置验证通过")
    except LLMConfigError as e:
        print(f"  [llm] 配置缺失，请设置以下环境变量:\n\n{e}", file=sys.stderr)
        sys.exit(1)
    print()

    # ---- Step 1: Auth ----
    print("[1/8] 用户注册/登录")
    client = session_client(base_url)
    user_id = register_or_login(client)

    # ---- Step 2: Create project ----
    print("[2/8] 创建项目")
    project_name = f"real-agent-demo-{uuid.uuid4().hex[:6]}"
    project = create_project(client, project_name)
    project_id = project["id"]

    # ---- Step 3: Create project token ----
    print("[3/8] 创建 Project Token")
    plain_token = create_project_token(client, project_id, "real-agent-runner")

    # ---- Step 4: Create dataset & cases ----
    print("[4/8] 创建数据集和测试用例")
    dataset_name = f"real-agent-dataset-{uuid.uuid4().hex[:6]}"
    dataset = create_dataset(client, project_id, dataset_name)
    dataset_id = dataset["id"]

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
    print(f"  [cases] 共 {len(case_ids)} 个用例")

    # ---- Step 5: Create experiment ----
    print("[5/8] 创建实验")
    target_name = "real-agent"
    experiment = create_experiment(client, project_id, dataset_id, target_name, case_ids)
    experiment_id = experiment["id"]

    # ---- Step 6: Register real LLM Agent target ----
    print("[6/8] 注册真实 Agent")
    sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

    from agentscope_sdk.target import target

    # Get a tracer for creating child spans inside the target
    demo_tracer = otel_trace.get_tracer("agentscope-demo-agent")

    @target(name=target_name, version="1.0.0")
    def real_agent(case_input: str, context: dict) -> str:
        """Real LLM Agent: calls DeepSeek/GLM via OpenAI-compatible API."""
        from agentscope_sdk.llm import LLMClient, LLMResponseError

        # 1. Prepare input — create child span
        with demo_tracer.start_as_current_span("agent.prepare_input") as span:
            messages = [
                {
                    "role": "system",
                    "content": (
                        "You are a concise Q&A agent. Answer each question with "
                        "the shortest possible correct answer. Do NOT add explanations. "
                        "Respond only with the answer itself."
                    ),
                },
                {"role": "user", "content": case_input},
            ]
            span.set_attribute("agentscope.run.id", context.get("run_id", ""))
            span.set_attribute("question", case_input[:200])

        # 2. Call LLM
        llm = LLMClient()
        with demo_tracer.start_as_current_span("llm.generate_response") as span:
            span.set_attribute("agentscope.run.id", context.get("run_id", ""))
            span.set_attribute("llm.provider", llm.provider)
            span.set_attribute("llm.model", llm.model)
            span.set_attribute("llm.base_url", llm.base_url)
            span.set_attribute("llm.prompt_preview", case_input[:200])

            # Build extra kwargs per-provider
            extra_kwargs: dict[str, Any] = {"temperature": 0.0}
            if llm.provider == "glm":
                # GLM-4.7-Flash supports thinking mode and large max_tokens
                extra_kwargs["thinking"] = {"type": "enabled"}
                extra_kwargs["max_tokens"] = 65536

            try:
                response = llm.generate_response(messages, **extra_kwargs)
            except LLMResponseError as e:
                span.set_status(otel_trace.Status(otel_trace.StatusCode.ERROR, str(e)))
                raise

            span.set_attribute("llm.output_preview", response.content[:500])
            span.set_attribute("llm.latency_ms", response.latency_ms)
            span.set_attribute("llm.prompt_tokens", response.prompt_tokens)
            span.set_attribute("llm.completion_tokens", response.completion_tokens)
            span.set_attribute("llm.total_tokens", response.total_tokens)

        # 3. Evaluate (span created by evaluator in runner)
        with demo_tracer.start_as_current_span("evaluator.compare_answer") as span:
            expected = context.get("expected", "")
            passed = response.content.strip().lower() == expected.strip().lower()
            span.set_attribute("agentscope.run.id", context.get("run_id", ""))
            span.set_attribute("expected", expected)
            span.set_attribute("actual", response.content.strip())
            span.set_attribute("passed", str(passed).lower())

        # 4. Finish
        with demo_tracer.start_as_current_span("agent.finish") as span:
            span.set_attribute("agentscope.run.id", context.get("run_id", ""))
            span.set_attribute("output_preview", response.content.strip()[:500])
            span.set_attribute("success", "true")

        return response.content.strip()

    # ---- Step 7: Run evaluation ----
    print("[7/8] 执行评测（真实 LLM 调用中…）")
    from agentscope_sdk.client import RunnerClient
    from agentscope_sdk.runner import run_once

    runner_client = RunnerClient(
        server_url=base_url,
        project_token=plain_token,
        runner_name=runner_name,
    )

    print("  [runner] 等待评测任务…")
    processed = 0
    while True:
        result = run_once(runner_client)
        if not result:
            if processed == 0:
                print("  [runner] 无可用任务，等待 2s…")
                time.sleep(2)
                continue
            break
        processed += 1
        print(f"  [runner] 完成 {processed}/{len(case_ids)}")
        if processed >= len(case_ids):
            break

    if processed == 0:
        print("  [runner] ⚠ 未处理任何任务")
    else:
        print(f"  [runner] 评测完成，处理了 {processed} 个任务")

    # ---- Step 8: Show results ----
    print("[8/8] 查看结果")
    print()

    exps = list_experiments(client, project_id)
    our_exp = next((e for e in exps if e["id"] == experiment_id), experiment)
    print(f"  实验状态: {our_exp['status']} (通过: {our_exp['passed_cases']}/{our_exp['total_cases']})")

    resp = client.get(f"/api/projects/{project_id}/experiments/{experiment_id}/results")
    if resp.status_code == 200:
        results = resp.json()
        print(f"  实验结果数量: {len(results)}")
        for r in results:
            status_icon = "✓" if r["passed"] else "✗"
            run_link = f" → {fe_url}/runs/{r['run_id']}" if r.get("run_id") else " (暂无 Trace)"
            print(f"    {status_icon} score={r['score']:.2f} {r['reason'][:60]}{run_link}")
    else:
        print(f"  (无法获取结果: {resp.status_code})")

    print()
    print("=" * 60)
    print("演示完成！以下是可访问的资源：")
    print("=" * 60)
    print()
    print(f"  登录邮箱:     {DEMO_EMAIL}")
    print(f"  登录密码:     {DEMO_PASSWORD}")
    print(f"  前端地址:     {fe_url}")
    print(f"  项目:         {fe_url}/projects/{project_id}")
    print(f"  实验详情:     {fe_url}/experiments/{experiment_id}")
    print(f"  API 项目:     {base_url}/api/projects/{project_id}")
    print(f"  API 实验结果: {base_url}/api/projects/{project_id}/experiments/{experiment_id}/results")
    print()
    print("  提示：在实验详情页点击各结果的 Trace 按钮可查看 span tree。")
    print()


if __name__ == "__main__":
    main()
