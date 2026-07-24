#!/usr/bin/env python3
"""
Trace → Annotation → Dataset → Experiment 闭环验证演示脚本。

使用 agentscope_sdk.tracing 上报真实 OpenTelemetry trace，
通过 OTLP HTTP 写入 AgentScope 后端，然后验证闭环流程。

验证流程：
1. 注册用户、创建项目、生成 token
2. 用 agentscope_sdk 注册 Agent 并初始化 runtime
3. 用 root span 包住 agent 执行：agent.run → agent.think → llm.call → tool.call → final_answer
4. 从 Run 创建 Annotation Item
5. 编辑 Annotation Item（corrected_output, failure_category, privacy_reviewed）
6. 提升（Promote）为 Dataset Case
7. 创建 Regression Experiment，跑 deterministic runner 完成评测
8. 调用 scripts/verify-demo-data.sh 验证 PostgreSQL / ClickHouse

用法：
  python3 python/examples/trace_dataset_loop_demo.py --base-url http://localhost:8080

环境变量：
  PGHOST / PGPORT / PGDATABASE / PGUSER / PGPASSWORD — PostgreSQL 连接参数
  CLICKHOUSE_HOST / CLICKHOUSE_PORT / CLICKHOUSE_DATABASE — ClickHouse 连接参数
"""

import argparse
import json
import os
import subprocess
import sys
import time

import requests

from agentscope_sdk.client import RunnerClient
from agentscope_sdk.runner import run_once
from agentscope_sdk.runtime import init as agent_init
from agentscope_sdk.runtime import start_run
from agentscope_sdk.target import target

# ── SDK tracing 和 runtime ─────────────────────────────────────────
from agentscope_sdk.tracing import (
    configure_tracing,
    force_flush,
    record_final_answer,
    trace_llm,
    trace_thought,
    trace_tool,
)

# ── 常量 ──────────────────────────────────────────────────────────────
DEFAULT_BASE = "http://localhost:8080"
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
VERIFY_SCRIPT = os.path.join(SCRIPT_DIR, "..", "..", "scripts", "verify-demo-data.sh")

# ── 参数 ──────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser(description="Trace → Annotation → Dataset → Experiment 闭环验证")
parser.add_argument("--base-url", default=DEFAULT_BASE, help=f"服务端地址（默认 {DEFAULT_BASE}）")
parser.add_argument("--user-email", default="demo-trace-loop@test.com", help="用户邮箱")
parser.add_argument("--user-password", default="demo123", help="用户密码")
parser.add_argument("--project-id", default=None, help="已有项目 ID（可选）")
parser.add_argument("--skip-db-check", action="store_true", help="跳过数据库直接查询验证（调试用）")
args = parser.parse_args()

BASE = args.base_url.rstrip("/")
session = requests.Session()


# ── 辅助函数 ──────────────────────────────────────────────────────────
def log(label: str, value: str = ""):
    tag = f"[{label}]"
    print(f"  {tag:24s} {value}")


def print_separator(title: str):
    width = 70
    print()
    print("=" * width)
    print(f"  {title}")
    print("=" * width)


def api_json(method: str, path: str, **kwargs):
    url = f"{BASE}{path}"
    suppress_error = kwargs.pop("suppress_error", False)
    resp = session.request(method, url, **kwargs)
    if not suppress_error and resp.status_code >= 400:
        print(f"    ❌ {method} {path} → {resp.status_code}: {resp.text[:200]}")
        sys.exit(1)
    return resp.json() if resp.text else {}


def fail(msg: str):
    print(f"\n  ❌ {msg}")
    sys.exit(1)


def check(condition: bool, msg: str):
    if not condition:
        fail(msg)
    print(f"  ✓ {msg}")


# ═══════════════════════════════════════════════════════════════════════
#  步骤 1: 用户注册 / 登录
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 1: 用户注册/登录")

email = args.user_email
password = args.user_password

login_data = api_json("POST", "/api/auth/login",
                       json={"email": email, "password": password},
                       suppress_error=True)
if login_data.get("id"):
    log("登录成功", f"用户 {email}")
    user_id = login_data["id"]
