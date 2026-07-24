#!/usr/bin/env python3
"""客服订单处理 Agent — 复杂真实 Agent 示例。

该示例展示一个完整的客服 Agent 流程：
1. 分类用户意图（退款/物流/售后政策）
2. 查询本地订单信息
3. 检索退款/物流/售后政策知识库
4. 根据订单和政策做业务判断
5. 调用 LLM 生成最终中文回复
6. 通过 SDK 上报 run 状态变化和 Deep Trace

不依赖数据库或向量库——订单数据和知识库均为内置列表。
可在不配置真实 LLM key 的情况下使用 FakeLLMClient 进行测试。

用法：
  python examples/customer_support_agent.py --server http://127.0.0.1:8080

单元测试：
  cd python && python -m pytest tests/test_customer_support_agent.py -v
"""

from __future__ import annotations

import datetime
import json
import logging
import os
import sys
import time
import uuid
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

logger = logging.getLogger(__name__)

# ============================================================
# 领域模型
# ============================================================


@dataclass
class Order:
    """订单信息。"""

    id: str
    status: str
    product_name: str
    price: float
    purchase_date: str
    delivery_date: str | None = None
    delivery_status: str = ""
    return_window_days: int = 7


@dataclass
class Policy:
    """政策条目。"""

    id: str
    title: str
    content: str
    tags: list[str] = field(default_factory=list)


@dataclass
class RetrievalResult:
    """检索结果。"""

    document_ids: list[str]
    scores: list[float]
    documents: list[str]


@dataclass
class IntentResult:
    """意图分类结果。"""

    intent: str  # "refund", "delivery", "return_policy", "unknown"
    confidence: float = 1.0


@dataclass
class EligibilityResult:
    """退款资格判断结果。"""

    eligible: bool
    reason: str


# ============================================================
# 内置本地订单数据
# ============================================================

LOCAL_ORDERS: dict[str, Order] = {
    "ORD-1001": Order(
        id="ORD-1001",
        status="delivered",
        product_name="无线蓝牙耳机 Pro",
        price=299.00,
        purchase_date="2026-07-01",
        delivery_date="2026-07-03",
        delivery_status="已签收",
        return_window_days=7,
    ),
    "ORD-1002": Order(
        id="ORD-1002",
        status="shipped",
        product_name="USB-C 充电器 65W",
        price=89.00,
        purchase_date="2026-07-14",
        delivery_date=None,
        delivery_status="运输中",
        return_window_days=7,
    ),
    "ORD-1003": Order(
        id="ORD-1003",
        status="delivered",
        product_name="机械键盘 K8 Pro",
        price=599.00,
        purchase_date="2026-06-20",
        delivery_date="2026-06-23",
        delivery_status="已签收",
        return_window_days=7,
    ),
    "ORD-1004": Order(
        id="ORD-1004",
        status="delivered",
        product_name="4K 显示器 27寸",
        price=2499.00,
        purchase_date="2026-05-01",
        delivery_date="2026-05-04",
        delivery_status="已签收",
        return_window_days=7,
    ),
}

# ============================================================
# 内置本地知识库（售后政策）
# ============================================================

LOCAL_POLICIES: list[Policy] = [
    Policy(
        id="POLICY-RETURN-001",
        title="七天无理由退货",
        content=(
            "自商品签收之日起 7 天内，商品完好、不影响二次销售的，"
            "可以申请七天无理由退货。非质量问题的退货运费由买家承担。"
        ),
        tags=["refund", "return", "退货", "退款"],
    ),
    Policy(
        id="POLICY-RETURN-002",
        title="质量问题退货",
        content=(
            "商品存在质量问题的，自签收之日起 15 天内可以申请退货退款。"
            "退货运费由卖家承担。需要提供质量问题证明（照片/视频）。"
        ),
        tags=["refund", "quality", "质量问题", "退货"],
    ),
    Policy(
        id="POLICY-DELIVERY-001",
        title="物流配送时效",
        content=(
            "标准配送：下单后 2-3 个工作日送达。加急配送：下单后 1 个工作日送达。如超时未送达，可联系客服查询物流状态。"
        ),
        tags=["delivery", "物流", "配送"],
    ),
    Policy(
        id="POLICY-DELIVERY-002",
        title="物流异常处理",
        content=("如物流信息超过 48 小时未更新，请联系客服核查。物流丢件或损坏的，由卖家补发或退款。"),
        tags=["delivery", "物流异常", "丢件", "损坏"],
    ),
    Policy(
        id="POLICY-REFUND-001",
        title="退款时效",
        content=("退款审核通过后，款项将在 1-3 个工作日内原路返回。使用余额支付的订单即时到账。"),
        tags=["refund", "退款", "时效"],
    ),
]


