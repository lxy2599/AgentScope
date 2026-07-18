"""Tests for the customer support agent.

Covers the core business logic of order lookup, refund eligibility,
knowledge base retrieval, intent classification, and the complete
orchestration flow with a fake LLM client.

These tests do NOT require a real LLM API key or a running backend.
"""

from __future__ import annotations

import datetime
from unittest.mock import Mock, patch

import pytest

from examples.customer_support_agent import (
    FakeLLMClient,
    FakeLLMResponse,
    LOCAL_ORDERS,
    LOCAL_POLICIES,
    Order,
    RetrievalResult,
    check_refund_eligibility,
    classify_intent,
    extract_order_id,
    generate_final_answer,
    lookup_order,
    retrieve_policy,
    run_customer_support_case,
)


# ================================================================
# 意图分类测试
# ================================================================


class TestClassifyIntent:
    def test_refund_keyword_detected(self):
        result = classify_intent("我的订单 ORD-1001 可以退款吗？")
        assert result.intent == "refund"
        assert result.confidence >= 0.5

    def test_refund_product_broken(self):
        result = classify_intent("我买的耳机坏了，能退吗？")
        assert result.intent == "refund"
        assert result.confidence >= 0.5

    def test_delivery_keyword_detected(self):
        result = classify_intent("ORD-1002 什么时候送达？")
        assert result.intent == "delivery"
        assert result.confidence >= 0.5

    def test_delivery_ship_status(self):
        result = classify_intent("我的货发出了吗？")
        assert result.intent == "delivery"
        assert result.confidence >= 0.5

    def test_policy_keyword_detected(self):
        result = classify_intent("退货政策是什么？")
        assert result.intent == "return_policy"

    def test_unknown_intent(self):
        result = classify_intent("你好，今天天气不错")
        assert result.intent == "unknown"
        assert result.confidence < 0.5


# ================================================================
# 订单 ID 提取测试
# ================================================================


class TestExtractOrderId:
    def test_extract_standard(self):
        assert extract_order_id("ORD-1001") == "ORD-1001"

    def test_extract_from_sentence(self):
        assert extract_order_id("我的订单 ORD-1001 可以退款吗？") == "ORD-1001"

    def test_extract_with_underscore(self):
        assert extract_order_id("查询 ORD_1002 的状态") == "ORD-1002"

    def test_extract_lowercase(self):
        assert extract_order_id("ord-1003 怎么样") == "ORD-1003"

    def test_no_order_id(self):
        assert extract_order_id("你好，请问有什么可以帮您？") is None


# ================================================================
# 订单查询测试
# ================================================================


class TestLookupOrder:
    def test_found_existing_order(self):
        order = lookup_order("ORD-1001")
        assert order is not None
        assert order.id == "ORD-1001"
        assert order.product_name == "无线蓝牙耳机 Pro"
        assert order.status == "delivered"
        assert order.delivery_status == "已签收"
        assert order.price == 299.00

    def test_found_order_1002(self):
        order = lookup_order("ORD-1002")
        assert order is not None
        assert order.id == "ORD-1002"
        assert order.product_name == "USB-C 充电器 65W"
        assert order.status == "shipped"
        assert order.delivery_status == "运输中"

    def test_not_found(self):
        order = lookup_order("ORD-9999")
        assert order is None

    def test_case_insensitive(self):
        order = lookup_order("ord-1001")
        assert order is not None
        assert order.id == "ORD-1001"

    def test_empty_input(self):
        order = lookup_order("")
        assert order is None

    def test_none_input(self):
        order = lookup_order(None)  # type: ignore[arg-type]
        assert order is None


# ================================================================
# 知识库检索测试
# ================================================================


class TestRetrievePolicy:
    def test_retrieval_returns_documents_for_refund(self):
        result = retrieve_policy("refund", top_k=3)
        assert len(result.document_ids) > 0
        assert len(result.scores) > 0
        assert len(result.scores) == len(result.document_ids)
        # All scores should be between 0 and 1
        for score in result.scores:
            assert 0 <= score <= 1

    def test_retrieval_returns_documents_for_delivery(self):
        result = retrieve_policy("delivery", top_k=2)
        assert len(result.document_ids) > 0
        assert len(result.documents) > 0
        # Should get delivery-related policies
        assert any("物流" in doc or "配送" in doc for doc in result.documents)

    def test_retrieval_document_ids_format(self):
        result = retrieve_policy("refund")
        for doc_id in result.document_ids:
            assert doc_id.startswith("POLICY-")

    def test_retrieval_top_k_limits(self):
        result = retrieve_policy("refund", top_k=1)
        assert len(result.document_ids) <= 1

    def test_retrieval_scores_are_floats(self):
        result = retrieve_policy("refund")
        for score in result.scores:
            assert isinstance(score, float)

    def test_retrieval_unknown_intent(self):
        result = retrieve_policy("unknown")
        # Unknown intent should still return some policies
        assert isinstance(result.document_ids, list)


