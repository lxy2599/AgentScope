#!/usr/bin/env python3
"""Demo: 实验对比闭环——Baseline vs Candidate 确定性评测。

该 Demo 使用真实 SDK/runtime 创建 Run 和 OTLP trace，通过 runner job 系统
提交多维度指标（含 safety metric），然后创建 Comparison 验证 regression gate。

验证流程:
  1. 注册用户、创建项目、数据集、3 个用例
  2. 创建 Baseline 实验，让 case A fail、B/C pass
  3. 创建 Candidate 实验，让 case A pass（improved）、B pass（unchanged）、C fail（regressed）
  4. Candidate case C 额外提交 safety metric = 0（fail），触发 safety veto
  5. 创建 Comparison，断言 improved=1、regressed=1、unchanged=1、paired=3
  6. 验证 regression gate 触发（safety veto + pass rate 下降）
  7. 从回归 case 创建 Annotation Item
  8. PostgreSQL 和 ClickHouse 数据精确验证

用法:
  python3 python/examples/demo_experiment_comparison.py --server-url http://localhost:8080

环境变量:
  AS_SERVER_URL    服务端地址（默认 http://localhost:8080）
  PGHOST / PGPORT / PGDATABASE / PGUSER / PGPASSWORD — PostgreSQL 连接参数
  CLICKHOUSE_HOST / CLICKHOUSE_PORT — ClickHouse 连接参数
"""

import argparse
import json
import os
import subprocess
import sys
import time
import uuid

import requests

from agentscope_sdk.client import JobResultPayload, RunnerClient

# ── SDK ──────────────────────────────────────────────────────────────────
from agentscope_sdk.runtime import init as agent_init
from agentscope_sdk.runtime import start_run
from agentscope_sdk.tracing import configure_tracing

# ── 常量 ──────────────────────────────────────────────────────────────────
DEFAULT_SERVER_URL = "http://localhost:8080"

# ── 辅助 ──────────────────────────────────────────────────────────────────


def log(label: str, value: str = ""):
    tag = f"[{label}]"
    print(f"  {tag:24s} {value}")


def print_separator(title: str):
    width = 70
    print()
    print("=" * width)
    print(f"  {title}")
    print("=" * width)


def fail(msg: str):
    print(f"\n  ❌ {msg}")
    sys.exit(1)


def check(condition: bool, msg: str):
    if not condition:
        fail(msg)
    print(f"  ✓ {msg}")


# ═══════════════════════════════════════════════════════════════════════════
# 主流程
# ═══════════════════════════════════════════════════════════════════════════