else:
    reg_data = api_json("POST", "/api/auth/register",
                         json={"email": email, "password": password, "display_name": "Demo 用户"})
    log("注册成功", f"用户 {email}")
    user_id = reg_data["id"]

log("用户 ID", user_id)

# ═══════════════════════════════════════════════════════════════════════
#  步骤 2: 项目 / Token
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 2: 项目")

project_id = args.project_id
if not project_id:
    proj_data = api_json("POST", "/api/projects",
                          json={"name": "Trace-Dataset 闭环验证", "description": "自动创建的演示项目"})
    project_id = proj_data["id"]
    log("项目创建成功", project_id)
else:
    log("使用已有项目", project_id)

# 创建 project token（ingest + runner 权限）
token_data = api_json("POST", f"/api/projects/{project_id}/tokens",
                       json={"description": "demo-token", "scopes": ["otlp:write", "runner:write", "agent:register", "agent:runtime"]})
plain_token = token_data.get("plain_token", "")
token_prefix = token_data.get("token", {}).get("token_prefix", "")
log("Token", f"已创建 (prefix={token_prefix})")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 3: 初始化 SDK 并上报真实 Trace
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 3: 注册 Agent 并用 root span 上报真实 Trace")

# 3a. 配置 OTLP tracing（同步导出确保 trace 立刻送达）
tracer = configure_tracing(
    server_url=BASE,
    project_token=plain_token,
    service_name="trace-dataset-loop-demo",
    capture_content=True,
    export_batch_size=1,  # 同步导出
)
log("OTLP tracing 已配置")

# 3b. 通过 SDK runtime 注册 Agent
_rt_instance = agent_init(
    server_url=BASE,
    project_token=plain_token,
    agent_name="demo-agent",
    description="Trace-Dataset 闭环演示 Agent",
    metadata={"demo": "trace-dataset-loop"},
    heartbeat_interval=60,
)
agent_id = _rt_instance.agent_id
instance_id = _rt_instance.instance_id
log("Agent ID", agent_id)
log("Instance ID", instance_id)

# 3c. Root span: agent.run — 用它提取真实的 trace_id 并启动 SDK Run
with tracer.start_as_current_span("agent.run") as root_span:
    # 从 root span 的 SpanContext 提取真实 trace_id（032x hex）
    span_ctx = root_span.get_span_context()
    actual_trace_id = format(span_ctx.trace_id, "032x")
    log("OTLP Trace ID", actual_trace_id)

    # 启动 SDK Run，传入真实的 trace_id
    run = start_run(
        input=json.dumps({"query": "请问如何办理退货？"}),
        metadata={"source": "trace-dataset-loop-demo"},
        trace_id=actual_trace_id,
    )
    if not run or not run.id:
        fail("start_run 返回空，无法继续")
    run_id = run.id
    log("Run ID", run_id)

    # 在 root span 上设置 run/agent 属性，并创建统一 span_attrs
    # 注意：OpenTelemetry 的子 span 只继承 trace context，不会自动继承 attributes
    #       必须显式为每个语义 span helper 传入 span_attrs，不可依赖隐式继承
    root_span.set_attribute("agentscope.run.id", run_id)
    root_span.set_attribute("agentscope.agent.id", agent_id)
    root_span.set_attribute("agentscope.agent.instance_id", instance_id)
    root_span.set_attribute("agentscope.agent.name", "demo-agent")

    span_attrs = {
        "agentscope.run.id": run_id,
        "agentscope.agent.id": agent_id,
        "agentscope.agent.instance_id": instance_id,
        "agentscope.agent.name": "demo-agent",
    }

    # 3d. 在 root span 的 context 内创建语义化 trace span，逐个传入 span_attrs
    #     agent.think — 思考
    with trace_thought(
        tracer, "agent.think",
        summary="用户询问退货流程，需要从知识库查找退货政策和步骤",
        stage="planning",
        confidence=0.95,
        **span_attrs,
    ):
        pass

    #     llm.call — 模拟 LLM 调用
    with trace_llm(
        tracer, "llm.call",
        provider="simulated",
        model="gpt-4o-mini",
        input_summary="如何办理退货？",
        output_summary="我将查询退货知识库并生成回复",
        input_tokens=15,
        output_tokens=25,
        **span_attrs,
    ):
        pass

    #     tool.call — 工具调用
    with trace_tool(
        tracer, "search_knowledge_base",
        tool_type="api",
        arguments_summary=json.dumps({"query": "退货流程"}),
        result_summary="找到退货政策：1. 联系客服 2. 填写申请 3. 寄回商品 4. 退款",
        **span_attrs,
    ):
        pass

    #     final_answer — 最终回答
    with record_final_answer(
        tracer, "agent.answer",
        summary="告知客户退货流程",
        output="尊敬的客户，退换货流程如下：1. 联系在线客服 2. 提交退换货申请 3. 寄回商品 4. 3个工作日内退款",
        **span_attrs,
    ):
        pass