# ================================================================
# 退款资格判断测试
# ================================================================


class TestCheckRefundEligibility:
    def test_recent_delivery_within_window(self):
        """订单在退货期内应可退款。"""
        order = Order(
            id="ORD-TEST",
            status="delivered",
            product_name="测试商品",
            price=100.00,
            purchase_date=(datetime.date.today() - datetime.timedelta(days=3)).isoformat(),
            delivery_date=(datetime.date.today() - datetime.timedelta(days=1)).isoformat(),
            delivery_status="已签收",
            return_window_days=7,
        )
        policy_result = RetrievalResult(document_ids=["POLICY-RETURN-001"], scores=[1.0], documents=["七天无理由退货政策"])
        result = check_refund_eligibility(order, policy_result)
        assert result.eligible is True
        assert "退货期" in result.reason

    def test_over_30_days_not_eligible(self):
        """订单签收超过30天应不可退款。"""
        order = Order(
            id="ORD-TEST",
            status="delivered",
            product_name="测试商品",
            price=100.00,
            purchase_date=(datetime.date.today() - datetime.timedelta(days=60)).isoformat(),
            delivery_date=(datetime.date.today() - datetime.timedelta(days=57)).isoformat(),
            delivery_status="已签收",
            return_window_days=7,
        )
        policy_result = RetrievalResult(document_ids=[], scores=[], documents=[])
        result = check_refund_eligibility(order, policy_result)
        assert result.eligible is False
        assert "无法退款" in result.reason or "保修" in result.reason

    def test_pending_order_eligible(self):
        """未发货的订单应可直接退款。"""
        order = Order(
            id="ORD-TEST",
            status="pending",
            product_name="测试商品",
            price=100.00,
            purchase_date=datetime.date.today().isoformat(),
            delivery_status="未发货",
            return_window_days=7,
        )
        policy_result = RetrievalResult(document_ids=[], scores=[], documents=[])
        result = check_refund_eligibility(order, policy_result)
        assert result.eligible is True
        assert "未发货" in result.reason

    def test_over_window_with_quality_policy(self):
        """超过退货期但存在质量政策时可能仍有条件退款。"""
        order = Order(
            id="ORD-TEST",
            status="delivered",
            product_name="测试商品",
            price=100.00,
            purchase_date=(datetime.date.today() - datetime.timedelta(days=14)).isoformat(),
            delivery_date=(datetime.date.today() - datetime.timedelta(days=12)).isoformat(),
            delivery_status="已签收",
            return_window_days=7,
        )
        policy_result = RetrievalResult(
            document_ids=["POLICY-RETURN-002"],
            scores=[1.0],
            documents=["商品存在质量问题的，自签收之日起 15 天内可以申请退货退款。"],
        )
        result = check_refund_eligibility(order, policy_result)
        # 12 days > 7 (return_window) so not eligible for no-questions-return
        # But should still check if quality policy applies
        assert result.reason
        # In this case the order is between 7-30 days with quality policy available
        # After 7 days but before/at 15 days with quality policy -> eligible
        if order.delivery_date:
            delivery = datetime.date.fromisoformat(order.delivery_date)
            days = (datetime.date.today() - delivery).days
            if days <= 15 and "质量" in policy_result.documents[0]:
                assert result.eligible is True


# ================================================================
# Fake LLM 客户端测试
# ================================================================


class TestFakeLLMClient:
    def test_generate_response_default(self):
        client = FakeLLMClient()
        response = client.generate_response([
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "Hello"},
        ])
        assert isinstance(response, FakeLLMResponse)
        assert response.content
        assert response.provider == "fake"

    def test_generate_response_custom(self):
        client = FakeLLMClient(responses={"Hello": "Hi there!"})
        response = client.generate_response([
            {"role": "system", "content": "You are helpful."},
            {"role": "user", "content": "Hello"},
        ])
        assert response.content == "Hi there!"
        assert response.prompt_tokens == 50
        assert response.completion_tokens == len("Hi there!")

    def test_generate_response_prefix_match(self):
        client = FakeLLMClient(responses={"我的订单": "订单已查询。"})
        response = client.generate_response([
            {"role": "user", "content": "我的订单 ORD-1001"},
        ])
        assert response.content == "订单已查询。"


# ================================================================
# 完整流程测试
# ================================================================


