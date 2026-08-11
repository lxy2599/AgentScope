#!/usr/bin/env python3
"""Run one observed OpenHands task, then remain alive for AgentScope heartbeats."""

from __future__ import annotations

import os
import signal
import sys
import time
from pathlib import Path

from pydantic import SecretStr


_RUNNING = True


def _required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"missing required environment variable: {name}")
    return value


def _stop(_signum: int, _frame: object) -> None:
    global _RUNNING
    _RUNNING = False


def main() -> int:
    workspace = Path(os.environ.get("OPENHANDS_OBSERVED_WORKSPACE", ".")).resolve()
    workspace.mkdir(parents=True, exist_ok=True)
    prompt = os.environ.get(
        "OPENHANDS_OBSERVED_PROMPT",
        "Reply with exactly: AgentScope OpenHands is online. Do not use tools.",
    )

    api_key = _required("OPENHANDS_LLM_API_KEY")
    base_url = _required("OPENHANDS_LLM_BASE_URL")
    model = _required("OPENHANDS_LLM_MODEL")

    # These are normal OpenHands APIs. The controlled fork activates its native
    # AgentScope bridge from Conversation.run(); this process never imports or
    # drives AgentScope Runtime directly.
    from openhands.sdk import Agent, Conversation, LLM
    from openhands.sdk.agentscope import get_agentscope_bridge

    llm = LLM(
        model=model,
        api_key=SecretStr(api_key),
        base_url=base_url,
        usage_id="agentscope-observed-openhands",
        stream=True,
    )
    conversation = Conversation(
        agent=Agent(llm=llm, tools=[]),
        workspace=str(workspace),
        visualizer=None,
        max_iteration_per_run=3,
    )
    signal.signal(signal.SIGTERM, _stop)
    signal.signal(signal.SIGINT, _stop)

    try:
        print("[observed-openhands] running initial task to activate AgentScope", flush=True)
        conversation.send_message(prompt)
        conversation.run()
        run_id = get_agentscope_bridge().last_run_id(str(conversation.state.id))
        if not run_id:
            raise RuntimeError("OpenHands native AgentScope bridge did not create a run")
        print(
            f"[observed-openhands] online; AgentScope run={run_id}; waiting for SIGINT/SIGTERM",
            flush=True,
        )
        while _RUNNING:
            time.sleep(1)
    finally:
        conversation.close()
        print("[observed-openhands] stopped", flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"[observed-openhands] error: {error}", file=sys.stderr, flush=True)
        raise SystemExit(1)