# root span 已退出，所有 Span 已创建

# 3e. 完成 run（通过 SDK Runtime API）
sdk_headers = {"Authorization": f"Bearer {plain_token}"}
api_json("POST", f"/api/sdk/runs/{run_id}/finish",
         json={
             "output": "尊敬的客户，退换货流程如下：1. 联系在线客服 2. 提交退换货申请 3. 寄回商品 4. 3个工作日内退款",
             "metadata": {"completed": True, "has_trace": True},
         },
         headers=sdk_headers)
log("Run 完成")

# 3f. 强制 flush tracing，确保所有 span 已导出
force_flush()
time.sleep(1)
log("Tracing flushed")

# 获取 trace_id（从 run detail 确认）
run_detail = api_json("GET", f"/api/runs/{run_id}")
trace_id = run_detail.get("run", {}).get("trace_id", run_detail.get("trace_id", ""))
log("PostgreSQL trace_id", trace_id)
check(trace_id == actual_trace_id, "PostgreSQL 中 trace_id 与 OTLP trace_id 一致")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 4: 从 Run 创建 Annotation Item
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 4: 创建 Annotation Item")

# 获取或创建 annotation queue
queues_data = api_json("GET", f"/api/projects/{project_id}/annotation-queues")
queue_id = None
for q in queues_data:
    if q["name"] == "线上评测待标注":
        queue_id = q["id"]
        break
if not queue_id:
    queue_data = api_json("POST", f"/api/projects/{project_id}/annotation-queues",
                           json={"name": "线上评测待标注", "description": "自动创建的默认队列"})
    queue_id = queue_data["id"]

log("Annotation Queue", queue_id)

# 从 Run 创建 Annotation Item
annot_item = api_json("POST", f"/api/projects/{project_id}/runs/{run_id}/annotation-items",
                       json={"queue_id": queue_id})
annot_item_id = annot_item.get("id", "")
log("Annotation Item", annot_item_id)
log("  run_id", annot_item.get("run_id", ""))
log("  trace_id", annot_item.get("trace_id", ""))
log("  agent_name", annot_item.get("agent_name", ""))

check(annot_item.get("run_id") == run_id, "annotation_item.run_id 匹配")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 5: 编辑 Annotation Item
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 5: 编辑 Annotation Item")

update_data = api_json("PATCH", f"/api/annotation-items/{annot_item_id}",
                        json={
                            "corrected_output": "尊敬的客户，退换货流程：1. 联系在线客服 2. 提交退换货申请 3. 寄回商品 4. 3个工作日内退款",
                            "expected_tool_path": json.dumps([
                                {"tool": "search_knowledge_base", "args": {"query": "退换货流程"}},
                                {"tool": "generate_response", "args": {"template": "return_process"}},
                            ]),
                            "failure_category": "information_incomplete",
                            "privacy_reviewed": True,
                            "reviewer_note": "已确认不包含个人隐私信息",
                            "comment": "需要补充退换货的具体时限说明",
                        })

check(update_data.get("corrected_output", "").startswith("尊敬的客户"), "corrected_output 已更新")
check(update_data.get("privacy_reviewed") is True, "privacy_reviewed=True")