class TestRunCustomerSupportCase:
    def test_full_flow_refund(self):
        """完整退款流程测试。"""
        responses = {
            "我的订单 ORD-1001 可以退款吗？": "您的订单 ORD-1001 在退货期内，可以退款。",
        }
        fake_llm = FakeLLMClient(responses=responses)
        result = run_customer_support_case("我的订单 ORD-1001 可以退款吗？", fake_llm)

        assert result["intent"] is not None
        assert result["intent"].intent == "refund"
        assert result["order"] is not None
        assert result["order"].id == "ORD-1001"
        assert result["eligibility"] is not None
        assert result["eligibility"].eligible is True
        assert result["policies"] is not None
        assert len(result["policies"].document_ids) > 0
        assert result["answer"] == "您的订单 ORD-1001 在退货期内，可以退款。"
        assert len(result["steps"]) >= 4

    def test_full_flow_delivery(self):
        """完整物流查询流程测试。"""
        responses = {
            "ORD-1002 什么时候送达？": "您的订单 ORD-1002 正在运输中。",
        }
        fake_llm = FakeLLMClient(responses=responses)
        result = run_customer_support_case("ORD-1002 什么时候送达？", fake_llm)

        assert result["intent"] is not None
        assert result["intent"].intent == "delivery"
        assert result["order"] is not None
        assert result["order"].id == "ORD-1002"
        assert result["order"].delivery_status == "运输中"
        assert result["policies"] is not None
        assert len(result["policies"].document_ids) > 0
        assert result["answer"] == "您的订单 ORD-1002 正在运输中。"

    def test_unknown_order(self):
        """未找到订单的流程测试。"""
        responses = {"ORD-9999": "未查到该订单信息。"}
        fake_llm = FakeLLMClient(responses=responses)
        result = run_customer_support_case("ORD-9999 现在什么状态？", fake_llm)

        assert result["order"] is None
        assert result["answer"] == "未查到该订单信息。"

    def test_refund_rejected_old_order(self):
        """超过退货期的订单退款被拒绝。"""
        # Override LOCAL_ORDERS temporarily
        from examples import customer_support_agent as csa

        old_order = Order(
            id="ORD-OLD",
            status="delivered",
            product_name="旧商品",
            price=500.00,
            purchase_date="2026-01-01",
            delivery_date="2026-01-05",
            delivery_status="已签收",
            return_window_days=7,
        )
        original_orders = csa.LOCAL_ORDERS.copy()
        csa.LOCAL_ORDERS["ORD-OLD"] = old_order

        try:
            responses = {
                "ORD-OLD 可以退款吗？": "该订单已超过退款期限。",
            }
            fake_llm = FakeLLMClient(responses=responses)
            result = run_customer_support_case("ORD-OLD 可以退款吗？", fake_llm)

            assert result["intent"] is not None
            assert result["intent"].intent == "refund"
            assert result["order"] is not None
            assert result["order"].id == "ORD-OLD"
            assert result["eligibility"] is not None
            # Over 30 days since delivery
            assert result["eligibility"].eligible is False
        finally:
            # Restore original orders
            csa.LOCAL_ORDERS.clear()
            csa.LOCAL_ORDERS.update(original_orders)

    def test_steps_contain_classification(self):
        """验证步骤日志包含所有关键步骤。"""
        fake_llm = FakeLLMClient()
        result = run_customer_support_case("我的订单 ORD-1001 可以退款吗？", fake_llm)

        step_names = [s["step"] for s in result["steps"]]
        assert "classify_intent" in step_names
        assert "lookup_order" in step_names
        assert "retrieve_policy" in step_names
        assert "check_refund_eligibility" in step_names
        assert "generate_final_answer" in step_names

    def test_retrieval_scores_in_result(self):
        """验证检索结果的 document_ids 和 scores。"""
        fake_llm = FakeLLMClient()
        result = run_customer_support_case("ORD-1001 可以退款吗？", fake_llm)

        policies = result.get("policies")
        if policies and policies.document_ids:
            assert len(policies.document_ids) > 0
            assert len(policies.scores) == len(policies.document_ids)
            for score in policies.scores:
                assert 0 <= score <= 1

    def test_generate_final_answer_with_order(self):
        """测试 generate_final_answer 在有订单时的工作。"""
        order = LOCAL_ORDERS["ORD-1001"]
        from examples.customer_support_agent import IntentResult

        intent = IntentResult(intent="refund", confidence=0.9)
        policy_result = retrieve_policy("refund")

        from examples.customer_support_agent import check_refund_eligibility
        eligibility = check_refund_eligibility(order, policy_result)

        fake_llm = FakeLLMClient(responses={
            "退款": "您可以申请退款。",
        })

        answer = generate_final_answer(
            user_input="可以退款吗？",
            order=order,
            intent=intent,
            eligibility=eligibility,
            policy_result=policy_result,
            llm_client=fake_llm,
        )
        assert answer
        assert len(answer) > 0

    def test_generate_final_answer_no_order(self):
        """测试 generate_final_answer 在无订单时的工作。"""
        from examples.customer_support_agent import IntentResult

        intent = IntentResult(intent="unknown", confidence=0.3)
        policy_result = RetrievalResult(document_ids=[], scores=[], documents=[])

        fake_llm = FakeLLMClient()

        answer = generate_final_answer(
            user_input="你好？",
            order=None,
            intent=intent,
            eligibility=None,
            policy_result=policy_result,
            llm_client=fake_llm,
        )
        assert answer
        assert len(answer) > 0


