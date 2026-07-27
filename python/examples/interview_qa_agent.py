"""A minimal Ark-backed evaluation agent for the interview Q&A experiment.

Before running, set these values in your local shell (do not put them in this
file):

    export AGENTSCOPE_PROJECT_TOKEN='...'
    export ARK_API_KEY='...'

Then run:

    PYTHONPATH=python python python/examples/interview_qa_agent.py
"""

from __future__ import annotations

import json
import os
from typing import Any

from agentscope_sdk.client import RunnerClient
from agentscope_sdk.llm import LLMClient
from agentscope_sdk.runner import run_once
from agentscope_sdk.target import target

SERVER_URL = "http://127.0.0.1:8080"
TARGET_NAME = "interview-qa-agent"
TARGET_VERSION = "1.0.0"


def _as_text(value: Any) -> str:
    if isinstance(value, str):
        return value
    return json.dumps(value, ensure_ascii=False)


def main() -> None:
    project_token = os.environ.get("AGENTSCOPE_PROJECT_TOKEN", "")
    ark_api_key = os.environ.get("ARK_API_KEY", "")
    if not project_token or not ark_api_key:
        raise RuntimeError("Set AGENTSCOPE_PROJECT_TOKEN and ARK_API_KEY before starting the agent.")

    llm = LLMClient(
        provider="volcengine-ark",
        base_url="https://ark.cn-beijing.volces.com/api/coding/v3",
        api_key=ark_api_key,
        model="deepseek-v4-flash",
    )

    @target(name=TARGET_NAME, version=TARGET_VERSION)
    def interview_qa_agent(case_input: Any, _: dict[str, Any]) -> str:
        response = llm.generate_response(
            [
                {
                    "role": "system",
                    "content": (
                        "You are a precise interview Q&A assistant. "
                        "Answer the user's question directly and concisely. "
                        "Return only the final answer, without headings or explanations about your process."
                    ),
                },
                {"role": "user", "content": _as_text(case_input)},
            ],
            temperature=0,
        )
        return response.content.strip()

    runner = RunnerClient(
        server_url=SERVER_URL,
        project_token=project_token,
        runner_name="interview-qa-ark-runner",
    )
    processed = run_once(runner)
    print("Processed one evaluation job." if processed else "No queued evaluation job found.")


if __name__ == "__main__":
    main()