# ============================================================
# 核心业务逻辑
# ============================================================


def classify_intent(user_input: str) -> IntentResult:
    """根据用户输入判断意图。

    Args:
        user_input: 用户输入文本。

    Returns:
        IntentResult 包含意图类型和置信度。
    """
    text = user_input.lower()

    # 退款相关关键词
    refund_keywords = ["退款", "退钱", "退货", "refund", "退货退款", "能退吗", "坏了", "质量问题"]
    # 物流相关关键词
    delivery_keywords = ["送达", "什么时候到", "物流", "配送", "快递", "发货", "发出", "delivery", "ship", "运输"]
    # 售后政策相关关键词
    policy_keywords = ["政策", "规则", "规定", "怎么办", "如何售后", "policy"]

    text_lower = text

    # 如果同时包含"退货"和"政策"，优先归为 return_policy
    if "退货政策" in text_lower or "退货规则" in text_lower or "return policy" in text_lower:
        return IntentResult(intent="return_policy", confidence=0.9)

    refund_score = sum(1 for kw in refund_keywords if kw in text_lower)
    delivery_score = sum(1 for kw in delivery_keywords if kw in text_lower)
    policy_score = sum(1 for kw in policy_keywords if kw in text_lower)

    max_score = max(refund_score, delivery_score, policy_score)

    if max_score == 0:
        return IntentResult(intent="unknown", confidence=0.3)

    if refund_score >= delivery_score and refund_score >= policy_score:
        return IntentResult(intent="refund", confidence=min(1.0, 0.5 + refund_score * 0.2))
    elif delivery_score >= refund_score and delivery_score >= policy_score:
        return IntentResult(intent="delivery", confidence=min(1.0, 0.5 + delivery_score * 0.2))
    else:
        return IntentResult(intent="return_policy", confidence=min(1.0, 0.5 + policy_score * 0.2))


def lookup_order(order_id: str | None) -> Order | None:
    """查询订单信息。

    Args:
        order_id: 订单 ID（如 "ORD-1001"）。

    Returns:
        Order 对象或 None（未找到）。
    """
    if not order_id:
        return None
    return LOCAL_ORDERS.get(order_id.upper())


def extract_order_id(user_input: str) -> str | None:
    """从用户输入中提取订单编号。

    Args:
        user_input: 用户输入文本。

    Returns:
        订单 ID 或 None。
    """
    import re

    match = re.search(r"(ORD[-_]?\w+)", user_input, re.IGNORECASE)
    if match:
        raw = match.group(1)
        normalized = raw.replace("_", "-").upper()
        if not normalized.startswith("ORD-"):
            normalized = "ORD-" + normalized.lstrip("ORD-")
        return normalized
    return None


def retrieve_policy(intent: str, top_k: int = 3) -> RetrievalResult:
    """根据意图检索相关政策。

    Args:
        intent: 意图类型（"refund"、"delivery"、"return_policy"、"unknown"）。
        top_k: 返回的最大文档数。

    Returns:
        RetrievalResult 包含文档 ID、分数和内容。
    """
    # 简单的关键词匹配检索
    intent_tag_map: dict[str, list[str]] = {
        "refund": ["退款", "退货", "refund"],
        "delivery": ["物流", "配送", "delivery"],
        "return_policy": ["退货", "退款", "售后"],
        "unknown": ["售后"],
    }

    keywords = intent_tag_map.get(intent, ["售后"])
    scored: list[tuple[float, Policy]] = []

    for policy in LOCAL_POLICIES:
        score = 0.0
        for kw in keywords:
            if kw in policy.title or kw in policy.content:
                score += 1.0
            for tag in policy.tags:
                if kw.lower() in tag.lower():
                    score += 0.5
        if score > 0:
            scored.append((score, policy))

    # 按分数降序排列
    scored.sort(key=lambda x: x[0], reverse=True)
    top = scored[:top_k]

    return RetrievalResult(
        document_ids=[p.id for _, p in top],
        scores=[round(s / max(s for s, _ in scored), 3) if scored else 0.0 for s, _ in top] if top else [],
        documents=[p.content for _, p in top],
    )


