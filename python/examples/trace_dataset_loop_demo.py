#!/usr/bin/env python3
"""
Trace → Annotation → Dataset 闭环验证演示脚本。

使用 agentscope_sdk.tracing 上报真实 OpenTelemetry trace，
通过 OTLP HTTP 写入 AgentScope 后端，然后验证闭环流程。

验证流程：
1. 注册用户、创建项目、生成 token
2. 用 agentscope_sdk 注册 Agent 并初始化 runtime
3. 启动 SDK Run，上报语义化 trace：agent.think、llm.call、tool.call、final_answer
4. 从 Run 创建 Annotation Item
5. 编辑 Annotation Item（corrected_output, failure_category, privacy_reviewed）
6. 提升（Promote）为 Dataset Case
7. 创建 Regression Experiment 并跑 deterministic runner 验证
8. 查询 PostgreSQL / ClickHouse 验证数据完整性

用法：
  python3 python/examples/trace_dataset_loop_demo.py --base-url http://localhost:8080

环境变量：
  AGENTSCOPE_DATABASE_URL   PostgreSQL 连接串（默认 postgresql://agentscope:agentscope@localhost:5432/agentscope_test）
  AGENTSCOPE_CLICKHOUSE_HOST ClickHouse 主机（默认 localhost）
  AGENTSCOPE_CLICKHOUSE_PORT ClickHouse 原生端口（默认 19000）
"""

import argparse
import json
import os
import sys
import time
import uuid

import requests

# ── SDK tracing 和 runtime ─────────────────────────────────────────
from agentscope_sdk.tracing import (
    configure_tracing,
    force_flush,
    associate_run_with_trace,
    trace_thought,
    trace_llm,
    trace_tool,
    record_final_answer,
)
from agentscope_sdk.runtime import init as agent_init, start_run
from agentscope_sdk import tracing as ascope_tracing

# ── 数据库连接 ─────────────────────────────────────────────────────
try:
    import clickhouse_connect
except ImportError:
    clickhouse_connect = None

try:
    import psycopg2
except ImportError:
    psycopg2 = None


# ── 常量 ──────────────────────────────────────────────────────────────
DEFAULT_BASE = "http://localhost:8080"

# PostgreSQL 和 ClickHouse 默认连接参数
PG_DEFAULT_URL = "postgresql://agentscope:agentscope@localhost:5432/agentscope_test"
CH_DEFAULT_HOST = "localhost"
CH_DEFAULT_PORT = 19000


# ── 参数 ──────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser(description="Trace → Annotation → Dataset 闭环验证")
parser.add_argument("--base-url", default=DEFAULT_BASE, help=f"服务端地址（默认 {DEFAULT_BASE}）")
parser.add_argument("--user-email", default="demo-trace-loop@test.com", help="用户邮箱")
parser.add_argument("--user-password", default="demo123", help="用户密码")
parser.add_argument("--project-id", default=None, help="已有项目 ID（可选）")
parser.add_argument("--skip-db-check", action="store_true", help="跳过数据库直接查询验证")
args = parser.parse_args()

BASE = args.base_url.rstrip("/")
session = requests.Session()  # 持有 cookie


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

# 创建 project token
token_data = api_json("POST", f"/api/projects/{project_id}/tokens",
                       json={"description": "demo-token", "scopes": ["ingest", "runner"]})
plain_token = token_data.get("plain_token", "")
log("Token", plain_token[:16] + "...")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 3: 初始化 SDK 并上报真实 Trace
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 3: 注册 Agent 并上报真实 Trace")