# ================================================================
# 数据完整性测试
# ================================================================


class TestDataIntegrity:
    def test_all_orders_have_required_fields(self):
        """验证所有内置订单都有必填字段。"""
        for order_id, order in LOCAL_ORDERS.items():
            assert order.id == order_id
            assert order.product_name
            assert order.price > 0
            assert order.return_window_days > 0

    def test_all_policies_have_ids(self):
        """验证所有内置政策都有 ID。"""
        for policy in LOCAL_POLICIES:
            assert policy.id
            assert policy.id.startswith("POLICY-")
            assert policy.title
            assert policy.content
            assert len(policy.tags) > 0


# ================================================================
# Trace 上下文传播测试（Issue 1：trace_id 一致性）
# ================================================================


class _ListSpanExporter:
    """A simple in-memory span exporter for testing."""

    def __init__(self) -> None:
        self.spans: list = []

    def export(self, spans: list) -> None:
        self.spans.extend(spans)

    def shutdown(self) -> None:
        pass

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return True


@pytest.fixture
def test_tracer_and_exporter():
    """Create a tracer with a ListSpanExporter for test inspection."""
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import SimpleSpanProcessor

    exporter = _ListSpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    tracer = provider.get_tracer("test-tracer")
    return tracer, exporter, provider


class TestTraceContextConsistency:
    """验证在根 span context 内执行时所有子 span 共享同一个 trace_id。"""

    def test_all_child_spans_same_trace_id(self, test_tracer_and_exporter):
        """使用 InMemorySpanExporter 验证 root/tool/retrieval/skill/llm span 的 trace_id 一致。"""
        tracer, exporter, provider = test_tracer_and_exporter
        responses = {
            "我的订单 ORD-1001 可以退款吗？": "订单可退款。",
        }
        fake_llm = FakeLLMClient(responses=responses)

        # 在根 span context 内执行
        from examples.customer_support_agent import run_customer_support_case

        with tracer.start_as_current_span("customer_support.run") as root_span:
            root_trace_id = format(root_span.get_span_context().trace_id, "032x")

            # run_customer_support_case 内部使用 trace_llm/trace_tool 等 helper
            # 当传入了 tracer 且运行在当前激活的 span context 内时，
            # 所有子 span 自动继承同一个 trace id
            result = run_customer_support_case(
                "我的订单 ORD-1001 可以退款吗？",
                fake_llm,
                tracer=tracer,
            )

        # flush
        provider.force_flush()
        spans = exporter.spans
        assert len(spans) > 0, "应该至少有一个 span"

        # 确认所有 span 的 trace_id 一致
        trace_ids = set(span.get_span_context().trace_id for span in spans)
        assert len(trace_ids) == 1, f"所有 span 应共享同一 trace_id，实际: {trace_ids}"

        # 确认 root span 的 trace id 与子 span 一致
        root_span_id = root_trace_id
        for span in spans:
            span_tid = format(span.get_span_context().trace_id, "032x")
            assert span_tid == root_span_id, (
                f"span {span.name} 的 trace_id {span_tid} "
                f"与 root span 的 trace_id {root_span_id} 不一致"
            )

    def test_span_names_present(self, test_tracer_and_exporter):
        """验证 root span context 产生的 span 包含预期名称。"""
        tracer, exporter, provider = test_tracer_and_exporter
        fake_llm = FakeLLMClient()

        with tracer.start_as_current_span("customer_support.run"):
            run_customer_support_case(
                "我的订单 ORD-1001 可以退款吗？",
                fake_llm,
                tracer=tracer,
            )

        provider.force_flush()
        span_names = [s.name for s in exporter.spans]
        assert "lookup_order" in span_names
        assert "generate_final_answer" in span_names