def check_refund_eligibility(order: Order, policy_result: RetrievalResult) -> EligibilityResult:
    """根据订单信息和政策判断退款资格。

    Args:
        order: 订单信息。
        policy_result: 检索到的政策。

    Returns:
        EligibilityResult 包含是否可退款及原因。
    """
    # 计算签收后天数
    if order.delivery_date:
        delivery = datetime.date.fromisoformat(order.delivery_date)
        today = datetime.date.today()
        days_since_delivery = (today - delivery).days
    else:
        days_since_delivery = 0

    # 订单未发货
    if order.status == "pending":
        return EligibilityResult(eligible=True, reason="订单未发货，可以直接取消并退款。")

    # 已签收，检查退货窗口
    if days_since_delivery <= order.return_window_days:
        return EligibilityResult(
            eligible=True,
            reason=f"订单在 {order.return_window_days} 天退货期内，可以申请退货退款。",
        )

    # 超过退货窗口
    if days_since_delivery > 30:
        return EligibilityResult(
            eligible=False,
            reason=f"订单已签收 {days_since_delivery} 天，远超退换货期限，无法退款。建议联系品牌方保修。",
        )

    # 超过窗口但在30天内——判断是否有质量问题相关政策
    has_quality_policy = any("质量" in doc for doc in policy_result.documents)
    if has_quality_policy:
        return EligibilityResult(
            eligible=True,
            reason=(
                f"订单已超过 {order.return_window_days} 天退货期，但仍在 15 天质量问题退货期内。"
                "若存在质量问题可申请退货退款。"
            ),
        )

    return EligibilityResult(
        eligible=False,
        reason=f"订单已签收超过 {order.return_window_days} 天，超过无理由退货期限。",
    )


# ============================================================
# Fake LLM Client（用于测试）
# ============================================================


class FakeLLMClient:
    """模拟 LLM 客户端，用于测试场景。

    Args:
        responses: 可选的响应映射表。key 为输入文本前缀，value 为返回内容。
    """

    def __init__(self, responses: dict[str, str] | None = None):
        self.provider = "fake"
        self.model = "fake-model"
        self._responses = responses or {}

    def generate_response(
        self,
        messages: list[dict[str, str]],
        **kwargs: Any,
    ) -> Any:
        """生成模拟响应。

        Args:
            messages: 消息列表。
            **kwargs: 额外参数。

        Returns:
            包含 content、provider、model 等属性的模拟响应。
        """
        user_msg = next((m["content"] for m in messages if m["role"] == "user"), "")

        content = self._responses.get(user_msg, "")
        if not content:
            # 根据子串匹配，在组合后的长文本中查找响应键
            for key, resp in self._responses.items():
                if key in user_msg:
                    content = resp
                    break

        if not content:
            content = f"【模拟回复】已收到您关于「{user_msg[:30]}」的咨询。"

        return FakeLLMResponse(
            content=content,
            provider=self.provider,
            model=self.model,
            latency_ms=10,
            prompt_tokens=50,
            completion_tokens=len(content),
            total_tokens=50 + len(content),
        )


@dataclass
class FakeLLMResponse:
    """模拟 LLM 响应。"""

    content: str
    provider: str
    model: str
    latency_ms: int
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int


# ============================================================
# 主流程编排
# ============================================================


def generate_final_answer(
    user_input: str,
    order: Order | None,
    intent: IntentResult,
    eligibility: EligibilityResult | None,
    policy_result: RetrievalResult,
    llm_client: Any,
) -> str:
    """生成最终的中文客服回复。

    Args:
        user_input: 用户原始输入。
        order: 订单信息（可能为 None）。
        intent: 意图分类结果。
        eligibility: 退款资格判断（可能为 None）。
        policy_result: 检索到的政策。
        llm_client: LLM 客户端（真实或 fake）。

    Returns:
        最终回复文本。
    """
    # 构建系统 prompt
    system_prompt = (
        "你是一位专业的电商客服。请根据以下信息用中文生成友好、清晰的回复。"
        "回复要包含具体信息（订单状态、政策条款），不要只说'请稍等'。"
        "如果订单不存在，礼貌告知用户未查到该订单。"
    )

    # 构建上下文
    context_parts: list[str] = []
    context_parts.append(f"用户意图: {intent.intent}（置信度: {intent.confidence:.2f}）")

    if order:
        context_parts.append(
            f"订单信息: {order.id}, 商品: {order.product_name}, 状态: {order.status}, 价格: ¥{order.price}"
        )
        if order.delivery_date:
            context_parts.append(f"签收日期: {order.delivery_date}")
        if order.delivery_status:
            context_parts.append(f"物流状态: {order.delivery_status}")
    else:
        context_parts.append("未找到该订单。")

    if eligibility:
        context_parts.append(
            f"退款资格: {'可退款' if eligibility.eligible else '不可退款'}。原因: {eligibility.reason}"
        )

    if policy_result.document_ids:
        context_parts.append(f"相关政策: {'; '.join(policy_result.documents)}")

    context_prompt = "\n".join(context_parts)

    messages = [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": f"用户问题: {user_input}\n\n上下文信息:\n{context_prompt}"},
    ]

    response = llm_client.generate_response(messages)
    return response.content.strip()