log("失败分类", update_data.get("failure_category", ""))
log("隐私审核", str(update_data.get("privacy_reviewed", False)))

# ═══════════════════════════════════════════════════════════════════════
#  步骤 6: 创建 Dataset 并 Promote
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 6: 创建 Dataset 并 Promote")

ds_data = api_json("POST", f"/api/projects/{project_id}/datasets",
                    json={"name": "退货流程 Regression 数据集", "description": "从线上 trace 提升的回归测试用例"})
dataset_id = ds_data["id"]
log("Dataset", dataset_id)

promote_data = api_json("POST", f"/api/annotation-items/{annot_item_id}/promote-to-dataset",
                         json={"dataset_id": dataset_id})
case_id = promote_data.get("case_id", "")
log("Promoted Dataset Case", case_id)

# 验证 promote 后的 dataset case metadata
cases_data = api_json("GET", f"/api/datasets/{dataset_id}/cases")
if not cases_data:
    fail("Dataset case 列表为空")
last_case = cases_data[-1]
meta_raw = last_case.get("metadata", "{}")
metadata = json.loads(meta_raw) if isinstance(meta_raw, str) else meta_raw

check(metadata.get("source") == "annotation", "metadata.source == annotation")
check(metadata.get("source_run_id") == run_id, "metadata.source_run_id == run_id")
check(metadata.get("source_trace_id") == trace_id, "metadata.source_trace_id == trace_id")
check(metadata.get("missing_trace_id") is False, "missing_trace_id 为 False（有 trace）")
check(metadata.get("failure_category") == "information_incomplete", "failure_category 正确")
check(metadata.get("privacy_reviewed") is True, "privacy_reviewed 为 True")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 7: 创建 Regression Experiment 并跑 deterministic runner
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 7: Regression Experiment + deterministic runner")

# 7a. 创建 evaluator / metric definition
api_json("POST", f"/api/projects/{project_id}/metric-definitions",
         json={
             "name": "output_non_empty",
             "kind": "final_answer",
             "config": json.dumps({"type": "exact", "weight": 1.0, "pass_threshold": 0.5, "enabled": True}),
         })

eval_config_data = api_json("POST", f"/api/projects/{project_id}/evaluators",
                             json={
                                 "name": "deterministic-checker",
                                 "type": "multi",
                                 "config": {
                                     "type": "multi",
                                     "multi_metrics": [
                                         {
                                             "type": "exact",
                                             "metric_name": "output_match",
                                             "metric_kind": "final_answer",
                                             "weight": 1.0,
                                             "pass_threshold": 0.5,
                                         },
                                     ],
                                     "pass_threshold": 0.5,
                                 },
                             })
evaluator_id = eval_config_data.get("id", "")
log("Evaluator", evaluator_id)

# 7b. 创建 experiment
experiment_data = api_json("POST", f"/api/projects/{project_id}/experiments",
                            json={
                                "dataset_id": dataset_id,
                                "evaluator_id": evaluator_id,
                                "target_name": "demo-agent",
                                "target_version": "1.0.0",
                                "name": "退货流程回归实验",
                                "case_ids": [case_id],
                            })
experiment_id = experiment_data.get("id", "")
log("Experiment", experiment_id)

# 7c. 注册 target 函数（deterministic runner 会用）
@target(name="demo-agent", version="1.0.0")
def demo_agent_target(case_input: str, context: dict) -> str:
    """Deterministic target: returns the expected output."""
    return "退货流程：联系在线客服 → 提交退换货申请 → 寄回商品 → 3个工作日内退款"

# 7d. 创建 RunnerClient 并跑完所有 job
runner_client = RunnerClient(
    server_url=BASE,
    project_token=plain_token,
    runner_name="demo-runner",
)

# Claim 并执行所有 job（最多重试 10 次）
max_attempts = 10
processed_jobs = 0
for attempt in range(max_attempts):
    processed = run_once(runner_client)
    if processed:
        processed_jobs += 1
        log(f"Processed job #{processed_jobs}")
        time.sleep(0.5)
    else:
        if processed_jobs == 0 and attempt < 3:
            # 前 3 次尝试可能还没分配 job
            time.sleep(1)
            continue
        break  # 没有更多 job 了

