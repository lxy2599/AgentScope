"""Backward-compatible import for the real Harbor Worker executor.

``BenchmarkWorker`` used to submit a synthetic ``pending`` result.  That is no
longer a valid benchmark outcome: only Harbor's official verifier may settle a
job.  New callers should use :class:`HarborJobExecutor` explicitly.
"""

from .harbor_executor import HarborJobExecutor

BenchmarkWorker = HarborJobExecutor

__all__ = ["BenchmarkWorker", "HarborJobExecutor"]