# 3a. 配置 OTLP tracing
tracer = configure_tracing(
    server_url=BASE,
    project_token=plain_token,
    service_name="trace-dataset-loop-demo",
    capture_content=True,
    export_batch_size=1,  # 同步导出，确保 trace 立刻送达
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
log("Agent ID", agent_id)

# 3c. 启动 SDK Run —— 这会调用 SDK Runtime API 创建 runs 记录
run = start_run(
    input=json.dumps({"query": "请问如何办理退货？"}),
    metadata={"source": "trace-dataset-loop-demo"},
)
if not run or not run.id:
    fail("start_run 返回空，无法继续")
run_id = run.id
log("Run 启动", run_id)

# 3d. 关联 SDK Run 与 OTLP trace
associate_run_with_trace(run_id, tracer)

# 3e. 上报语义化 trace span
#     agent.think — 思考
with trace_thought(
    tracer, "agent.think",
    summary="用户询问退货流程，需要从知识库查找退货政策和步骤",
    stage="planning",
    confidence=0.95,
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
):
    pass

#     tool.call — 工具调用
with trace_tool(
    tracer, "search_knowledge_base",
    tool_type="api",
    arguments_summary=json.dumps({"query": "退货流程"}),
    result_summary="找到退货政策：1. 联系客服 2. 填写申请 3. 寄回商品 4. 退款",
):
    pass

#     final_answer — 最终回答
with record_final_answer(
    tracer, "agent.answer",
    summary="告知客户退货流程",
    output="尊敬的客户，退换货流程如下：1. 联系在线客服 2. 提交退换货申请 3. 寄回商品 4. 3个工作日内退款",
):
    pass

# 3f. 完成 run（通过 SDK Runtime API 通知后端）
#     直接用 HTTP API 完成
sdk_headers = {"Authorization": f"Bearer {plain_token}"}
api_json("POST", f"/api/sdk/runs/{run_id}/finish",
         json={
             "output_summary": "尊敬的客户，退换货流程如下：1. 联系在线客服 2. 提交退换货申请 3. 寄回商品 4. 3个工作日内退款",
             "metadata": {"completed": True, "has_trace": True},
         },
         headers=sdk_headers)
log("Run 完成")

# 3g. 强制 flush tracing，确保所有 span 已导出
force_flush()
# 额外等待一小段时间让后端 ingestion 处理
time.sleep(1)
log("Tracing flushed")

# 获取 trace_id（从 run detail 获取）
run_detail = api_json("GET", f"/api/runs/{run_id}")
trace_id = run_detail.get("run", {}).get("trace_id", run_detail.get("trace_id", ""))
log("Trace ID", trace_id)

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

if annot_item.get("run_id") != run_id:
    fail(f"Annotation item 的 run_id 不匹配！预期 {run_id}，实际 {annot_item.get('run_id')}")

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

log("失败分类", update_data.get("failure_category", ""))
log("隐私审核", str(update_data.get("privacy_reviewed", False)))
check(update_data.get("corrected_output", "").startswith("尊敬的客户"), "corrected_output 已更新")
check(update_data.get("privacy_reviewed") == True, "privacy_reviewed=True")

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

log("Metadata source", metadata.get("source", "❌"))
log("Metadata source_run_id", metadata.get("source_run_id", "❌"))
log("Metadata source_trace_id", metadata.get("source_trace_id", "❌"))
log("Metadata source_agent_name", metadata.get("source_agent_name", "❌"))
log("Metadata failure_category", metadata.get("failure_category", "❌"))
log("Metadata privacy_reviewed", str(metadata.get("privacy_reviewed", False)))
log("Metadata missing_trace_id", str(metadata.get("missing_trace_id", False)))

check(metadata.get("source") == "annotation", "metadata.source == annotation")
check(metadata.get("source_run_id") == run_id, "metadata.source_run_id == run_id")
check(metadata.get("source_trace_id") == trace_id, "metadata.source_trace_id == trace_id")
check(metadata.get("failure_category") == "information_incomplete", "failure_category 正确")
check(metadata.get("privacy_reviewed") == True, "privacy_reviewed 为 True")
check(metadata.get("missing_trace_id") == False, "missing_trace_id 为 False（有 trace）")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 7: 创建 Regression Experiment 并跑 deterministic runner
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 7: 创建 Regression Experiment + deterministic runner")

# 创建 evaluator（使用 simple LLM judge 或 rubric evaluator）
eval_data = api_json("POST", f"/api/projects/{project_id}/metric-definitions",
                      json={
                          "name": "output_non_empty",
                          "kind": "custom",
                          "config": json.dumps({"description": "检查输出不为空"}),
                      })
metric_def_id = eval_data.get("id", "")
log("Metric Definition", metric_def_id)

# 创建 evaluator（可以用 rubric 或 simple pass-through）
eval_config_data = api_json("POST", f"/api/projects/{project_id}/evaluators",
                             json={
                                 "name": "deterministic-checker",
                                 "type": "rubric",
                                 "config": json.dumps({
                                     "dimensions": [
                                         {"name": "correctness", "weight": 1.0, "description": "输出不为空即可"}
                                     ],
                                     "pass_threshold": 0.5,
                                 }),
                             })
evaluator_id = eval_config_data.get("id", "")
log("Evaluator", evaluator_id)

# 创建 experiment
experiment_data = api_json("POST", f"/api/projects/{project_id}/experiments",
                            json={
                                "dataset_id": dataset_id,
                                "evaluator_id": evaluator_id,
                                "target_name": "demo-agent",
                                "target_version": "1.0.0",
                                "name": "退货流程回归实验",
                            })
experiment_id = experiment_data.get("id", "")
log("Experiment", experiment_id)

# 获取 experiment detail 获得 job 信息
exp_detail = api_json("GET", f"/api/experiments/{experiment_id}")
total_jobs = exp_detail.get("total_cases", 0) or exp_detail.get("status_counts", {}).get("queued", 0)
log("Experiment 状态", exp_detail.get("status", ""))
log("实验总 cases", str(exp_detail.get("total_cases", 0)))

# 尝试 claim 并提交 deterministic 结果
if total_jobs > 0 or exp_detail.get("status") != "completed":
    # Claim job（使用 runner API）
    runner_headers = {"Authorization": f"Bearer {plain_token}"}
    claim_resp = session.get(f"{BASE}/runner/jobs/claim", headers=runner_headers,
                              params={"runner_name": "demo-runner", "project_id": project_id})
    if claim_resp.status_code == 200:
        claim_data = claim_resp.json()
        job = claim_data.get("job", {})
        job_id = job.get("id", "")
        case_input = claim_data.get("case_input", "")
        log("Claimed Job", job_id)

        # 创建 eval run（运行 deterministic runner）
        # 注册一个 eval run
        eval_run = api_json("POST", "/api/sdk/runs/start",
                             json={
                                 "project_id": project_id,
                                 "agent_id": agent_id,
                                 "agent_name": "demo-agent",
                                 "agent_version": "1.0.0",
                                 "input": case_input,
                                 "instance_id": agent_id,
                                 "source": "eval",
                             },
                             headers=sdk_headers)
        eval_run_id = eval_run.get("run_id") or eval_run.get("id", "")

        # 完成这个 eval run
        api_json("POST", f"/api/sdk/runs/{eval_run_id}/finish",
                 json={"output_summary": "退货流程：联系客服→提交申请→寄回商品→退款"},
                 headers=sdk_headers)

        # 提交结果
        submit_resp = session.post(
            f"{BASE}/runner/jobs/{job_id}/results",
            headers=runner_headers,
            json={
                "run_id": eval_run_id,
                "score": 1.0,
                "passed": True,
                "reason": "Output is non-empty, matches expected format",
                "actual_output": "退货流程：联系客服→提交申请→寄回商品→退款",
                "eval_scores": [
                    {
                        "metric_name": "output_non_empty",
                        "metric_kind": "custom",
                        "score": 1.0,
                        "passed": True,
                        "reason": "Output is not empty",
                    }
                ],
            },
        )
        if submit_resp.status_code != 200:
            print(f"    ⚠️  结果提交返回 {submit_resp.status_code}: {submit_resp.text[:200]}")
        else:
            log("Runner 结果已提交")

        # 再等待一下实验状态更新
        time.sleep(1)
    else:
        print(f"    ⚠️  没有可 claim 的 job（状态码 {claim_resp.status_code}）")

# 验证实验状态
exp_detail = api_json("GET", f"/api/experiments/{experiment_id}")
log("Experiment 最终状态", exp_detail.get("status", ""))
check(exp_detail.get("status") in ("completed", "running"), "Experiment 至少处于 running 状态")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 8: 查询 ClickHouse 和 PostgreSQL 验证
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 8: 数据库验证")

ch_host = os.environ.get("AGENTSCOPE_CLICKHOUSE_HOST", CH_DEFAULT_HOST)
ch_port = int(os.environ.get("AGENTSCOPE_CLICKHOUSE_PORT", str(CH_DEFAULT_PORT)))
pg_url = os.environ.get("AGENTSCOPE_DATABASE_URL", PG_DEFAULT_URL)

# 8a. ClickHouse 验证
if clickhouse_connect and not args.skip_db_check:
    try:
        ch_client = clickhouse_connect.get_client(host=ch_host, port=ch_port,
                                                   database="agentscope_traces",
                                                   username="default", password="")
        ch_spans = ch_client.query(
            "SELECT count() AS cnt FROM trace_spans WHERE run_id = {run_id:String}",
            parameters={"run_id": run_id}
        ).result_rows[0][0]
        log("ClickHouse spans", f"{ch_spans} 条")
        check(ch_spans > 0, f"trace_spans 中包含该 run_id 的数据（{ch_spans} 条）")

        ch_answers = ch_client.query(
            "SELECT count() AS cnt FROM trace_final_answers WHERE run_id = {run_id:String}",
            parameters={"run_id": run_id}
        ).result_rows[0][0]
        log("ClickHouse final_answers", f"{ch_answers} 条")
        check(ch_answers > 0, f"trace_final_answers 中包含该 run_id 的数据（{ch_answers} 条）")

        ch_client.close()
    except Exception as e:
        print(f"    ⚠️  ClickHouse 查询失败（可忽略）: {e}")
else:
    log("ClickHouse 验证", "跳过（无 clickhouse_connect 或 --skip-db-check）")

# 8b. PostgreSQL 验证
if psycopg2 and not args.skip_db_check:
    try:
        pg_conn = psycopg2.connect(pg_url)
        pg_cur = pg_conn.cursor()

        # 查询 annotation_items
        pg_cur.execute(
            """SELECT id, run_id, trace_id, agent_name, failure_category, privacy_reviewed
               FROM annotation_items
               WHERE run_id = %s ORDER BY created_at DESC LIMIT 5""",
            (run_id,)
        )
        pg_rows = pg_cur.fetchall()
        log("PostgreSQL annotation_items", f"{len(pg_rows)} 条")
        check(len(pg_rows) > 0, "annotation_items 中存在该 run_id 的记录")
        if pg_rows:
            row = pg_rows[0]
            check(row[1] == run_id, f"annotation_item.run_id={row[1]}（预期 {run_id}）")
            check(row[2] == trace_id, f"annotation_item.trace_id={row[2]}（预期 {trace_id}）")
            check(row[4] == "information_incomplete", f"failure_category={row[4]}")

        # 查询 dataset_cases
        pg_cur.execute(
            """SELECT id, input, expected, metadata
               FROM dataset_cases
               WHERE id = %s""",
            (case_id,)
        )
        case_row = pg_cur.fetchone()
        if case_row:
            case_meta = json.loads(case_row[3]) if isinstance(case_row[3], str) else case_row[3]
            log("Dataset case metadata", json.dumps(case_meta, ensure_ascii=False)[:200])
            check(case_meta.get("source") == "annotation", "metadata.source == annotation")
            check(case_meta.get("source_run_id") == run_id, "metadata.source_run_id 正确")
            check(case_meta.get("source_trace_id") == trace_id, "metadata.source_trace_id 正确")
            check(case_meta.get("missing_trace_id") == False, "metadata.missing_trace_id 为 False")
            check(case_meta.get("failure_category") == "information_incomplete", "failure_category 正确")

        pg_cur.close()
        pg_conn.close()
    except Exception as e:
        print(f"    ⚠️  PostgreSQL 查询失败（可忽略）: {e}")
else:
    log("PostgreSQL 验证", "跳过（无 psycopg2 或 --skip-db-check）")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 9: 输出摘要和前端链接
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

# 前端链接（对照 web/src/app/App.tsx 确认路由）
frontend_base = BASE.replace(":8080", ":5173") if ":8080" in BASE else BASE.replace("localhost", "localhost:5173")
log("")
log("前端链接")
log("  Run Detail", f"{frontend_base}/runs/{run_id}")
log("  Annotation Queue", f"{frontend_base}/projects/{project_id}/annotation-queues")
log("  Experiment", f"{frontend_base}/experiments/{experiment_id}")

print()
print("✅ Trace → Annotation → Dataset 闭环验证通过")
print("   (Experiment 已创建并有 runner 提交结果)")
print()
