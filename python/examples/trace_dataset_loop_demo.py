#!/usr/bin/env python3
"""
Trace → Annotation → Dataset → Experiment 闭环演示脚本。

验证流程：
1. 注册用户、创建项目、生成 token
2. 模拟一次 agent run（通过 SDK API）
3. 从 Run 创建 Annotation Item
4. 编辑 Annotation Item（corrected_output, failure_category, privacy_reviewed）
5. 提升（Promote）为 Dataset Case
6. 创建 Regression Experiment
7. 查询并验证数据完整性

用法：
  python3 python/examples/trace_dataset_loop_demo.py --base-url http://localhost:8080

注意：
  - 不需要真实 LLM key
  - 如果已有项目/用户，可以通过 --user-email 和 --project-id 跳过创建步骤
  - 输出前端链接供手动验证
"""

import argparse
import json
import os
import sys
import time
import uuid

import requests

# ── 常量 ──────────────────────────────────────────────────────────────
DEFAULT_BASE = "http://localhost:8080"
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))

# ── 参数 ──────────────────────────────────────────────────────────────
parser = argparse.ArgumentParser(description="Trace → Annotation → Dataset 闭环验证")
parser.add_argument("--base-url", default=DEFAULT_BASE, help=f"服务端地址（默认 {DEFAULT_BASE}）")
parser.add_argument("--user-email", default="demo-trace-loop@test.com", help="用户邮箱")
parser.add_argument("--user-password", default="demo123", help="用户密码")
parser.add_argument("--project-id", default=None, help="已有项目 ID（可选）")
parser.add_argument("--dataset-id", default=None, help="已有数据集 ID（可选）")
parser.add_argument("--skip-agent-run", action="store_true", help="跳过 agent run 步骤（仅验证 annotation 流程）")
args = parser.parse_args()

BASE = args.base_url.rstrip("/")
session = requests.Session()  # 持有 cookie


# ── 辅助函数 ──────────────────────────────────────────────────────────
def log(label: str, value: str = ""):
    """打印带标记的输出"""
    tag = f"[{label}]"
    print(f"  {tag:24s} {value}")


def print_separator(title: str):
    width = 70
    print()
    print("=" * width)
    print(f"  {title}")
    print("=" * width)


def api_json(method: str, path: str, **kwargs) -> dict:
    """发送 API 请求并返回 JSON"""
    url = f"{BASE}{path}"
    suppress_error = kwargs.pop("suppress_error", False)
    resp = session.request(method, url, **kwargs)
    if not suppress_error and resp.status_code >= 400:
        print(f"    ❌ {method} {path} → {resp.status_code}: {resp.text[:200]}")
        print(f"    API 调用失败，终止流程")
        sys.exit(1)
    return resp.json() if resp.text else {}


# ═══════════════════════════════════════════════════════════════════════
#  步骤 1: 用户注册 / 登录
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 1: 用户注册/登录")

email = args.user_email
password = args.user_password

# 尝试登录
login_data = api_json("POST", "/api/auth/login",
                       json={"email": email, "password": password},
                       suppress_error=True)
if "id" in login_data:
    log("登录成功", f"用户 {email}")
    user_id = login_data["id"]
else:
    # 注册
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

# 创建 project token（用于 SDK API）
token_data = api_json("POST", f"/api/projects/{project_id}/tokens",
                       json={"description": "demo-token", "scopes": ["ingest", "runner"]})
plain_token = token_data.get("plain_token", "")
log("Token", plain_token[:16] + "...")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 3: 注册 Agent 并启动一次带 Trace 的 Run
# ═══════════════════════════════════════════════════════════════════════
if not args.skip_agent_run:
    print_separator("步骤 3: Agent Run（带 Trace）")

    # 注册 agent（SDK API，使用 token auth）
    sdk_headers = {"Authorization": f"Bearer {plain_token}"}
    agent_reg = api_json("POST", "/api/v1/agents/register",
                          json={
                              "project_id": project_id,
                              "name": "demo-agent",
                              "description": "Trace-Dataset 闭环演示 Agent",
                          },
                          headers=sdk_headers)
    agent_id = agent_reg.get("id", agent_reg.get("agent_id", ""))
    log("Agent 注册", agent_id)

    # 启动 run（SDK Runtime API）
    run_start = api_json("POST", "/api/sdk/runs/start",
                          json={
                              "project_id": project_id,
                              "agent_id": agent_id,
                              "agent_name": "demo-agent",
                              "agent_version": "1.0.0",
                              "input": json.dumps({"query": "如何办理退货？"}),
                              "trace_id": f"demo-trace-{uuid.uuid4().hex[:16]}",
                              "instance_id": agent_id,
                          },
                          headers=sdk_headers)
    run_id = run_start.get("run_id") or run_start.get("id", "")
    trace_id = run_start.get("trace_id", "")
    log("Run 启动", run_id)

    # 更新 run 状态到 running
    api_json("POST", f"/api/sdk/runs/{run_id}/state",
             json={"status": "running", "message": "开始执行"},
             headers=sdk_headers)

    # 模拟一段时间后完成
    api_json("POST", f"/api/sdk/runs/{run_id}/finish",
             json={
                 "output_summary": "尊敬的客户，退换货流程如下：1. 联系客服 2. 填写退换货申请 3. 寄回商品 4. 退款处理",
                 "metadata": {"completed": True},
             },
             headers=sdk_headers)
    log("Run 完成", run_id)
    log("Trace ID", trace_id)