def run_customer_support_case(
    user_input: str,
    llm_client: Any,
    *,
    tracer: Any = None,
    trace_attrs: dict[str, Any] | None = None,
    status_callback: Callable[[str, str, dict[str, Any] | None], None] | None = None,
) -> dict[str, Any]:
    """执行完整的客服订单处理流程。

    这是核心编排函数，包含 Agent 运行的全部步骤。
    当提供 tracer 时，自动使用 SDK trace helper 记录深度 Trace。

    Args:
        user_input: 用户输入文本。
        llm_client: LLM 客户端（FakeLLMClient 或真实 LLMClient）。
        tracer: 可选的 OpenTelemetry tracer。提供后会自动创建 trace_llm/trace_tool/
            trace_retrieval/trace_skill 深度 Trace span。
        trace_attrs: 传递给每个结构化子 span 的额外属性。推荐包含 agentscope.agent.id、
            agentscope.agent.instance_id、agentscope.run.id，确保后端结构化调用记录
            能完整提取 Agent 归属字段。
        status_callback: 状态上报回调，签名 (step_name, status, attributes) -> None。
            如果为 None，则不记录状态变化。

    Returns:
        包含完整执行结果的字典：
        - intent: 意图分类结果
        - order: 查询到的订单（或 None）
        - eligibility: 退款资格判断（或 None）
        - policies: 检索到的政策
        - answer: LLM 生成的中文回复
        - steps: 执行步骤列表
    """
    from agentscope_sdk.tracing import trace_llm, trace_retrieval, trace_skill, trace_tool

    trace_kwargs: dict[str, Any] = trace_attrs or {}

    steps: list[dict[str, Any]] = []

    def _status(step: str, status: str, attrs: dict[str, Any] | None = None) -> None:
        if status_callback:
            status_callback(step, status, attrs)

    result: dict[str, Any] = {
        "intent": None,
        "order": None,
        "eligibility": None,
        "policies": None,
        "answer": "",
        "steps": steps,
        "order_id": extract_order_id(user_input),
    }

    # ---- Step 1: 意图分类 ----
    _status("classify_intent", "running", {"input": user_input})
    intent_result = classify_intent(user_input)
    steps.append({"step": "classify_intent", "intent": intent_result.intent, "confidence": intent_result.confidence})
    result["intent"] = intent_result

    # ---- Step 2: 查询订单（SDK trace_tool） ----
    _status("lookup_order", "running", {"intent": intent_result.intent})
    order_id = extract_order_id(user_input)
    order: Order | None = None

    if tracer:
        with trace_tool(
            tracer,
            "lookup_order",
            tool_type="local",
            arguments_summary=json.dumps({"order_id": order_id}, ensure_ascii=False),
            **trace_kwargs,
        ) as tool_span:
            order = lookup_order(order_id) if order_id else None
            # 在查询完成后设置 result_summary，确保结果准确
            tool_span.set_attribute("tool.result_summary", f"found={order is not None}")
            tool_span.set_attribute("tool.success", "true")
    else:
        order = lookup_order(order_id) if order_id else None

    steps.append({"step": "lookup_order", "order_id": order_id, "found": order is not None})
    result["order"] = order

    # ---- Step 3: 意图分发 ----
    if intent_result.intent in ("refund", "return_policy") and order:
        # Step 3a: 检索退款政策（SDK trace_retrieval）
        _status("retrieve_policy", "running", {"intent": intent_result.intent})
        if tracer:
            with trace_retrieval(
                tracer,
                "retrieve_policy",
                knowledge_base="local_policies",
                query_text=intent_result.intent,
                top_k=3,
                **trace_kwargs,
            ) as span:
                policy_result = retrieve_policy(intent_result.intent, top_k=3)
                span.set_attribute("retrieval.returned_count", len(policy_result.document_ids))
                if policy_result.document_ids:
                    span.set_attribute("retrieval.document_ids", policy_result.document_ids)
                if policy_result.scores:
                    span.set_attribute("retrieval.scores", policy_result.scores)
        else:
            policy_result = retrieve_policy(intent_result.intent, top_k=3)

        steps.append(
            {
                "step": "retrieve_policy",
                "document_ids": policy_result.document_ids,
                "scores": policy_result.scores,
            }
        )
        result["policies"] = policy_result

        # Step 3b: 判断退款资格（SDK trace_skill）
        _status("check_refund_eligibility", "running", {"order_id": order.id})
        if tracer:
            skill_input = json.dumps({"order_id": order.id, "status": order.status}, ensure_ascii=False)
            with trace_skill(tracer, "check_refund_eligibility", input_summary=skill_input, **trace_kwargs) as span:
                eligibility = check_refund_eligibility(order, policy_result)
                span.set_attribute(
                    "skill.output_summary",
                    json.dumps(
                        {"eligible": eligibility.eligible, "reason": eligibility.reason[:100]}, ensure_ascii=False
                    ),
                )
                span.set_attribute("skill.success", "true")
        else:
            eligibility = check_refund_eligibility(order, policy_result)

        steps.append(
            {
                "step": "check_refund_eligibility",
                "eligible": eligibility.eligible,
                "reason": eligibility.reason,
            }
        )
        result["eligibility"] = eligibility

    elif intent_result.intent == "delivery" and order:
        # 物流查询：检索物流政策
        _status("retrieve_policy", "running", {"intent": "delivery"})
        if tracer:
            with trace_retrieval(
                tracer,
                "retrieve_policy",
                knowledge_base="local_policies",
                query_text="delivery",
                top_k=2,
                **trace_kwargs,
            ) as span:
                policy_result = retrieve_policy("delivery", top_k=2)
                span.set_attribute("retrieval.returned_count", len(policy_result.document_ids))
                if policy_result.document_ids:
                    span.set_attribute("retrieval.document_ids", policy_result.document_ids)
                if policy_result.scores:
                    span.set_attribute("retrieval.scores", policy_result.scores)
        else:
            policy_result = retrieve_policy("delivery", top_k=2)

        steps.append(
            {
                "step": "retrieve_policy",
                "document_ids": policy_result.document_ids,
                "scores": policy_result.scores,
            }
        )
        result["policies"] = policy_result
        result["eligibility"] = None

    else:
        policy_result = RetrievalResult(document_ids=[], scores=[], documents=[])
        result["policies"] = policy_result
        result["eligibility"] = None

    # ---- Step 4: LLM 生成最终回复（SDK trace_llm） ----
    _status("generate_final_answer", "running", {"intent": intent_result.intent})
    provider = getattr(llm_client, "provider", "unknown")
    model = getattr(llm_client, "model", "unknown")

    if tracer:
        with trace_llm(
            tracer,
            "generate_final_answer",
            provider=provider,
            model=model,
            input_summary=user_input[:200],
            **trace_kwargs,
        ) as span:
            answer = generate_final_answer(
                user_input=user_input,
                order=order,
                intent=intent_result,
                eligibility=result.get("eligibility"),
                policy_result=policy_result,
                llm_client=llm_client,
            )
            span.set_attribute("llm.output_summary", answer[:200])
    else:
        answer = generate_final_answer(
            user_input=user_input,
            order=order,
            intent=intent_result,
            eligibility=result.get("eligibility"),
            policy_result=policy_result,
            llm_client=llm_client,
        )

    steps.append(
        {
            "step": "generate_final_answer",
            "answer_length": len(answer),
        }
    )
    result["answer"] = answer

    _status("completed", "completed", {})

    return result