if processed_jobs == 0:
    fail("Runner 未能 claim 到任何 job，评测无法完成")
log(f"已处理 {processed_jobs} 个 job")

# 7e. 验证 experiment 状态
time.sleep(1)
exp_detail = api_json("GET", f"/api/experiments/{experiment_id}")
exp_status = exp_detail.get("status", "")
log("Experiment 状态", exp_status)
check(exp_status == "completed", f"Experiment 状态为 completed（当前: {exp_status}）")

# 验证 results 数量等于 dataset case 数量
total_cases = exp_detail.get("total_cases", 0)
results_count = exp_detail.get("passed_cases", 0) + exp_detail.get("failed_cases", 0)
log(f"Cases: {total_cases}, Results: {results_count}")
check(results_count == total_cases, f"实验结果数量（{results_count}）等于 case 数量（{total_cases}）")

# 7f. 严格验证 Report API 返回的完整性和维度聚合数据
report_data = api_json("GET", f"/api/projects/{project_id}/experiments/{experiment_id}/report")
report_results = report_data.get("results", [])
report_count = len(report_results)
check(report_count == total_cases,
      f"Report 结果数（{report_count}）等于 case 总数（{total_cases}）")

# 每个 result 必须有非空的 run_id
for i, r in enumerate(report_results):
    rid = r.get("run_id", "")
    if not rid:
        fail(f"Report 第 {i+1} 个结果缺少 run_id")
check(True, "所有 Report result 均有非空 run_id")

# Report 必须包含 dimension_aggregation 维度聚合数据
dim_agg = report_data.get("dimension_aggregation", [])
if not dim_agg:
    fail("Report 缺少 dimension_aggregation（维度聚合数据为空）")
check(len(dim_agg) > 0, f"dimension_aggregation 包含 {len(dim_agg)} 个聚合维度")

# 严格校验每个聚合项的字段语义
for da in dim_agg:
    mk = da.get("metric_kind", "")
    if not mk:
        fail("dimension_aggregation 中存在缺少 metric_kind 的聚合项")
    tc = da.get("total_count", 0)
    if not isinstance(tc, (int, float)) or tc <= 0:
        fail(f"dimension_aggregation['{mk}'].total_count 无效: {tc}")
    ac = da.get("avg_score", -1)
    if not isinstance(ac, (int, float)) or ac < 0 or ac > 1:
        fail(f"dimension_aggregation['{mk}'].avg_score {ac} 超出 [0,1] 范围")
    pc = da.get("passed_count", 0)
    if not isinstance(pc, (int, float)) or pc < 0 or pc > tc:
        fail(f"dimension_aggregation['{mk}'].passed_count {pc} 超出 [0,{tc}]")
check(True, "所有聚合维度的 metric_kind、total_count、avg_score、passed_count 字段有效")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 8: 数据库验证
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 8: 数据库验证")

if args.skip_db_check:
    log("数据库验证", "已跳过（--skip-db-check）")
