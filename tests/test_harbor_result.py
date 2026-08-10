from __future__ import annotations

import json
from pathlib import Path

import pytest

from benchmark_worker.harbor_result import HarborVerifierError, parse_official_harbor_verdict


def write_result(root: Path, payload: dict) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / "result.json").write_text(json.dumps(payload), encoding="utf-8")


def test_parse_harbor_official_verifier_reward_from_result_json(tmp_path: Path) -> None:
    write_result(tmp_path, {"verifier_result": {"rewards": {"reward": 1.0, "checks": 3, "api_key": "excluded"}}})
    verdict = parse_official_harbor_verdict(tmp_path)
    assert verdict.status == "passed"
    assert verdict.score == 1.0
    assert verdict.metrics == {"reward": 1.0, "checks": 3}


def test_parse_harbor_reward_artifact_fallback(tmp_path: Path) -> None:
    write_result(tmp_path, {"agent_result": {"status": "done"}})
    verifier = tmp_path / "verifier"
    verifier.mkdir()
    (verifier / "reward.txt").write_text("0.0\n", encoding="utf-8")
    verdict = parse_official_harbor_verdict(tmp_path)
    assert verdict.status == "failed"
    assert verdict.score == 0.0


def test_parse_harbor_result_refuses_agent_success_without_verifier(tmp_path: Path) -> None:
    write_result(tmp_path, {"agent_result": {"status": "succeeded", "final_answer": "trust me"}})
    with pytest.raises(HarborVerifierError, match="reward is missing"):
        parse_official_harbor_verdict(tmp_path)


@pytest.mark.parametrize("reward", [-0.1, float("inf")])
def test_parse_harbor_result_rejects_invalid_reward(tmp_path: Path, reward: float) -> None:
    write_result(tmp_path, {"verifier_result": {"rewards": {"reward": reward}}})
    with pytest.raises(HarborVerifierError, match="reward is invalid"):
        parse_official_harbor_verdict(tmp_path)


def test_parse_official_harbor_verdict_uses_single_trial_result_below_aggregate_job_result(tmp_path: Path) -> None:
    (tmp_path / "result.json").write_text(json.dumps({"n_total_trials": 1}), encoding="utf-8")
    trial = tmp_path / "task__trial"
    trial.mkdir()
    (trial / "result.json").write_text(
        json.dumps({"verifier_result": {"rewards": {"reward": 1.0}}}), encoding="utf-8"
    )

    verdict = parse_official_harbor_verdict(tmp_path)

    assert verdict.status == "passed"
    assert verdict.score == 1.0
    assert verdict.result_path == trial / "result.json"


def test_parse_official_harbor_verdict_rejects_ambiguous_multiple_trial_rewards(tmp_path: Path) -> None:
    (tmp_path / "result.json").write_text(json.dumps({"n_total_trials": 2}), encoding="utf-8")
    for name in ("task__a", "task__b"):
        trial = tmp_path / name
        trial.mkdir()
        (trial / "result.json").write_text(
            json.dumps({"verifier_result": {"rewards": {"reward": 1.0}}}), encoding="utf-8"
        )

    with pytest.raises(HarborVerifierError, match="ambiguous"):
        parse_official_harbor_verdict(tmp_path)