# ============================================================
# 主入口（作为独立脚本运行时使用）
# ============================================================


def _print_demo_result(result: dict[str, Any]) -> None:
    """打印演示结果到控制台。"""
    print()
    print("  ┌──────────────────────────────────────────────┐")
    print("  │             客服 Agent 执行结果              │")
    print("  └──────────────────────────────────────────────┘")
    print()

    intent = result.get("intent")
    if intent:
        print(f"  🏷  意图: {intent.intent} (置信度: {intent.confidence:.2f})")

    order = result.get("order")
    if order:
        print(f"  📦 订单: {order.id}")
        print(f"     商品: {order.product_name} (¥{order.price})")
        print(f"     状态: {order.status} / {order.delivery_status}")
    else:
        print("  📦 订单: 未找到")

    eligibility = result.get("eligibility")
    if eligibility:
        print(f"  ✅ 退款资格: {'可退款' if eligibility.eligible else '不可退款'}")
        print(f"     原因: {eligibility.reason}")

    policies = result.get("policies")
    if policies and policies.document_ids:
        print(f"  📚 相关政策 ({len(policies.document_ids)} 条): {', '.join(policies.document_ids)}")

    answer = result.get("answer", "")
    if answer:
        print(f"\n  💬 客服回复:\n    {answer}")

    steps = result.get("steps", [])
    print(f"\n  📋 执行步骤 ({len(steps)} 步):")
    for s in steps:
        print(f"    {s['step']}")

    print()