else:
    # 优先使用 verify-demo-data.sh 脚本
    if os.path.exists(VERIFY_SCRIPT):
        log("调用 verify-demo-data.sh", VERIFY_SCRIPT)
        result = subprocess.run(
            ["bash", VERIFY_SCRIPT,
             "--project-id", project_id,
             "--run-id", run_id,
             "--annotation-item-id", annot_item_id,
             "--dataset-case-id", case_id,
             "--experiment-id", experiment_id],
            capture_output=False,
        )
        if result.returncode != 0:
            fail("verify-demo-data.sh 验证失败")
        log("verify-demo-data.sh 全部通过")
    else:
        log("verify-demo-data.sh 不存在，使用 Python 客户端验证",
            "（环境变量中未设置 DB 连接参数将自动跳过）")
        # Python 客户端验证（注：下面的错误会被限制，但如果 clickhouse-connect/psycopg2
        # 已安装且 DB 可达，验证会严格执行）
        use_python = False
        try:
            import clickhouse_connect
            import psycopg2
            use_python = True
        except ImportError:
            log("Python DB 验证", "跳过（无 clickhouse_connect 或 psycopg2）")

        if use_python:
            # ClickHouse 验证
            ch_host = os.environ.get("CLICKHOUSE_HOST", "localhost")
            ch_port = int(os.environ.get("CLICKHOUSE_PORT", "19000"))
            pg_host = os.environ.get("PGHOST", "localhost")
            pg_port = os.environ.get("PGPORT", "5432")
            pg_db = os.environ.get("PGDATABASE", "agentscope_test")
            pg_user = os.environ.get("PGUSER", "agentscope")
            pg_pass = os.environ.get("PGPASSWORD", "agentscope")

            # ClickHouse
            ch_client = clickhouse_connect.get_client(
                host=ch_host, port=ch_port, database="agentscope_traces",
                username="default", password="")
            try:
                ch_spans = ch_client.query(
                    "SELECT count() AS cnt FROM trace_spans WHERE run_id = {run_id:String}",
                    parameters={"run_id": run_id}).result_rows[0][0]
                check(ch_spans > 0, f"trace_spans 中该 run_id 存在（{ch_spans} 条）")

                ch_answers = ch_client.query(
                    "SELECT count() AS cnt FROM trace_final_answers WHERE run_id = {run_id:String}",
                    parameters={"run_id": run_id}).result_rows[0][0]
                check(ch_answers > 0, f"trace_final_answers 中该 run_id 存在（{ch_answers} 条）")
            finally:
                ch_client.close()

            # PostgreSQL
            pg_conn = psycopg2.connect(
                host=pg_host, port=pg_port, dbname=pg_db,
                user=pg_user, password=pg_pass)
            try:
                pg_cur = pg_conn.cursor()
                # annotation_items
                pg_cur.execute(
                    "SELECT id, run_id, trace_id FROM annotation_items WHERE run_id = %s ORDER BY created_at DESC LIMIT 5",
                    (run_id,))
                pg_rows = pg_cur.fetchall()
                check(len(pg_rows) > 0, "annotation_items 中存在该 run_id")
                if pg_rows:
                    row = pg_rows[0]
                    check(row[1] == run_id, f"run_id 正确（{row[1]}）")
                    check(row[2] == trace_id, f"trace_id 正确（{row[2]}）")

                # dataset_cases
                pg_cur.execute(
                    "SELECT id, metadata FROM dataset_cases WHERE id = %s", (case_id,))
                case_row = pg_cur.fetchone()
                if case_row:
                    case_meta_str = case_row[1]
                    case_meta = json.loads(case_meta_str) if isinstance(case_meta_str, str) else case_meta_str
                    check(case_meta.get("source_run_id") == run_id, "dataset_cases.metadata.source_run_id 正确")
                    check(case_meta.get("source_trace_id") == trace_id, "dataset_cases.metadata.source_trace_id 正确")
                    check(case_meta.get("missing_trace_id") is False, "dataset_cases.metadata.missing_trace_id == false")

                pg_cur.close()
            finally:
                pg_conn.close()
            log("Python DB 验证全部通过")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 9: 输出摘要
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 9: 结果摘要")

log("用户邮箱", email)
log("用户密码", password)
log("Project ID", project_id)
log("Run ID", run_id)
log("Trace ID", trace_id)
log("Annotation Item ID", annot_item_id)
log("Dataset ID", dataset_id)
log("Dataset Case ID", case_id)
log("Experiment ID", experiment_id)
log("Experiment 状态", exp_status)
log("Processed Jobs", str(processed_jobs))

# 前端链接（对照 web/src/app/App.tsx 确认）
frontend_base = BASE.replace(":8080", ":5173") if ":8080" in BASE else BASE.replace("localhost", "localhost:5173")
log("")
log("前端链接")
log("  Run Detail", f"{frontend_base}/runs/{run_id}")
log("  Annotation Queue", f"{frontend_base}/projects/{project_id}/annotation-queues")
log("  Experiment", f"{frontend_base}/experiments/{experiment_id}")

print()
print("✅ Trace → Annotation → Dataset → Experiment 闭环验证通过")
print()
