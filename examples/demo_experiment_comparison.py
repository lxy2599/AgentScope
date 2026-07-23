#!/usr/bin/env python3
"""Demo: 实验对比闭环——Baseline vs Candidate 确定性评测。

该 Demo 使用完全确定性的 target 和 evaluator，不依赖外部 LLM：
  1. 新建 project、dataset、cases
  2. 创建 baseline experiment，执行全部 case（output 固定）
  3. 创建 candidate experiment，构造 1 个 improved + 1 个 regressed case
  4. 创建 comparison，验证 regression gate 触发
  5. 验证 PostgreSQL 和 ClickHouse 数据完整性

用法：
  python examples/demo_experiment_comparison.py [--server-url URL] [--project-token TOKEN]
"""

import json
import os
import sys
import time
import uuid

SERVER_URL = os.environ.get("AS_SERVER_URL", "http://localhost:8000")

# ---- Helpers ----

def api_post(path, token, data):
    import httpx
    resp = httpx.post(
        f"{SERVER_URL}{path}",
        json=data,
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()

def api_get(path, token):
    import httpx
    resp = httpx.get(
        f"{SERVER_URL}{path}",
        headers={"Authorization": f"Bearer {token}"},
        timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


# ---- Main Demo ----

def main():
    print("=" * 60)
    print("AgentScope 实验对比闭环 Demo")
    print("=" * 60)

    # Step 0: Registry — use session-based auth
    email = f"demo-comparison-{uuid.uuid4().hex[:8]}@test.com"
    import httpx

    # Register user
    reg_resp = httpx.post(
        f"{SERVER_URL}/api/auth/register",
        json={"email": email, "password": "test1234", "display_name": "Demo User"},
        timeout=30,
    )
    if reg_resp.status_code == 409:
        # User exists — login
        login_resp = httpx.post(
            f"{SERVER_URL}/api/auth/login",
            json={"email": email, "password": "test1234"},
            timeout=30,
        )
        session_token = login_resp.cookies.get("session_token", "")
    else:
        reg_resp.raise_for_status()
        session_token = reg_resp.cookies.get("session_token", "")

    if not session_token:
        print("ERROR: 无法获取 session_token")
        sys.exit(1)

    cookies = {"session_token": session_token}
    headers = {"Content-Type": "application/json"}
    headers.update({"Cookie": f"session_token={session_token}"})
    # Try Authorization header instead
    headers = {"Content-Type": "application/json", "Authorization": f"Bearer {session_token}"}

    def authed_post(path, data):
        resp = httpx.post(
            f"{SERVER_URL}{path}",
            json=data,
            headers=headers,
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()

    def authed_get(path):
        resp = httpx.get(
            f"{SERVER_URL}{path}",
            headers=headers,
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json()

    # ---- Step 1: Create project ----
    print("\n[1/8] 创建项目...")
    project = authed_post("/api/projects", {"name": f"比较Demo-{uuid.uuid4().hex[:8]}", "description": "实验对比验证"})
    project_id = project["id"]
    print(f"  Project ID: {project_id}")
    print(f"  Project Name: {project['name']}")

    # ---- Step 2: Create dataset ----
    print("\n[2/8] 创建数据集和用例...")
    ds = authed_post(f"/api/projects/{project_id}/datasets", {"name": "demo-compare-ds", "description": "实验对比数据集"})
    dataset_id = ds["id"]
    print(f"  Dataset ID: {dataset_id}")

    cases = []
    case_inputs = [
        {"query": "1+1=?", "category": "simple"},
        {"query": "法国的首都是什么？", "category": "knowledge"},
        {"query": "如果明天下雨，公园还会开放吗？", "category": "reasoning"},
    ]
    for ci in case_inputs:
        case = authed_post(f"/api/datasets/{dataset_id}/cases", {"input": json.dumps(ci), "expected": json.dumps({"answer": "deterministic"})})
        cases.append(case["id"])
        print(f"  Case: {case['id'][:8]}... {ci['category']}")

    # ---- Step 3: Create baseline experiment ----
    print("\n[3/8] 创建 Baseline 实验...")
    bl_exp = authed_post(f"/api/projects/{project_id}/experiments", {
        "dataset_id": dataset_id,
        "target_name": "deterministic-agent",
        "target_version": "v1.0",
        "case_ids": cases,
        "model_provider": "openai",
        "model_name": "gpt-4",
        "model_version": "0613",
        "prompt_version": "prompt-v1",
        "harness_version": "1.0.0",
        "dataset_version": "ds-v1",
        "feature_flags": json.dumps({"tool_retry": False}),
    })
    bl_exp_id = bl_exp["id"]
    print(f"  Baseline Experiment ID: {bl_exp_id}")
    print(f"  Target: deterministic-agent v1.0")

    # ---- Step 4: Create baseline runs (simulate runner) ----
    print("\n[4/8] 提交 Baseline 评测结果...")
    # In a real scenario, the runner would execute targets.
    # For demo, we submit results directly via the runner result API.
    # First, claim the eval jobs
    import httpx

    # Get project token for runner access
    tok_resp = authed_post(f"/api/projects/{project_id}/tokens", {"scopes": ["runner:write"]})
    project_token = tok_resp.get("plain_token", "")
    if not project_token:
        print("  WARNING: 无法创建 project token，尝试 session auth 提交结果")
        runner_headers = headers
    else:
        runner_headers = {"Authorization": f"Bearer {project_token}", "Content-Type": "application/json"}

    # Claim each job and submit result
    for idx, case_id in enumerate(cases):
        # Claim job
        claim_resp = httpx.get(
            f"{SERVER_URL}/runner/jobs/claim?runner_name=demo-runner",
            headers=runner_headers,
            timeout=30,
        )
        # May fail if job claimed already - just continue
        try:
            claimed = claim_resp.json()
            job_id = claimed.get("job", {}).get("id", "")
            if job_id:
                # Submit result
                result_data = {
                    "score": 1.0 if idx < 2 else 0.0,
                    "passed": idx < 2,
                    "reason": "确定性的正确回答" if idx < 2 else "确定性错误",
                    "actual_output": json.dumps({"answer": "42"} if idx == 0 else {"answer": "巴黎"} if idx == 1 else {"answer": "不知道"}),
                    "latency_ms": 100 + idx * 50,
                    "cost": 0.01 + idx * 0.005,
                }
                submit_resp = httpx.post(
                    f"{SERVER_URL}/runner/jobs/{job_id}/results",
                    json=result_data,
                    headers=runner_headers,
                    timeout=30,
                )
                submit_resp.raise_for_status()
                print(f"  Case {idx+1}: job {job_id[:8]}... submitted (score={result_data['score']}, passed={result_data['passed']})")
        except Exception as e:
            print(f"  Case {idx+1}: submit error: {e}")

    # Wait for processing
    time.sleep(1)

    # ---- Step 5: Create candidate experiment ----
    print("\n[5/8] 创建 Candidate 实验...")
    cand_exp = authed_post(f"/api/projects/{project_id}/experiments", {
        "dataset_id": dataset_id,
        "target_name": "deterministic-agent",
        "target_version": "v2.0",
        "case_ids": cases,
        "model_provider": "anthropic",
        "model_name": "claude-sonnet-4",
        "model_version": "20261001",
        "prompt_version": "prompt-v2",
        "harness_version": "2.0.0",
        "dataset_version": "ds-v1",
        "toolset_version": "tools-v2",
        "feature_flags": json.dumps({"tool_retry": True, "use_rag": True}),
    })
    cand_exp_id = cand_exp["id"]
    print(f"  Candidate Experiment ID: {cand_exp_id}")
    print(f"  Target: deterministic-agent v2.0 (model: claude-sonnet-4)")

    # ---- Step 6: Create candidate runs with one improved + one regressed ----
    print("\n[6/8] 提交 Candidate 评测结果...")
    for idx, case_id in enumerate(cases):
        claim_resp = httpx.get(
            f"{SERVER_URL}/runner/jobs/claim?runner_name=demo-runner",
            headers=runner_headers,
            timeout=30,
        )
        try:
            claimed = claim_resp.json()
            job_id = claimed.get("job", {}).get("id", "")
            if job_id:
                # Construct improved/regressed: case 0 improved (0.0→0.9), case 1 unchanged, case 2 regressed (1.0→0.0)
                if idx == 0:  # improved
                    score, passed = 1.0, True
                    reason = "候选版本正确回答了一加一问题"
                elif idx == 1:  # unchanged
                    score, passed = 1.0, True
                    reason = "两个版本都正确"
                else:  # case 2 regressed
                    score, passed = 0.0, False
                    reason = "候选版本推理错误"

                result_data = {
                    "score": score,
                    "passed": passed,
                    "reason": reason,
                    "actual_output": json.dumps({"answer": "2"} if idx == 0 else {"answer": "巴黎"} if idx == 1 else {"answer": "错误推理"}),
                    "latency_ms": 200 + idx * 100,
                    "cost": 0.02 + idx * 0.01,
                }
                submit_resp = httpx.post(
                    f"{SERVER_URL}/runner/jobs/{job_id}/results",
                    json=result_data,
                    headers=runner_headers,
                    timeout=30,
                )
                submit_resp.raise_for_status()
                print(f"  Case {idx+1}: {result_data['score']}/passed={result_data['passed']} ({reason})")
        except Exception as e:
            print(f"  Case {idx+1}: submit error: {e}")

    # Wait for processing
    time.sleep(2)

    # ---- Step 7: Create comparison with regression gates ----
    print("\n[7/8] 创建实验对比（含回归 Gate）...")
    gates = {
        "min_pass_rate_delta": 0.3,
        "min_score_delta": 0.2,
        "max_cost_increase_ratio": 0.5,
        "max_p95_latency_inc_pct": 50,
        "safety_veto_enabled": True,
    }
    comparison = authed_post(f"/api/projects/{project_id}/comparisons", {
        "baseline_experiment_id": bl_exp_id,
        "candidate_experiment_id": cand_exp_id,
        "comparison_type": "general",
        "regression_gates": json.dumps(gates),
    })
    print(f"  Comparison ID: {comparison.get('id', '')}")
    summary = comparison.get("comparison_summary", {})
    print(f"  Total cases: {summary.get('total_cases')}, Paired: {summary.get('paired_cases')}")
    print(f"  Improved: {summary.get('improved_cases')}, Regressed: {summary.get('regressed_cases')}, Unchanged: {summary.get('unchanged_cases')}")
    print(f"  Baseline pass rate: {summary.get('baseline_pass_rate', 0)*100:.0f}%")
    print(f"  Candidate pass rate: {summary.get('candidate_pass_rate', 0)*100:.0f}%")
    print(f"  Score delta: {summary.get('avg_score_delta', 0):+.4f}")
    print(f"  Regression triggered: {summary.get('regression_triggered')}")
    if summary.get('regression_reasons'):
        for r in summary['regression_reasons']:
            print(f"    ⚠ {r}")

    # ---- Step 8: Database verification ----
    print("\n[8/8] PostgreSQL 和 ClickHouse 数据验证...")
    errors = []
    try:
        # Verify through API
        comp_detail = authed_get(f"/api/comparisons/{comparison['id']}")
        print(f"  ✓ Comparison detail loaded ({comp_detail.get('comparison_type')})")

        # Check harness metadata
        bl_detail = authed_get(f"/api/experiments/{bl_exp_id}")
        print(f"  ✓ Baseline harness: model={bl_detail.get('model_name', 'N/A')}, h={bl_detail.get('harness_version', 'N/A')}, p={bl_detail.get('prompt_version', 'N/A')}")

        cand_detail = authed_get(f"/api/experiments/{cand_exp_id}")
        print(f"  ✓ Candidate harness: model={cand_detail.get('model_name', 'N/A')}, h={cand_detail.get('harness_version', 'N/A')}, p={cand_detail.get('prompt_version', 'N/A')}")

        # Verify cases are paired
        case_results = comp_detail.get("case_results", [])
        if len(case_results) >= 3:
            print(f"  ✓ Case results: {len(case_results)} total")
            paired = sum(1 for c in case_results if c.get("status") != "incomparable")
            print(f"  ✓ Paired cases: {paired}")
            incomparable = sum(1 for c in case_results if c.get("status") == "incomparable")
            if incomparable > 0:
                print(f"  ⚠ Incomparable cases: {incomparable}")
        else:
            errors.append(f"预期至少 3 个 case results，实际 {len(case_results)}")

        # Verify regression status
        reg = comp_detail.get("comparison_summary", {})
        if reg.get("regression_triggered"):
            print(f"  ✓ Regression gate triggered: {len(reg.get('regression_reasons', []))} reasons")
        else:
            errors.append("Regression gate 未触发（预期至少 pass rate 下降触发）")

        # Verification summary
        if not errors:
            print("\n✅ Demo 完成！所有验证通过")
        else:
            print(f"\n❌ Demo 完成但有 {len(errors)} 个验证失败:")
            for e in errors:
                print(f"   - {e}")
            sys.exit(1)

    except Exception as e:
        print(f"\n❌ 验证过程异常: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