else:
    # 跳过 agent run 时，从已有的 runs 列表取最近的一个
    runs_data = api_json("GET", f"/api/projects/{project_id}/runs")
    if not runs_data:
        print("    ⚠️  没有可用的 run，请先启动 agent 或不要使用 --skip-agent-run")
        sys.exit(1)
    last_run = runs_data[0]
    run_id = last_run["id"]
    trace_id = last_run.get("trace_id", "")
    log("复用已有 Run", run_id)

# ═══════════════════════════════════════════════════════════════════════
#  步骤 4: 创建 Annotation Queue
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 4: 创建 Annotation Item（从 Run）")

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
log("  status", annot_item.get("status", ""))
log("  privacy_reviewed", str(annot_item.get("privacy_reviewed", False)))

# 验证 source_run_id 和 source_trace_id 已被复制
if annot_item.get("run_id") != run_id:
    print(f"    ⚠️  run_id 不匹配！预期 {run_id}，实际 {annot_item.get('run_id')}")

# ═══════════════════════════════════════════════════════════════════════
#  步骤 5: 编辑 Annotation Item
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 5: 编辑 Annotation Item")

# 更新 corrected_output, failure_category, privacy_reviewed
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

log("Annotation Item 更新成功")
log("  corrected_output", update_data.get("corrected_output", "")[:50] + "...")
log("  failure_category", update_data.get("failure_category", ""))
log("  privacy_reviewed", str(update_data.get("privacy_reviewed", False)))
log("  reviewer_note", update_data.get("reviewer_note", ""))

# ═══════════════════════════════════════════════════════════════════════
#  步骤 6: 创建 Dataset 并 Promote
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 6: 创建 Dataset 并 Promote")

# 创建 dataset
ds_data = api_json("POST", f"/api/projects/{project_id}/datasets",
                    json={"name": "退换货流程 Regression 数据集", "description": "从线上 trace 提升的回归测试用例"})
dataset_id = ds_data["id"]
log("Dataset 创建", dataset_id)

# Promote annotation item to dataset case
promote_data = api_json("POST", f"/api/annotation-items/{annot_item_id}/promote-to-dataset",
                         json={"dataset_id": dataset_id})
case_id = promote_data.get("case_id", "")
log("Promote 成功", f"case_id = {case_id}")

# 验证 dataset case metadata
cases_data = api_json("GET", f"/api/datasets/{dataset_id}/cases")
if cases_data:
    last_case = cases_data[-1]
    metadata_str = last_case.get("metadata", "{}")
    if isinstance(metadata_str, str):
        metadata = json.loads(metadata_str)
    else:
        metadata = metadata_str
    log("Case 来源", metadata.get("source", "❌ missing"))
    log("来源 Run", metadata.get("source_run_id", "❌ missing"))
    log("来源 Trace", metadata.get("source_trace_id", "❌ missing"))
    log("来源 Agent", metadata.get("source_agent_name", "❌ missing"))
    log("失败分类", metadata.get("failure_category", "❌ missing"))

# ═══════════════════════════════════════════════════════════════════════
#  步骤 7: 创建 Regression Experiment
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 7: 创建 Regression Experiment")

# 尝试获取 evaluator（如果没有，就跳过后面的 runner）
evaluators_data = api_json("GET", f"/api/projects/{project_id}/evaluators")
evaluator_id = None

experiment_data = api_json("POST", f"/api/projects/{project_id}/experiments",
                            json={
                                "dataset_id": dataset_id,
                                "evaluator_id": evaluator_id if evaluator_id else None,
                                "target_name": "demo-agent",
                                "target_version": "1.0.0",
                                "name": "Trace-Dataset 回归实验",
                            })
experiment_id = experiment_data.get("id", "")
log("Experiment 创建", experiment_id)

# ═══════════════════════════════════════════════════════════════════════
#  步骤 8: 最终验证
# ═══════════════════════════════════════════════════════════════════════
print_separator("步骤 8: 验证结果")

log("最终结果")
log("  用户邮箱", email)
log("  用户密码", password)
log("  Project ID", project_id)
log("  Run ID", run_id)
log("  Trace ID", trace_id)
log("  Annotation Item ID", annot_item_id)
log("  Dataset ID", dataset_id)
log("  Dataset Case ID", case_id)
log("  Experiment ID", experiment_id)

# 前端链接
frontend_base = BASE.replace(":8080", ":5173") if ":8080" in BASE else BASE.replace("localhost", "localhost:5173")
log("")
log("前端链接（替换实际 URL）")
log("  Run Detail", f"{frontend_base}/runs/{run_id}")
log("  Annotation", f"{frontend_base}/projects/{project_id}/annotation")
log("  Dataset", f"{frontend_base}/projects/{project_id}/datasets/{dataset_id}")
log("  Experiment", f"{frontend_base}/projects/{project_id}/experiments/{experiment_id}")

print()
print("✅ Trace → Annotation → Dataset → Experiment 闭环验证完成")
print()