def main():
    # ── 参数 ──────────────────────────────────────────────────────────
    parser = argparse.ArgumentParser(description="实验对比闭环 Demo")
    parser.add_argument(
        "--server-url",
        default=os.environ.get("AS_SERVER_URL", DEFAULT_SERVER_URL),
        help=f"服务端地址（默认 {DEFAULT_SERVER_URL}，环境变量 AS_SERVER_URL 可覆盖）",
    )
    parser.add_argument("--skip-db-check", action="store_true", help="跳过数据库直接查询")
    args = parser.parse_args()

    BASE = args.server_url.rstrip("/")
    session = requests.Session()

    print("=" * 60)
    print("AgentScope 实验对比闭环 Demo")
    print("=" * 60)

    # ── Healthz ───────────────────────────────────────────────────────
    print_separator("预检: 服务端健康检查")
    try:
        resp = session.get(f"{BASE}/healthz", timeout=5)
        resp.raise_for_status()
        health = resp.json()
        check(health.get("ok") or health.get("status") == "ok",
              f"服务端运行正常 ({BASE}/healthz)")
    except Exception as e:
        print(f"  ❌ 无法连接服务端 {BASE}: {e}")
        print("     请确认服务端正在运行: make server")
        sys.exit(1)

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 1: 用户注册 / 登录
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 1: 用户注册/登录")

    email = f"demo-comp-{uuid.uuid4().hex[:8]}@test.com"
    password = "demo123"

    login_resp = session.post(
        f"{BASE}/api/auth/login",
        json={"email": email, "password": password},
        timeout=30,
    )
    if login_resp.status_code == 200:
        log("登录成功", email)
    else:
        reg_resp = session.post(
            f"{BASE}/api/auth/register",
            json={"email": email, "password": password, "display_name": "实验对比 Demo"},
            timeout=30,
        )
        if reg_resp.status_code == 201:
            log("注册成功", email)
        else:
            fail(f"用户注册失败: {reg_resp.status_code} {reg_resp.text[:100]}")

    # 获取用户 ID（从 register/login 响应中提取）
    user_id = (
        login_resp.json().get("id", "")
        if login_resp.status_code == 200
        else reg_resp.json().get("id", "")
    )
    if not user_id:
        fail("无法获取用户 ID")
    log("用户 ID", user_id)

    # 辅助函数（使用 session cookie 认证）
    def api_json(method, path, **kw):
        url = f"{BASE}{path}"
        suppress = kw.pop("suppress_error", False)
        resp = session.request(method, url, **kw)
        if not suppress and resp.status_code >= 400:
            fail(f"{method} {path} → {resp.status_code}: {resp.text[:200]}")
        return resp.json() if resp.text else {}

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 2: 创建项目和数据集
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 2: 创建项目和数据集")

    proj = api_json("POST", "/api/projects",
                    json={"name": f"比较Demo-{uuid.uuid4().hex[:8]}",
                          "description": "实验对比验证"})
    project_id = proj["id"]
    log("Project ID", project_id)

    ds = api_json("POST", f"/api/projects/{project_id}/datasets",
                  json={"name": "demo-compare-ds", "description": "实验对比数据集"})
    dataset_id = ds["id"]
    log("Dataset ID", dataset_id)

    # 创建 3 个用例: simple, knowledge, reasoning
    case_inputs = [
        {"query": "1+1=?", "category": "simple", "expected_answer": "2"},
        {"query": "法国的首都是什么？", "category": "knowledge", "expected_answer": "巴黎"},
        {"query": "如果明天下雨，公园还会开放吗？", "category": "reasoning", "expected_answer": "视情况而定"},
    ]
    case_ids = []
    for ci in case_inputs:
        case = api_json("POST", f"/api/datasets/{dataset_id}/cases",
                        json={"input": json.dumps(ci),
                              "expected": json.dumps(ci["expected_answer"])})
        case_ids.append(case["id"])
        log(f"Case {len(case_ids)}", f"{ci['category']} → {case['id'][:8]}...")
    check(len(case_ids) == 3, f"创建了 {len(case_ids)} 个用例")

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 3: 创建 project token
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 3: 创建 Project Token")

    tok = api_json("POST", f"/api/projects/{project_id}/tokens",
                   json={"description": "demo-token",
                         "scopes": ["runner:write", "agent:runtime",
                                    "agent:register", "otlp:write"]})
    plain_token = tok.get("plain_token", "")
    if not plain_token:
        fail("无法获取 plain_token")
    token_prefix = tok.get("token", {}).get("token_prefix", "")
    log("Token", f"已创建 (prefix={token_prefix})")

    # ── 注册 Agent ────────────────────────────────────────────────────
    _rt = agent_init(
        server_url=BASE,
        project_token=plain_token,
        agent_name="demo-agent",
        description="实验对比 Demo Agent",
        metadata={"demo": "experiment-comparison"},
        heartbeat_interval=120,
    )
    agent_id = _rt.agent_id
    instance_id = _rt.instance_id
    log("Agent ID", agent_id)
    log("Instance ID", instance_id)

    # ── 初始化 RunnerClient ────────────────────────────────────────────
    runner_client = RunnerClient(
        server_url=BASE,
        project_token=plain_token,
        runner_name="demo-runner",
    )

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 4: 创建 Baseline Experiment
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 4: 创建 Baseline 实验")

    bl_exp = api_json("POST", f"/api/projects/{project_id}/experiments",
                      json={
                          "dataset_id": dataset_id,
                          "target_name": "deterministic-agent",
                          "target_version": "v1.0",
                          "case_ids": case_ids,
                          "model_provider": "openai",
                          "model_name": "gpt-4",
                          "model_version": "0613",
                          "prompt_version": "prompt-v1",
                          "harness_version": "1.0.0",
                          "dataset_version": "ds-v1",
                          "toolset_version": "tools-v1",
                          "skillset_version": "skills-v1",
                          "rag_config_version": "rag-v1",
                          "feature_flags": json.dumps({"tool_retry": False}),
                      })
    bl_exp_id = bl_exp["id"]
    log("Baseline Experiment ID", bl_exp_id)
    log("Target", "deterministic-agent v1.0")

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 5: 执行 Baseline 评测
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 5: 执行 Baseline 评测")

    # Baseline: case A fail (0.0), case B pass (1.0), case C pass (1.0)
    bl_configs = {
        case_ids[0]: {"score": 0.0, "passed": False, "reason": "确定性的错误回答",
                      "output": json.dumps({"answer": "42"}), "eval": [
                          {"metric_name": "accuracy", "metric_kind": "final_answer",
                           "score": 0.0, "passed": False, "reason": "答案应为2"}]},
        case_ids[1]: {"score": 1.0, "passed": True, "reason": "确定性的正确回答",
                      "output": json.dumps({"answer": "巴黎"}), "eval": [
                          {"metric_name": "accuracy", "metric_kind": "final_answer",
                           "score": 1.0, "passed": True, "reason": "正确"}]},
        case_ids[2]: {"score": 1.0, "passed": True, "reason": "确定性的正确回答",
                      "output": json.dumps({"answer": "视情况而定"}), "eval": [
                          {"metric_name": "accuracy", "metric_kind": "final_answer",
                           "score": 1.0, "passed": True, "reason": "正确"}]},
    }

    # 配置 tracing（idempotent，后续调用复用缓存）
    tracer = configure_tracing(
        server_url=BASE,
        project_token=plain_token,
        service_name="experiment-comparison-demo",
        capture_content=True,
        export_batch_size=1,  # 同步导出
    )
    log("Tracing 已配置")

    processed_bl = 0
    for attempt in range(15):
        claimed = runner_client.claim_job()
        if claimed is None:
            if processed_bl == 0 and attempt < 5:
                time.sleep(1)
                continue
            if processed_bl >= 3:
                break
            time.sleep(1)
            continue

        case_id = claimed.dataset_case_id
        cfg = bl_configs.get(case_id)
        if cfg is None:
            runner_client.fail_job(claimed.id, "config_error",
                                   f"No config for case {case_id}")
            continue

        # 创建 OTLP 根 span（自动生成 trace_id，同步导出到 ClickHouse）
        with tracer.start_as_current_span("agent.run") as root_span:
            span_ctx = root_span.get_span_context()
            actual_trace_id = format(span_ctx.trace_id, "032x")
            root_span.set_attribute("agentscope.run.id", "")
            root_span.set_attribute("agentscope.agent.name", "deterministic-agent")
            root_span.set_attribute("agentscope.agent.version", "v1.0")
            root_span.set_attribute("agentscope.eval.job_id", claimed.id)
            root_span.set_attribute("agentscope.dataset.case_id", case_id)

            # 创建 Run（关联 trace_id）
            run = start_run(
                input=claimed.case_input,
                trace_id=actual_trace_id,
            )
            if not run or not run.id:
                fail("start_run 返回空，无法继续")

            run_id = run.id
            root_span.set_attribute("agentscope.run.id", run_id)
            log(f"Baseline Case {case_id[:8]}...", f"run={run_id[:8]}..., trace={actual_trace_id[:16]}...")

        # span 已结束，SimpleSpanProcessor 自动同步导出

        # 提交结果（含 eval_scores）
        result = JobResultPayload(
            run_id=run_id,
            score=cfg["score"],
            passed=cfg["passed"],
            reason=cfg["reason"],
            actual_output=cfg["output"],
            latency_ms=150 + processed_bl * 50,
            cost=0.01 + processed_bl * 0.005,
            eval_scores=cfg["eval"],
        )
        runner_client.submit_result(claimed.id, result)
        processed_bl += 1
        log(f"  → job {claimed.id[:8]}... submitted (score={cfg['score']}, passed={cfg['passed']})")

    check(processed_bl == 3, f"Baseline 完成 {processed_bl} 个 job")

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 6: 创建 Candidate Experiment
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 6: 创建 Candidate 实验")

    cand_exp = api_json("POST", f"/api/projects/{project_id}/experiments",
                        json={
                            "dataset_id": dataset_id,
                            "target_name": "deterministic-agent",
                            "target_version": "v2.0",
                            "case_ids": case_ids,
                            "model_provider": "anthropic",
                            "model_name": "claude-sonnet-4",
                            "model_version": "20261001",
                            "prompt_version": "prompt-v2",
                            "harness_version": "2.0.0",
                            "dataset_version": "ds-v1",
                            "toolset_version": "tools-v2",
                            "skillset_version": "skills-v2",
                            "rag_config_version": "rag-v2",
                            "feature_flags": json.dumps({"tool_retry": True, "use_rag": True}),
                        })
    cand_exp_id = cand_exp["id"]
    log("Candidate Experiment ID", cand_exp_id)
    log("Target", "deterministic-agent v2.0 (model: claude-sonnet-4)")

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 7: 执行 Candidate 评测
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 7: 执行 Candidate 评测")

    # Candidate: case A pass/improved (0.0→0.9), case B pass/unchanged (1.0→1.0),
    #            case C fail/regressed (1.0→0.0) + safety failed
    cand_configs = {
        case_ids[0]: {"score": 0.9, "passed": True, "reason": "候选版本正确回答了一加一问题",
                      "output": json.dumps({"answer": "2"}), "eval": [
                          {"metric_name": "accuracy", "metric_kind": "final_answer",
                           "score": 0.9, "passed": True, "reason": "答案正确"},
                          {"metric_name": "safety_check", "metric_kind": "safety",
                           "score": 1.0, "passed": True, "reason": "安全通过"},
                          {"metric_name": "tool_correctness", "metric_kind": "tool_correctness",
                           "score": 0.8, "passed": True, "reason": "工具调用正确"},
                          {"metric_name": "latency_score", "metric_kind": "efficiency",
                           "score": 0.7, "passed": True, "reason": "延迟可接受"}]},
        case_ids[1]: {"score": 1.0, "passed": True, "reason": "两个版本都正确",
                      "output": json.dumps({"answer": "巴黎"}), "eval": [
                          {"metric_name": "accuracy", "metric_kind": "final_answer",
                           "score": 1.0, "passed": True, "reason": "答案正确"},
                          {"metric_name": "safety_check", "metric_kind": "safety",
                           "score": 1.0, "passed": True, "reason": "安全通过"},
                          {"metric_name": "tool_correctness", "metric_kind": "tool_correctness",
                           "score": 1.0, "passed": True, "reason": "工具调用正确"},
                          {"metric_name": "latency_score", "metric_kind": "efficiency",
                           "score": 0.8, "passed": True, "reason": "延迟可接受"}]},
        case_ids[2]: {"score": 0.0, "passed": False, "reason": "候选版本推理错误",
                      "output": json.dumps({"answer": "错误推理"}), "eval": [
                          {"metric_name": "accuracy", "metric_kind": "final_answer",
                           "score": 0.0, "passed": False, "reason": "答案错误"},
                          {"metric_name": "safety_check", "metric_kind": "safety",
                           "score": 0.0, "passed": False, "reason": "内容安全检测失败"},
                          {"metric_name": "tool_correctness", "metric_kind": "tool_correctness",
                           "score": 0.3, "passed": False, "reason": "工具调用错误"},
                          {"metric_name": "latency_score", "metric_kind": "efficiency",
                           "score": 0.5, "passed": False, "reason": "延迟偏高"}]},
    }

    processed_cand = 0
    for attempt in range(15):
        claimed = runner_client.claim_job()
        if claimed is None:
            if processed_cand == 0 and attempt < 5:
                time.sleep(1)
                continue
            if processed_cand >= 3:
                break
            time.sleep(1)
            continue

        case_id = claimed.dataset_case_id
        cfg = cand_configs.get(case_id)
        if cfg is None:
            runner_client.fail_job(claimed.id, "config_error",
                                   f"No config for case {case_id}")
            continue

        # 创建 OTLP 根 span 和 Run（与 baseline 相同模式）
        with tracer.start_as_current_span("agent.run") as root_span:
            span_ctx = root_span.get_span_context()
            actual_trace_id = format(span_ctx.trace_id, "032x")
            root_span.set_attribute("agentscope.agent.name", "deterministic-agent")
            root_span.set_attribute("agentscope.agent.version", "v2.0")
            root_span.set_attribute("agentscope.eval.job_id", claimed.id)
            root_span.set_attribute("agentscope.dataset.case_id", case_id)

            run = start_run(
                input=claimed.case_input,
                trace_id=actual_trace_id,
            )
            if not run or not run.id:
                fail("start_run 返回空，无法继续")

            run_id = run.id
            root_span.set_attribute("agentscope.run.id", run_id)
            log(f"Candidate Case {case_id[:8]}...", f"run={run_id[:8]}..., trace={actual_trace_id[:16]}...")

        # 提交结果（含 eval_scores 和 safety metric）
        result = JobResultPayload(
            run_id=run_id,
            score=cfg["score"],
            passed=cfg["passed"],
            reason=cfg["reason"],
            actual_output=cfg["output"],
            latency_ms=200 + processed_cand * 100,
            cost=0.02 + processed_cand * 0.01,
            eval_scores=cfg["eval"],
        )
        runner_client.submit_result(claimed.id, result)
        processed_cand += 1
        log(f"  → job {claimed.id[:8]}... submitted (score={cfg['score']}, passed={cfg['passed']})")

    check(processed_cand == 3, f"Candidate 完成 {processed_cand} 个 job")

    # 等待 ClickHouse 异步 ingestion 完成
    time.sleep(2)

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 8: 创建 Comparison（含回归 Gates）
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 8: 创建实验对比（含回归 Gate + Safety Veto）")

    gates = {
        "min_pass_rate_delta": 0.2,
        "min_score_delta": 0.2,
        "max_cost_increase_ratio": 0.5,
        "max_p95_latency_inc_pct": 50,
        "safety_veto_enabled": True,
    }

    comparison = api_json("POST", f"/api/projects/{project_id}/comparisons",
                          json={
                              "baseline_experiment_id": bl_exp_id,
                              "candidate_experiment_id": cand_exp_id,
                              "comparison_type": "general",
                              "regression_gates": json.dumps(gates),
                          })
    comparison_id = comparison.get("id", "")
    log("Comparison ID", comparison_id)

    summary = comparison.get("comparison_summary", {})
    log("Total cases", str(summary.get("total_cases")))
    log("Paired cases", str(summary.get("paired_cases")))
    log("Improved", str(summary.get("improved_cases")))
    log("Regressed", str(summary.get("regressed_cases")))
    log("Unchanged", str(summary.get("unchanged_cases")))

    # ── 断言 ──────────────────────────────────────────────────────────
    check(summary.get("improved_cases") == 1, "improved == 1")
    check(summary.get("regressed_cases") == 1, "regressed == 1")
    check(summary.get("unchanged_cases") == 1, "unchanged == 1")
    check(summary.get("paired_cases") == 3, "paired == 3")

    # Regression gate 应触发（safety veto + pass rate 下降）
    check(summary.get("regression_triggered") is True, "regression_triggered == True")

    reasons = summary.get("regression_reasons", [])
    has_safety_veto = any("safety" in r.lower() or "veto" in r.lower() for r in reasons)
    # 如果 safety veto 未出现在 reasons 中，至少应有其他原因（如 pass rate 下降）
    if not has_safety_veto:
        log("⚠ Safety veto 原因未显式包含，检查 regression_reasons:", str(reasons))
        check(len(reasons) > 0, "regression_reasons 非空")

    # 验证 SafetyRegressed
    check(summary.get("safety_regressed") is True, "safety_regressed == True")

    # ── 验证多维度指标（Multi-Dim Metrics）────────────────────────────────
    multi_dim = summary.get("multi_dim_metrics", {})
    log("Multi-dim metrics", str(list(multi_dim.keys())))

    # 断言必须有预期的 metric key（metric_kind:metric_name），不是只按 kind 聚合
    required_keys = {
        "final_answer:accuracy",
        "safety:safety_check",
        "tool_correctness:tool_correctness",
        "efficiency:latency_score",
    }
    actual_keys = set(multi_dim.keys())
    for key in required_keys:
        check(key in actual_keys, f"多维度指标包含 '{key}'")

    # 验证每个 entry 都有 metric_name 和 metric_kind
    for key, val in multi_dim.items():
        check("metric_name" in val, f"指标 '{key}' 包含 metric_name")
        check("metric_kind" in val, f"指标 '{key}' 包含 metric_kind")

    # 验证 specific dimension values
    if "final_answer:accuracy" in multi_dim:
        acc = multi_dim["final_answer:accuracy"]
        log("  final_answer:accuracy",
            f"name={acc.get('metric_name')} kind={acc.get('metric_kind')} "
            f"BL={acc.get('baseline_avg')} CAND={acc.get('candidate_avg')} delta={acc.get('delta')}")
        check(acc.get("baseline_total_count", 0) == 3,
              f"final_answer:accuracy baseline_total_count=3 (got {acc.get('baseline_total_count')})")
        check(acc.get("candidate_total_count", 0) == 3,
              f"final_answer:accuracy candidate_total_count=3 (got {acc.get('candidate_total_count')})")

    if "safety:safety_check" in multi_dim:
        sf = multi_dim["safety:safety_check"]
        log("  safety:safety_check",
            f"name={sf.get('metric_name')} kind={sf.get('metric_kind')} "
            f"BL={sf.get('baseline_avg')} CAND={sf.get('candidate_avg')} delta={sf.get('delta')}")
        check(sf.get("baseline_total_count", 0) == 0,
              f"safety:safety_check baseline_total_count=0 (无 baseline safety, got {sf.get('baseline_total_count')})")
        check(sf.get("candidate_total_count", 0) == 3,
              f"safety:safety_check candidate_total_count=3 (got {sf.get('candidate_total_count')})")

    if "efficiency:latency_score" in multi_dim:
        eff = multi_dim["efficiency:latency_score"]
        log("  efficiency:latency_score",
            f"name={eff.get('metric_name')} kind={eff.get('metric_kind')} "
            f"BL={eff.get('baseline_avg')} CAND={eff.get('candidate_avg')} delta={eff.get('delta')}")

    if "tool_correctness:tool_correctness" in multi_dim:
        tc = multi_dim["tool_correctness:tool_correctness"]
        log("  tool_correctness:tool_correctness",
            f"name={tc.get('metric_name')} kind={tc.get('metric_kind')} "
            f"BL={tc.get('baseline_avg')} CAND={tc.get('candidate_avg')} delta={tc.get('delta')}")

    # ── 验证详情 ───────────────────────────────────────────────────────
    print_separator("验证: Comparison 详情 API")
    comp_detail = api_json("GET", f"/api/comparisons/{comparison_id}")
    log("Comparison type", comp_detail.get("comparison_type"))
    log("Baseline exp", comp_detail.get("baseline_experiment_id", "")[:8] + "...")
    log("Candidate exp", comp_detail.get("candidate_experiment_id", "")[:8] + "...")

    # 验证 case_results
    case_results = comp_detail.get("case_results", [])
    check(len(case_results) == 3, f"case_results 数量 = 3 (实际 {len(case_results)})")
    for cr in case_results:
        log(f"  Case {cr['dataset_case_id'][:8]}...",
            f"status={cr['status']}, bl={cr.get('baseline_score', 'N/A')}→cand={cr.get('candidate_score', 'N/A')}")

    # ── 验证 Harness 元数据 ────────────────────────────────────────────
    print_separator("验证: 实验 Harness 元数据")
    bl_detail = api_json("GET", f"/api/experiments/{bl_exp_id}")
    log("Baseline model", bl_detail.get("model_name", "N/A"))
    log("Baseline prompt_ver", bl_detail.get("prompt_version", "N/A"))
    log("Baseline harness_ver", bl_detail.get("harness_version", "N/A"))
    log("Baseline toolset_ver", bl_detail.get("toolset_version", "N/A"))
    log("Baseline skillset_ver", bl_detail.get("skillset_version", "N/A"))
    log("Baseline rag_config_ver", bl_detail.get("rag_config_version", "N/A"))
    check(bl_detail.get("toolset_version") == "tools-v1",
          f"Baseline toolset_version 正确 ({bl_detail.get('toolset_version', 'N/A')})")
    check(bl_detail.get("skillset_version") == "skills-v1",
          f"Baseline skillset_version 正确 ({bl_detail.get('skillset_version', 'N/A')})")

    cand_detail = api_json("GET", f"/api/experiments/{cand_exp_id}")
    check(cand_detail.get("model_name") == "claude-sonnet-4",
          f"Candidate model_name 正确 ({cand_detail.get('model_name', 'N/A')})")
    check(cand_detail.get("harness_version") == "2.0.0",
          f"Candidate harness_version 正确 ({cand_detail.get('harness_version', 'N/A')})")

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 9: 从回归 Case 创建 Annotation Item
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 9: 从回归 Candidate Run 创建 Annotation Item")

    # 找到回归 case 的 candidate_run_id
    regressed_case = None
    for cr in case_results:
        if cr.get("status") == "regressed":
            regressed_case = cr
            break
    check(regressed_case is not None, "找到回归 case")

    regressed_run_id = regressed_case.get("candidate_run_id", "")
    check(regressed_run_id != "", "回归 case 的 candidate_run_id 非空")

    # 获取或创建 annotation queue
    qs = api_json("GET", f"/api/projects/{project_id}/annotation-queues")
    queue_id = None
    for q in qs:
        if q["name"] == "回归检测队列":
            queue_id = q["id"]
            break
    if not queue_id:
        qd = api_json("POST", f"/api/projects/{project_id}/annotation-queues",
                       json={"name": "回归检测队列", "description": "用于回归 case 的标注队列"})
        queue_id = qd["id"]
    log("Annotation Queue ID", queue_id)

    # 从回归 run 创建 annotation item
    annot_resp = session.post(
        f"{BASE}/api/projects/{project_id}/runs/{regressed_run_id}/annotation-items",
        json={"queue_id": queue_id},
        headers={"Content-Type": "application/json"},
        timeout=30,
    )
    if annot_resp.status_code >= 400:
        fail(f"Annotation Item 创建失败: {annot_resp.status_code} {annot_resp.text[:200]}")
    annot_data = annot_resp.json()
    annot_item_id = annot_data.get("id", "")
    check(annot_item_id != "", "annotation item id 非空")
    log("Annotation Item ID", annot_item_id)
    log("  run_id", annot_data.get("run_id", ""))
    log("  trace_id", annot_data.get("trace_id", ""))

    # 验证 source_run_id 和 source_trace_id
    check(annot_data.get("run_id") == regressed_run_id,
          "annotation_item.run_id 匹配回归 run_id")

    # ═══════════════════════════════════════════════════════════════════
    #  步骤 10: PostgreSQL 和 ClickHouse 数据验证
    # ═══════════════════════════════════════════════════════════════════
    print_separator("步骤 10: 数据库数据验证")

    db_errors = []

    # 收集所有 run_ids（含 trace_id）
    run_trace_pairs = []
    all_run_ids = set()
    for cr in case_results:
        if cr.get("baseline_run_id"):
            all_run_ids.add(cr["baseline_run_id"])
        if cr.get("candidate_run_id"):
            all_run_ids.add(cr["candidate_run_id"])

    # 通过 API 验证 run 详情，收集 (run_id, trace_id) 对
    log("Runs:", f"{len(all_run_ids)} 个")
    for rid in sorted(all_run_ids):
        rd = api_json("GET", f"/api/runs/{rid}")
        run_info = rd.get("run", rd)
        trace_id = run_info.get("trace_id", "")
        if trace_id:
            run_trace_pairs.append((rid, trace_id))
            log(f"  Run {rid[:8]}...", f"trace_id={trace_id[:16]}...")
        else:
            db_errors.append(f"Run {rid[:8]}... 缺少 trace_id")

    if run_trace_pairs:
        log("Traces:", f"{len(run_trace_pairs)} 个唯一 trace_id")
    else:
        db_errors.append("未收集到任何 trace_id")

    # ── 直接数据库验证 ────────────────────────────────────────────────
    use_db = False
    if args.skip_db_check:
        log("DB 验证", "已跳过数据库验证")
    else:
        try:
            import psycopg2
            use_db = True
        except ImportError as e:
            db_errors.append(f"数据库依赖不可用: {e}。使用 --skip-db-check 可跳过此检查。")

    if not args.skip_db_check and use_db:
        pg_host = os.environ.get("PGHOST", "localhost")
        pg_port = os.environ.get("PGPORT", "15432")
        pg_db = os.environ.get("PGDATABASE", "agentscope_test")
        pg_user = os.environ.get("PGUSER", "agentscope")
        pg_pass = os.environ.get("PGPASSWORD", "agentscope")

        # PostgreSQL 验证
        try:
            pg_conn = psycopg2.connect(
                host=pg_host, port=pg_port, dbname=pg_db,
                user=pg_user, password=pg_pass,
            )
            pg_cur = pg_conn.cursor()

            # 1. 验证 experiment 全部 Harness 列
            pg_cur.execute("""
                SELECT toolset_version, skillset_version, rag_config_version
                FROM experiments WHERE id = %s
            """, (cand_exp_id,))
            row = pg_cur.fetchone()
            if row:
                check(row[0] == "tools-v2", f"candidate toolset_version (PG) = {row[0]}")
                check(row[1] == "skills-v2", f"candidate skillset_version (PG) = {row[1]}")
                check(row[2] == "rag-v2", f"candidate rag_config_version (PG) = {row[2]}")
            else:
                db_errors.append("experiments 表中未找到 candidate experiment")

            # 2. 验证 experiment_comparisons
            pg_cur.execute("""
                SELECT id, baseline_experiment_id, candidate_experiment_id,
                       comparison_type, status
                FROM experiment_comparisons
                WHERE id = %s
            """, (comparison_id,))
            row = pg_cur.fetchone()
            if row:
                check(row[1] == bl_exp_id, "comparison baseline_exp_id 匹配")
                check(row[2] == cand_exp_id, "comparison candidate_exp_id 匹配")
                check(row[3] == "general", "comparison type = general")
                check(row[4] == "completed", "comparison status = completed")
            else:
                db_errors.append("experiment_comparisons 中未找到 comparison")

            # 3. 验证 comparison_case_results
            pg_cur.execute("""
                SELECT dataset_case_id, baseline_run_id, candidate_run_id,
                       status, score_diff
                FROM comparison_case_results
                WHERE comparison_id = %s
                ORDER BY created_at
            """, (comparison_id,))
            pg_rows = pg_cur.fetchall()
            if len(pg_rows) >= 3:
                log("comparison_case_results", f"{len(pg_rows)} 条")
                for r in pg_rows:
                    log(f"  case={r[0][:8]}...", f"status={r[3]}, score_diff={r[4]}")
            else:
                db_errors.append(f"comparison_case_results 应 >=3 条，实际 {len(pg_rows)}")

            # 4. 精确验证 runs（project_id + trace_id + run_id 一致性）
            for rid in sorted(all_run_ids):
                pg_cur.execute("""
                    SELECT id, trace_id, project_id, status
                    FROM runs WHERE id = %s
                """, (rid,))
                row = pg_cur.fetchone()
                if row:
                    log(f"  PG Run {rid[:8]}...", f"trace={row[1][:16] if row[1] else 'N/A'}..." if row[1] else "no trace")
                    check(row[2] == project_id, f"Run {rid[:8]}... 属于正确项目 (project_id)")
                    check(row[1] is not None and row[1] != "", f"Run {rid[:8]}... 有非空 trace_id")
                    # trace_id 必须在 ClickHouse trace 集合中
                    if row[1]:
                        all_trace_ids = {t for _, t in run_trace_pairs}
                        matching_trace = [t for t in all_trace_ids if t == row[1]]
                        check(len(matching_trace) == 1,
                              f"Run {rid[:8]}... 的 trace_id 在已收集的 all_trace_ids 中")
                else:
                    db_errors.append(f"runs 表中未找到 run {rid[:8]}")

            # 5. 验证 annotation_items（如果创建成功）
            if annot_item_id:
                pg_cur.execute("""
                    SELECT id, run_id, trace_id FROM annotation_items
                    WHERE run_id = %s
                """, (regressed_run_id,))
                row = pg_cur.fetchone()
                if row:
                    check(row[0] == annot_item_id, "annotation_items 中找到匹配记录")
                    check(row[1] == regressed_run_id, "annotation_item.run_id 匹配")
                else:
                    db_errors.append("annotation_items 中未找到回归 run 的记录")

            pg_cur.close()
            pg_conn.close()
            log("PostgreSQL 验证", "通过")
        except Exception as e:
            db_errors.append(f"PostgreSQL 验证异常: {e}")

        # ClickHouse 验证 —— 对每个 PostgreSQL run 查询 CH 验证 run_id + trace_id + agent.run span
        try:
            ch_ok = True
            for rid, pg_trace_id in run_trace_pairs:
                try:
                    result = subprocess.run(
                        ["docker", "compose", "-p", "phase3-6-experiment-comparison",
                         "exec", "-T", "clickhouse",
                         "clickhouse-client", "-q",
                         f"SELECT count() FROM agentscope_traces.trace_spans WHERE trace_id = '{pg_trace_id}' AND run_id = '{rid}' AND name = 'agent.run'"],
                        capture_output=True, text=True, timeout=10,
                    )
                    if result.returncode == 0:
                        count = result.stdout.strip()
                        if count and int(count) > 0:
                            log(f"  CH run={rid[:8]}...", f"trace={pg_trace_id[:16]}... agent.run spans={count}")
                        else:
                            db_errors.append(
                                f"ClickHouse run={rid[:8]}... trace={pg_trace_id[:16]}... 缺少 agent.run span"
                            )
                            ch_ok = False
                    else:
                        db_errors.append(
                            f"ClickHouse query error for run={rid[:8]}...: {result.stderr[:200]}"
                        )
                        ch_ok = False
                except Exception as e2:
                    db_errors.append(f"ClickHouse docker exec failed for {rid[:8]}...: {e2}")
                    ch_ok = False
            if ch_ok:
                log("ClickHouse 验证", "通过")
        except Exception as e:
            db_errors.append(f"ClickHouse 验证异常: {e}")

    # ── 结果 ──────────────────────────────────────────────────────────
    print_separator("结果摘要")
    log("User ID", user_id)
    log("Project ID", project_id)
    log("Dataset ID", dataset_id)
    log("Baseline Experiment", bl_exp_id)
    log("Candidate Experiment", cand_exp_id)
    log("Comparison ID", comparison_id)
    log("Improved cases", str(summary.get("improved_cases")))
    log("Regressed cases", str(summary.get("regressed_cases")))
    log("Unchanged cases", str(summary.get("unchanged_cases")))
    log("Safety regressed", str(summary.get("safety_regressed")))
    log("Regression triggered", str(summary.get("regression_triggered")))

    # 前端链接
    fe = BASE.replace(":8080", ":5173") if ":8080" in BASE else BASE
    log("")
    log("前端链接")
    log("  Comparison", f"{fe}/comparisons/{comparison_id}")
    log("  Baseline Exp", f"{fe}/experiments/{bl_exp_id}")
    log("  Candidate Exp", f"{fe}/experiments/{cand_exp_id}")

    if db_errors:
        print(f"\n❌ 数据库验证发现 {len(db_errors)} 个错误:")
        for e in db_errors:
            print(f"   - {e}")
        sys.exit(1)
    elif args.skip_db_check:
        pass  # 已跳过数据库验证，不输出"通过"
    elif not use_db:
        print("\n⚠ 数据库验证跳过（缺少依赖或 DB 连接参数），仅通过 API 验证")
    else:
        print("\n✅ PostgreSQL 和 ClickHouse 数据完整性验证通过")

    if not db_errors:
        print("\n✅ 实验对比闭环 Demo 全部通过！")
    else:
        print(f"\n❌ Demo 完成但有 {len(db_errors)} 个验证失败")
        sys.exit(1)


if __name__ == "__main__":
    main()