def _setup_fake_demo() -> None:
    """使用 FakeLLMClient 运行演示（不依赖真实 LLM）。"""
    print()
    print("=" * 60)
    print("AgentScope 客服订单处理 Agent 演示 (Fake LLM 模式)")
    print("=" * 60)
    print()

    test_cases = [
        "我的订单 ORD-1001 可以退款吗？",
        "ORD-1002 什么时候送达？",
        "我买的耳机坏了，能退吗？",
        "ORD-9999 现在什么状态？",
    ]

    responses = {
        "我的订单 ORD-1001 可以退款吗？": (
            "您好！关于您订单 ORD-1001（无线蓝牙耳机 Pro）的退款问题，"
            "查询到该订单于 2026-07-03 签收，目前仍在 7 天退货期内，"
            "您可以申请退货退款。商品完好不影响二次销售即可。"
        ),
        "ORD-1002 什么时候送达？": (
            "您好！关于订单 ORD-1002（USB-C 充电器 65W）的配送进度，"
            "查询到该订单于 2026-07-14 下单，目前状态为'运输中'。"
            "按照标准配送时效预计 2-3 个工作日送达，请您耐心等待。"
            "如果超过预计时间未收到，请联系我们核查物流状态。"
        ),
        "我买的耳机坏了，能退吗？": (
            "您好！关于无线蓝牙耳机的质量问题，非常抱歉给您带来不便。"
            "如果您购买的耳机存在质量问题，自签收之日起 15 天内可以申请质量问题退货退款。"
            "退货运费由我们承担。请您提供质量问题照片或视频作为凭证，"
            "通过订单详情页的'申请售后'入口提交申请即可。"
        ),
    }

    fake_llm = FakeLLMClient(responses=responses)

    for i, case in enumerate(test_cases, 1):
        print(f"  [客户] 用户 {i}: {case}")
        result = run_customer_support_case(case, fake_llm)
        print(f"  [客服] {result['answer']}")
        print()

    print("=" * 60)
    print("演示完成！共处理", len(test_cases), "个客服请求。")
    print("=" * 60)


def compute_latency_ms(*, start_monotonic: float, end_monotonic: float) -> int:
    """Compute the wall-clock latency in milliseconds from two ``time.monotonic()`` values.

    Returns at least 1 ms so the backend never sees a zero-duration run.
    """
    return max(1, int((end_monotonic - start_monotonic) * 1000))