# ================================================================
# 工具结果摘要测试（Issue 2：tool.result_summary 准确性）
# ================================================================


class TestToolResultSummary:
    """验证 lookup_order 的 tool.result_summary 准确反映查询结果。"""

    def test_existing_order_has_found_true(self, test_tracer_and_exporter):
        """ORD-1001 存在，result_summary 应包含 found=True。"""
        tracer, exporter, provider = test_tracer_and_exporter
        responses = {"ORD-1001": "可退款。"}
        fake_llm = FakeLLMClient(responses=responses)

        with tracer.start_as_current_span("test_root"):
            run_customer_support_case(
                "我的订单 ORD-1001 可以退款吗？",
                fake_llm,
                tracer=tracer,
            )

        provider.force_flush()
        lookup_span = None
        for span in exporter.spans:
            if span.name == "lookup_order":
                lookup_span = span
                break

        assert lookup_span is not None, "应找到 lookup_order span"
        summary = lookup_span.attributes.get("tool.result_summary", "")
        assert "found=True" in summary, f"result_summary 应包含 found=True，实际: {summary}"

    def test_unknown_order_has_found_false(self, test_tracer_and_exporter):
        """ORD-9999 不存在，result_summary 应包含 found=False。"""
        tracer, exporter, provider = test_tracer_and_exporter
        fake_llm = FakeLLMClient()

        with tracer.start_as_current_span("test_root"):
            run_customer_support_case(
                "ORD-9999 现在什么状态？",
                fake_llm,
                tracer=tracer,
            )

        provider.force_flush()
        lookup_span = None
        for span in exporter.spans:
            if span.name == "lookup_order":
                lookup_span = span
                break

        assert lookup_span is not None, "应找到 lookup_order span"
        summary = lookup_span.attributes.get("tool.result_summary", "")
        assert "found=False" in summary, f"result_summary 应包含 found=False，实际: {summary}"


# ================================================================
# 子 span 归属字段传播测试（trace_attrs → 各结构化子 span）
# ================================================================


class TestTraceAttrsPropagation:
    """验证 trace_attrs 正确传播到所有结构化子 span。"""

    SPAN_NAMES = ["lookup_order", "retrieve_policy", "check_refund_eligibility", "generate_final_answer"]

    def _collect_by_name(self, spans, name):
        return [s for s in spans if s.name == name]

    def test_all_child_spans_have_agent_id(self, test_tracer_and_exporter):
        """所有结构化子 span 都应包含 agentscope.agent.id。"""
        tracer, exporter, provider = test_tracer_and_exporter
        fake_llm = FakeLLMClient(responses={
            "我的订单 ORD-1001 可以退款吗？": "可退款。",
        })
        trace_attrs = {
            "agentscope.agent.id": "agent-uuid-123",
            "agentscope.agent.instance_id": "instance-uuid-456",
            "agentscope.run.id": "run-uuid-789",
        }

        with tracer.start_as_current_span("test_root"):
            from examples.customer_support_agent import run_customer_support_case
            run_customer_support_case(
                "我的订单 ORD-1001 可以退款吗？",
                fake_llm,
                tracer=tracer,
                trace_attrs=trace_attrs,
            )

        provider.force_flush()

        for span_name in self.SPAN_NAMES:
            spans = self._collect_by_name(exporter.spans, span_name)
            if not spans:
                continue  # some spans may not be created for certain inputs
            span = spans[0]
            assert span.attributes.get("agentscope.agent.id") == "agent-uuid-123", (
                f"span {span_name} 缺少 agentscope.agent.id"
            )
            assert span.attributes.get("agentscope.agent.instance_id") == "instance-uuid-456", (
                f"span {span_name} 缺少 agentscope.agent.instance_id"
            )
            assert span.attributes.get("agentscope.run.id") == "run-uuid-789", (
                f"span {span_name} 缺少 agentscope.run.id"
            )

    def test_trace_attrs_defaults_to_none(self, test_tracer_and_exporter):
        """不传 trace_attrs 时应正常执行（回归测试）。"""
        tracer, exporter, provider = test_tracer_and_exporter
        fake_llm = FakeLLMClient()

        with tracer.start_as_current_span("test_root"):
            from examples.customer_support_agent import run_customer_support_case
            run_customer_support_case(
                "我的订单 ORD-1001 可以退款吗？",
                fake_llm,
                tracer=tracer,
            )

        provider.force_flush()
        lookup_spans = self._collect_by_name(exporter.spans, "lookup_order")
        assert len(lookup_spans) > 0, "不传 trace_attrs 时 lookup_order span 仍应存在"
