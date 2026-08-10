"""Official Harbor verifier artifact parsing.

Only a verifier-produced reward is allowed to turn an AgentScope eval_job into
an official passed/failed result. Agent final text, process exit status, and
sidecar state are deliberately insufficient.
"""

from __future__ import annotations

import json
import math
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping


class HarborVerifierError(RuntimeError):
    """A safe error indicating absent or malformed official verifier evidence."""


@dataclass(frozen=True)
class HarborVerifierVerdict:
    status: str
    score: float
    metrics: Mapping[str, Any]
    result_path: Path


def parse_official_harbor_verdict(job_directory: str | Path) -> HarborVerifierVerdict:
    """Read an official Harbor verifier reward from exactly one trial result.

    ``harbor run`` writes a job-level aggregate ``result.json`` plus one
    subdirectory per trial.  The aggregate intentionally has no verifier
    reward, so accept its child trial result only when exactly one candidate
    contains official verifier evidence.  This remains fail-closed for batch
    jobs: multiple rewarded trial results are ambiguous to a single eval job.
    """

    root = Path(job_directory)
    result_paths = [root / "result.json", *sorted(root.glob("*/result.json"))]
    candidates: list[tuple[Path, Mapping[str, Any], float]] = []
    for result_path in result_paths:
        if not result_path.is_file():
            continue
        result = _load_json_object(result_path)
        reward = _result_reward(result, result_path.parent)
        if reward is not None:
            candidates.append((result_path, result, reward))
    if not candidates:
        raise HarborVerifierError("official verifier reward is missing")
    if len(candidates) != 1:
        raise HarborVerifierError("official verifier reward is ambiguous")

    result_path, result, reward = candidates[0]
    if not math.isfinite(reward) or reward < 0:
        raise HarborVerifierError("official verifier reward is invalid")
    metrics = _metrics(result)
    # Terminal-Bench's official verifier is binary. Preserve fractional reward
    # as score for future Harbor suites, but only a full reward is a pass.
    return HarborVerifierVerdict("passed" if reward >= 1.0 else "failed", reward, metrics, result_path)


def _result_reward(result: Mapping[str, Any], result_directory: Path) -> float | None:
    reward = _nested_number(result, ("verifier_result", "rewards", "reward"))
    if reward is None:
        reward = _nested_number(result, ("verifier_result", "reward"))
    if reward is None:
        reward = _nested_number(result, ("reward",))
    if reward is None:
        reward = _reward_artifact(result_directory)
    return reward


def _load_json_object(path: Path) -> Mapping[str, Any]:
    try:
        decoded = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise HarborVerifierError("official Harbor result.json is unavailable") from exc
    if not isinstance(decoded, Mapping):
        raise HarborVerifierError("official Harbor result.json must be an object")
    return decoded


def _nested_number(value: Mapping[str, Any], path: tuple[str, ...]) -> float | None:
    current: Any = value
    for key in path:
        if not isinstance(current, Mapping):
            return None
        current = current.get(key)
    if isinstance(current, bool) or not isinstance(current, (int, float)):
        return None
    return float(current)


def _reward_artifact(root: Path) -> float | None:
    reward_json = root / "verifier" / "reward.json"
    if reward_json.is_file():
        try:
            decoded = json.loads(reward_json.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError):
            return None
        if isinstance(decoded, Mapping):
            for key in ("reward", "score"):
                candidate = decoded.get(key)
                if not isinstance(candidate, bool) and isinstance(candidate, (int, float)):
                    return float(candidate)
    reward_txt = root / "verifier" / "reward.txt"
    if reward_txt.is_file():
        try:
            return float(reward_txt.read_text(encoding="utf-8").strip())
        except (OSError, UnicodeDecodeError, ValueError):
            return None
    return None


def _metrics(result: Mapping[str, Any]) -> Mapping[str, Any]:
    verifier = result.get("verifier_result")
    if not isinstance(verifier, Mapping):
        return {}
    rewards = verifier.get("rewards")
    if isinstance(rewards, Mapping):
        return {str(key): value for key, value in rewards.items() if not _secret_like_key(str(key))}
    metrics = verifier.get("metrics")
    if isinstance(metrics, Mapping):
        return {str(key): value for key, value in metrics.items() if not _secret_like_key(str(key))}
    return {}


def _secret_like_key(key: str) -> bool:
    compact = "".join(ch for ch in key.lower() if ch.isalnum())
    return compact.endswith(("token", "secret", "password", "apikey", "credential"))
