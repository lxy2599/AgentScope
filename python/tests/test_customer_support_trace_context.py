"""Tests for customer support agent trace context management and latency."""

from examples.customer_support_agent import compute_latency_ms


def test_compute_latency_ms_is_positive():
    """Latency between two monotonic timestamps should be at least 1 ms."""
    assert compute_latency_ms(start_monotonic=1.0, end_monotonic=1.123) == 123


def test_compute_latency_ms_minimum_one():
    """Even identical timestamps should return 1 ms to avoid zero latency."""
    assert compute_latency_ms(start_monotonic=1.0, end_monotonic=1.0) == 1


def test_compute_latency_ms_zero_duration():
    """Very small duration should round up to 1 ms."""
    assert compute_latency_ms(start_monotonic=1.0, end_monotonic=1.0005) >= 1
