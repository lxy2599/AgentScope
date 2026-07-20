#!/usr/bin/env python3
r"""通用 Agent 深度可观测演示。

该脚本演示任意 Agent 如何通过 SDK 语义埋点实现在平台中的完整可观测。
与 real_agent_demo 不同，本演示使用 deterministic/mock 模式运行，
不要求真实 LLM API key，但也可配置真实 LLM。

产生的语义轨迹：
  - thought:  Agent 思考过程
  - llm:     LLM 调用（mock 或真实）
  - tool:    工具调用
  - retrieval: 知识库检索
  - skill:   Skill 调用
  - final_answer: 最终答案

使用方法：
  make demo-generic-observability-agent

或直接运行：
  python examples/generic_observability_agent.py --server http://127.0.0.1:8080
"""

from __future__ import annotations

import argparse
import json
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

# Case data: input, expected, mock_answer, mock_thought, mock_stage, mock_confidence
CASES = [
    {
        "input": "Who wrote the book '1984'?",
        "expected": "George Orwell",
        "mock_answer": "George Orwell",
        "mock_thought": "The user is asking about the author of the book '1984'. I recall this is a classic dystopian novel by George Orwell.",
        "mock_stage": "fact_retrieval",
        "mock_confidence": 0.98,
    },
    {
        "input": "What is the chemical symbol for gold?",
        "expected": "Au",
        "mock_answer": "Au",
        "mock_thought": "The user wants to know the chemical symbol of gold. In the periodic table, gold is element 79 with symbol Au from the Latin 'aurum'.",
        "mock_stage": "knowledge_lookup",
        "mock_confidence": 0.95,
    },
]


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
    resp = client.post("/api/projects", json={"name": name, "description": "Demo project for generic observability agent"})
    if resp.status_code != 201:
        print(f"  [project] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    proj = resp.json()
    print(f"  [project] 创建成功: {proj['name']} (id={proj['id']})")
    return proj


def create_project_token(client: httpx.Client, project_id: str, desc: str) -> str:
    resp = client.post(
        f"/api/projects/{project_id}/tokens",
        json={"description": desc, "scopes": ["otlp:write", "runner:write", "agent:register", "agent:runtime"]},
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
        json={"name": name, "description": "Dataset for generic observability agent evaluation"},
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


# ---- Mock LLM Provider ----

MOCK_KNOWLEDGE_BASE = {
    "Who wrote the book '1984'?": {
        "documents": [
            {"id": "doc-1", "title": "1984 Summary", "content": "1984 is a dystopian novel by George Orwell, published in 1949.", "score": 0.98},
            {"id": "doc-2", "title": "George Orwell Biography", "content": "Eric Arthur Blair, known as George Orwell, was an English novelist.", "score": 0.85},
        ],
        "answer": "George Orwell",
    },
    "What is the chemical symbol for gold?": {
        "documents": [
            {"id": "doc-3", "title": "Periodic Table", "content": "Gold (Au) is element 79 on the periodic table.", "score": 0.99},
            {"id": "doc-4", "title": "Gold Chemistry", "content": "The chemical symbol for gold is Au, from the Latin word 'aurum'.", "score": 0.97},
        ],
        "answer": "Au",
    },
}


def mock_llm_response(case_input: str) -> str:
    """Simulate an LLM response."""
    if case_input in MOCK_KNOWLEDGE_BASE:
        return MOCK_KNOWLEDGE_BASE[case_input]["answer"]
    return "Unknown"


def mock_search_knowledge(case_input: str) -> list[dict]:
    """Simulate knowledge base search."""
    result = MOCK_KNOWLEDGE_BASE.get(case_input, {"documents": []})
    return result["documents"]


def mock_assess_confidence(case_input: str) -> float:
    """Simulate confidence assessment."""
    if case_input in MOCK_KNOWLEDGE_BASE:
        doc_scores = [d["score"] for d in MOCK_KNOWLEDGE_BASE[case_input]["documents"]]
        return max(doc_scores) if doc_scores else 0.7
    return 0.5


# ---- Main ----


def main() -> None:
    parser = argparse.ArgumentParser(description="AgentScope 通用 Agent 可观测演示")
    parser.add_argument("--server", default=BASE_URL, help=f"后端地址 (默认: {BASE_URL})")
    parser.add_argument("--runner-name", default="generic-obsv-runner", help="Runner 名称")
    args = parser.parse_args()

    base_url = args.server
    runner_name = args.runner_name
    fe_url = base_url.replace(":8080", ":5173")

    print("=" * 60)
    print("AgentScope 通用 Agent 深度可观测演示")
    print("=" * 60)
    print()

    # ---- Check if real LLM config is available ----
    use_real_llm = all([
        os.environ.get("AGENTSCOPE_LLM_API_KEY"),
        os.environ.get("AGENTSCOPE_LLM_PROVIDER"),
    ])
    if use_real_llm:
        print("[模式] 使用真实 LLM")
        from agentscope_sdk.llm import LLMClient, LLMConfigError
        try:
            llm_client = LLMClient()
            print(f"  [llm] 配置: provider={llm_client.provider} model={llm_client.model}")
        except LLMConfigError as e:
            print(f"  [llm] 配置无效: {e}")
            use_real_llm = False
    if not use_real_llm:
        print("[模式] 使用模拟 LLM（无需 API key）")
        print("  [提示] 设置 AGENTSCOPE_LLM_API_KEY + AGENTSCOPE_LLM_PROVIDER 可用真实 LLM")
    print()

    # ---- Step 1: Auth ----
    print("[1/8] 用户注册/登录")
    client = session_client(base_url)
    user_id = register_or_login(client)
    print()

    # ---- Step 2: Create project ----
    print("[2/8] 创建项目")
    project_name = f"generic-obsv-demo-{uuid.uuid4().hex[:6]}"
    project = create_project(client, project_name)
    project_id = project["id"]
    print()

    # ---- Step 3: Define target name ----
    print("[3/8] 定义目标 Agent 名称")
    target_name = "generic-research-agent"
    print(f"  [target] 目标 Agent: {target_name}")
    print()

    # ---- Step 4: Create project token ----
    print("[4/8] 创建 Project Token")
    plain_token = create_project_token(client, project_id, "generic-obsv-runner")
    print()

    # ---- Step 5: Initialize tracing and agent runtime ----
    print("[5/8] 初始化 SDK（tracing + agent registration）")

    # Configure tracing
    from agentscope_sdk.tracing import configure_tracing, force_flush

    tracer = configure_tracing(
        server_url=base_url,
        project_token=plain_token,
        service_name="generic-observability-agent",
        capture_content=True,
    )
    print("  [tracing] OTLP 导出已配置")

    # Register agent
    from agentscope_sdk.runtime import init as agent_init, shutdown_runtime

    _rt_instance = agent_init(
        server_url=base_url,
        project_token=plain_token,
        agent_name=target_name,
        description="Generic research agent demonstrating full observability",
        metadata={"demo": "generic-observability", "framework": "generic"},
        heartbeat_interval=30,
    )
    agent_id = _rt_instance.agent_id
    print(f"  [agent] 注册成功: id={agent_id}")
    print()

    # ---- Step 6: Create dataset & cases ----
    print("[6/8] 创建数据集和测试用例")
    dataset_name = f"generic-obsv-dataset-{uuid.uuid4().hex[:6]}"
    dataset = create_dataset(client, project_id, dataset_name)
    dataset_id = dataset["id"]

    case_ids: list[str] = []
    for case_data in CASES:
        case = add_case(client, dataset_id, case_data["input"], case_data["expected"])
        case_ids.append(case["id"])
    print(f"  [cases] 共 {len(case_ids)} 个用例")
    print()

    # ---- Step 7: Register the generic agent target ----
    print("[7/8] 注册通用 Agent 目标函数")
    sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

    from agentscope_sdk.target import target
    from agentscope_sdk.tracing import (
        record_final_answer,
        trace_llm,
        trace_retrieval,
        trace_skill,
        trace_thought,
        trace_tool,
    )

    @target(name=target_name, version="1.0.0")
    def generic_agent(case_input: str, context: dict) -> str:
        """Generic agent: demonstrates all six semantic span types."""
        from agentscope_sdk import runtime as ascope_runtime

        run = ascope_runtime.start_run(input=case_input[:200], metadata={"case_input": case_input})

        try:
            # ---- 1. Thought: agent reasoning ----
            case_data = next((c for c in CASES if c["input"] == case_input), None)
            if case_data:
                thought_summary = case_data["mock_thought"][:200]
                thought_stage = case_data["mock_stage"]
                thought_confidence = case_data["mock_confidence"]
            else:
                thought_summary = "Processing query..."
                thought_stage = "analysis"
                thought_confidence = 0.5

            with trace_thought(
                tracer, "agent.think",
                summary=thought_summary,
                capture_content=True,
                content=case_data["mock_thought"] if case_data else "Analyzing input...",
                stage=thought_stage,
                confidence=thought_confidence,
            ):
                pass  # thought is captured via context manager

            # ---- 2. LLM Call (mock or real) ----
            ascope_runtime.update_run_status("waiting_llm", message="calling LLM")

            if use_real_llm:
                from agentscope_sdk.llm import LLMClient as RealLLMClient

                llm = RealLLMClient()
                messages = [
                    {"role": "system", "content": "You are a precise Q&A agent. Answer concisely."},
                    {"role": "user", "content": case_input},
                ]
                with trace_llm(
                    tracer, "llm.call",
                    provider=llm.provider,
                    model=llm.model,
                    input_summary=case_input[:200],
                    input_tokens=50,
                    output_tokens=10,
                    cost=0.001,
                    capture_content=True,
                ):
                    response_text = llm.generate_response(messages).content.strip()
            else:
                # Mock LLM
                from agentscope_sdk.llm import LLMProvider

                llm_provider = "mock"
                llm_model = "mock-v1"

                with trace_llm(
                    tracer, "llm.call",
                    provider=llm_provider,
                    model=llm_model,
                    input_summary=case_input[:200],
                    input_tokens=45,
                    output_tokens=5,
                    cost=0.0,
                    capture_content=True,
                ):
                    response_text = mock_llm_response(case_input)

            # ---- 3. Tool: knowledge base search ----
            ascope_runtime.update_run_status("waiting_tool", message="searching knowledge base")
            docs = mock_search_knowledge(case_input)
            doc_ids = [d["id"] for d in docs]
            doc_scores = [d["score"] for d in docs]

            with trace_tool(
                tracer, "search_knowledge_base",
                tool_type="api",
                arguments_summary=json.dumps({"query": case_input[:50]}, ensure_ascii=False),
                result_summary=f"Found {len(docs)} documents",
                success=True,
            ):
                pass

            # ---- 4. Retrieval: knowledge base query ----
            with trace_retrieval(
                tracer, "retrieve_knowledge",
                knowledge_base="general-knowledge",
                query_text=case_input[:200],
                top_k=3,
                returned_count=len(docs),
                document_ids=doc_ids,
                scores=doc_scores,
            ):
                pass

            # ---- 5. Skill: Answer composition ----
            with trace_skill(
                tracer, "compose_answer",
                skill_version="1.0.0",
                input_summary=case_input[:200],
                output_summary=response_text[:200],
                success=True,
            ):
                pass

            # ---- 6. Final Answer ----
            with record_final_answer(
                tracer, "agent.answer",
                summary=response_text[:200],
                output=response_text,
                structured_output=json.dumps({"answer": response_text, "confidence": mock_assess_confidence(case_input)}, ensure_ascii=False),
            ):
                pass

            # ---- Evaluation ----
            expected = context.get("expected", "").strip().lower()
            passed = response_text.strip().lower() == expected

            ascope_runtime.finish_run(output=response_text, metrics={"passed": passed})
            return response_text

        except Exception as e:
            ascope_runtime.fail_run(error=str(e), attributes={"case_input": case_input})
            raise

    # ---- Step 8: Run evaluation ----
    print("[8/8] 执行评测")
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

    # ---- Force flush traces before shutdown ----
    print("  [tracing] 强制刷新 trace 缓冲区")
    force_flush()
    time.sleep(1)

    # ---- Shutdown agent runtime ----
    print("  [runtime] 关闭 Agent 运行时")
    shutdown_runtime()

    # ---- Show results ----
    print()
    print("  [results] 查看结果")
    print()

    exps = list_experiments(client, project_id)
    our_exp = next((e for e in exps if e["id"] in [dataset_id]), None)
    run_resp = client.get(f"/api/projects/{project_id}/experiments/{dataset_id}/results") if dataset_id else None
    if run_resp and run_resp.status_code == 200:
        results = run_resp.json()
        print(f"  实验结果数量: {len(results)}")
        for r in results:
            status_icon = "✓" if r["passed"] else "✗"
            run_link = f" → {fe_url}/runs/{r['run_id']}" if r.get("run_id") else " (暂无 Trace)"
            print(f"    {status_icon} score={r['score']:.2f} {r['reason'][:60]}{run_link}")

    print()
    print("=" * 60)
    print("演示完成！以下是可访问的资源：")
    print("=" * 60)
    print()
    print(f"  登录邮箱:     {DEMO_EMAIL}")
    print(f"  登录密码:     {DEMO_PASSWORD}")
    print(f"  前端地址:     {fe_url}")
    print(f"  项目:         {fe_url}/projects/{project_id}")
    print(f"  Agent 列表:   {fe_url}/projects/{project_id}/agents")
    print()
    print("  Run Detail 链接:")
    # List runs for this project
    runs_resp = client.get(f"/api/projects/{project_id}/runs")
    if runs_resp.status_code == 200:
        runs = runs_resp.json()
        for run in runs:
            run_url = f"{fe_url}/runs/{run['id']}"
            print(f"    - [{run['agent_name']}] {run_url}")
    print()
    print("  数据库验证:")
    print("    PostgreSQL:  SELECT id, agent_name, status, trace_id, input_summary, output_summary, error FROM runs ORDER BY created_at DESC LIMIT 10;")
    print("    ClickHouse:  SELECT run_id, count() FROM trace_thoughts GROUP BY run_id ORDER BY count() DESC LIMIT 10;")
    print("    ClickHouse:  SELECT run_id, count() FROM trace_final_answers GROUP BY run_id ORDER BY count() DESC LIMIT 10;")
    print("    ClickHouse:  SELECT run_id, count() FROM trace_spans WHERE run_id != '00000000-0000-0000-0000-000000000000' GROUP BY run_id ORDER BY count() DESC LIMIT 10;")
    print()
    print("  提示：在前端打开 Run Detail 链接，查看完整的 Agent 时间线。")
    print()


if __name__ == "__main__":
    main()