def main_with_sdk() -> None:
    """通过 AgentScope SDK 运行客服 Agent 演示（连接后端）。"""
    import argparse

    from agentscope_sdk.llm import LLMClient, LLMConfigError
    from agentscope_sdk.runtime import (
        fail_run,
        finish_run,
        shutdown_runtime,
        start_run,
        update_run_status,
    )
    from agentscope_sdk.runtime import (
        init as agent_init,
    )
    from agentscope_sdk.tracing import configure_tracing, force_flush, shutdown_tracing

    parser = argparse.ArgumentParser(description="AgentScope 客服订单处理 Agent 示例")
    parser.add_argument("--server", default="http://127.0.0.1:8080", help="后端地址")
    parser.add_argument("--fake", action="store_true", help="使用 fake LLM（不调用真实模型）")
    args = parser.parse_args()

    base_url = args.server
    fe_url = base_url.replace(":8080", ":5173")

    print("=" * 60)
    print("AgentScope 客服订单处理 Agent 演示")
    print("=" * 60)
    print()

    # ---- 创建临时邮箱和项目 ----
    import httpx

    demo_email = f"cs-demo-{uuid.uuid4().hex[:8]}@example.com"
    demo_password = "demo-password-123"

    print("[1/6] 用户注册/登录")
    client = httpx.Client(base_url=base_url, follow_redirects=True)
    resp = client.post(
        "/api/auth/register",
        json={"email": demo_email, "password": demo_password, "display_name": "客服Demo用户"},
    )
    if resp.status_code == 201:
        user_data = resp.json()
        print(f"  [user] 注册成功: {user_data['email']} (id={user_data['id']})")
    elif resp.status_code == 409:
        resp = client.post(
            "/api/auth/login",
            json={"email": demo_email, "password": demo_password},
        )
        user_data = resp.json()
        print(f"  [user] 登录成功: {user_data['email']} (id={user_data['id']})")
    else:
        print(f"  [user] 注册/登录失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    # ---- 创建项目 ----
    print("[2/6] 创建项目")
    project_name = f"cs-agent-demo-{uuid.uuid4().hex[:6]}"
    resp = client.post("/api/projects", json={"name": project_name, "description": "客服订单处理Agent演示"})
    if resp.status_code != 201:
        print(f"  [project] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    project = resp.json()
    project_id = project["id"]
    print(f"  [project] 创建成功: {project['name']} (id={project_id})")

    # ---- 创建 Project Token ----
    print("[3/6] 创建 Project Token")
    resp = client.post(
        f"/api/projects/{project_id}/tokens",
        json={
            "description": "customer-support-agent-demo",
            "scopes": ["otlp:write", "runner:write", "agent:register", "agent:runtime"],
        },
    )
    if resp.status_code != 201:
        print(f"  [token] 创建失败: {resp.status_code} {resp.text}", file=sys.stderr)
        sys.exit(1)
    token_data = resp.json()
    plain_token = token_data["plain_token"]
    print(f"  [token] 创建成功: prefix={token_data['token']['token_prefix']}")

    # ---- 配置 LLM ----
    print("[4/6] 配置 LLM")

    if args.fake:
        llm_client = FakeLLMClient()
        print("  [llm] 使用 Fake LLM（不调用真实模型）")
    else:
        try:
            llm_client = LLMClient(timeout=60.0)
            print(f"  [llm] 使用 {llm_client.provider}/{llm_client.model}")
        except LLMConfigError as e:
            print("  [llm] 未配置真实 LLM（可加 --fake 运行 Demo）")
            print(f"  [llm] {e}")
            sys.exit(1)

    # ---- 注册 Agent + 启动心跳 ----
    print("[5/6] 注册 Agent 到控制中心（启动心跳）")
    rt = agent_init(
        server_url=base_url,
        project_token=plain_token,
        agent_name="customer-support-agent",
        description="客服订单处理Agent — 支持退款、物流查询、售后政策咨询",
        metadata={"framework": "agentscope-demo", "demo": "customer-support"},
        heartbeat_interval=30,
    )
    agent_id = rt.agent_id
    print(f"  [agent] 注册成功: id={agent_id}")

    # ---- 配置 Tracing ----
    tracer = configure_tracing(
        server_url=base_url,
        project_token=plain_token,
        service_name="customer-support-agent",
    )
    print("  [tracing] OpenTelemetry 追踪已配置")

    # ---- 运行演示 ----
    print("[6/6] 运行客服 Agent 演示")

    test_cases = [
        "我的订单 ORD-1001 可以退款吗？",
        "ORD-1002 什么时候送达？",
        "我买的耳机坏了，能退吗？",
        "ORD-1004 退不了了怎么办？",
    ]

    # 定义状态回调：通过 SDK runtime 上报状态变化
    def _make_status_cb(rt_obj: Any) -> Callable[[str, str, dict[str, Any] | None], None]:
        def _cb(step: str, status: str, attrs: dict[str, Any] | None) -> None:
            if step == "classify_intent":
                rt_obj.update_run_status("waiting_llm", message="正在分析用户意图")
            elif step == "lookup_order":
                rt_obj.update_run_status("waiting_tool", message="正在查询订单")
            elif step == "retrieve_policy":
                rt_obj.update_run_status("waiting_retrieval", message="正在检索政策")
            elif step == "check_refund_eligibility":
                rt_obj.update_run_status("running", message="正在判断退款资格")
            elif step == "generate_final_answer":
                rt_obj.update_run_status("waiting_llm", message="正在生成回复")
            elif step == "completed":
                # Terminal states should use finish_run, not update_run_status
                pass

        return _cb

    for i, case in enumerate(test_cases, 1):
        print(f"\n  ── 案例 {i}/{len(test_cases)} ──")
        print(f"  [用户]: {case}")

        case_started = time.monotonic()

        # 创建根 span（作为当前激活 span），获取真实 OTLP trace id
        with tracer.start_as_current_span("customer_support.run") as root_span:
            actual_trace_id = format(root_span.get_span_context().trace_id, "032x")
            root_span.set_attribute("agentscope.span.kind", "AGENT")
            root_span.set_attribute("openinference.span.kind", "AGENT")
            root_span.set_attribute("agentscope.agent.name", "customer-support-agent")
            root_span.set_attribute("agentscope.agent.id", rt.agent_id)
            root_span.set_attribute("agentscope.agent.instance_id", rt.instance_id)

            # 用真实 OTLP trace id 创建 run（不再使用随机 uuid）
            run = start_run(input=case[:200], metadata={"case_index": i, "user_input": case}, trace_id=actual_trace_id)
            run_id_value = ""
            if run:
                run_id_value = run.id
                root_span.set_attribute("agentscope.run.id", run_id_value)
                print(f"  [runtime run]: {run_id_value}")
                print(f"  [OTLP trace]:  {actual_trace_id}")
                print(f"  [Run Detail]:   {fe_url}/runs/{run_id_value}")

            try:
                # 构造子 span 归属字段，确保结构化调用记录携带 Agent 信息
                trace_attrs: dict[str, Any] = {
                    "agentscope.agent.id": rt.agent_id,
                    "agentscope.agent.instance_id": rt.instance_id,
                    "agentscope.run.id": run_id_value,
                }

                # Update status to running
                update_run_status("running", message=f"处理第 {i} 个客服请求")
                print("  [状态]: running")

                # 在根 span context 内执行 run_customer_support_case
                # 子 span（LLM/tool/retrieval/skill）自动继承同一个 trace id
                result = run_customer_support_case(
                    case,
                    llm_client,
                    tracer=tracer,
                    trace_attrs=trace_attrs,
                    status_callback=_make_status_cb(rt),
                )

                # Print result
                answer = result.get("answer", "")
                print("  [状态]: completed")
                print(f"  [客服]: {answer[:100]}...")

                # Compute actual latency
                case_latency_ms = compute_latency_ms(start_monotonic=case_started, end_monotonic=time.monotonic())

                # Finish the run
                if run:
                    finish_run(
                        output=answer[:500],
                        latency_ms=case_latency_ms,
                        metrics={
                            "steps": len(result.get("steps", [])),
                            "intent": result.get("intent", {}).intent if result.get("intent") else "",
                        },
                    )

            except Exception as e:
                print(f"  [状态]: failed ({e})")
                if run:
                    fail_run(error=str(e), attributes={"case": case})
                import traceback

                traceback.print_exc()

        # 强制发送 OTLP spans 到后端，确保每个 case 的 trace 独立入站
        force_flush()

    # ---- 关闭 Agent 运行时 ----
    print("\n  [runtime] 关闭 Agent 运行时")
    shutdown_runtime()
    shutdown_tracing()

    # ---- 输出资源链接 ----
    print()
    print("=" * 60)
    print("演示完成！以下是可访问的资源：")
    print("=" * 60)
    print()
    print(f"  登录邮箱:     {demo_email}")
    print(f"  登录密码:     {demo_password}")
    print(f"  前端地址:     {fe_url}")
    print(f"  项目:         {fe_url}/projects/{project_id}")
    print(f"  Agent 列表:   {fe_url}/projects/{project_id}/agents")
    print(f"  API 项目:     {base_url}/api/projects/{project_id}")
    print()
    print("  提示：在前端的 Run Detail 页面可查看 LLM、工具、知识库检索和 Skill 调用。")
    print()


def main() -> None:
    """主入口。当作为独立脚本运行时检查环境并执行。"""
    # 检查是否是 SDK 模式
    if "--server" in sys.argv or os.environ.get("AGENTSCOPE_SERVER_URL"):
        main_with_sdk()
    else:
        _setup_fake_demo()


if __name__ == "__main__":
    main()
