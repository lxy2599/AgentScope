# AgentScope OpenHands native fork provenance

This directory is a vendored source snapshot of the controlled OpenHands Software
Agent SDK fork used by AgentScope native observability. It is intentionally tracked
inside the AgentScope repository so the optional bridge, real execution-point
changes, and matching AgentScope SDK API can be reviewed together.

- Fork source commit: pending first AgentScope native integration commit
- Upstream OpenHands baseline: `a375c66aaf29c8d3b6553c9164e65ed917149e81`
- Upstream remote: `https://github.com/OpenHands/software-agent-sdk`
- Native bridge: `openhands-sdk/openhands/sdk/agentscope/bridge.py`
- Real integration points: `conversation/impl/local_conversation.py`,
  `agent/agent.py`, and `llm/llm.py`
- Native regressions: `tests/sdk/test_agentscope_native_lifecycle.py`

The source is imported without a nested `.git` directory. AgentScope observability
is optional, disabled by default, lazily imported, and fail-open. A business project
uses this fork as normal OpenHands: it must not create wrappers, callbacks, or
explicit AgentScope Run/Turn objects. When synchronizing with a new upstream
revision, update this provenance record, re-freeze the execution call graph, and
run the native regressions before publishing.
