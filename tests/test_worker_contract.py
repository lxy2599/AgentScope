"""Compatibility imports for the Worker package.

The former contract worker emitted a synthetic ``pending`` result.  Harbor
execution now lives in ``HarborJobExecutor`` and only emits official verifier
outcomes; lifecycle behaviour is covered by ``test_harbor_executor.py``.
"""

from benchmark_worker.harbor_executor import HarborJobExecutor
from benchmark_worker.worker import BenchmarkWorker


def test_legacy_worker_import_points_to_real_harbor_executor() -> None:
    assert BenchmarkWorker is HarborJobExecutor
