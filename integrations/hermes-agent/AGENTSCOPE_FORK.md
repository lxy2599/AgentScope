# AgentScope Hermes native fork provenance

This directory is a vendored source snapshot of the controlled Hermes fork used by
AgentScope native observability. It is intentionally tracked inside the AgentScope
repository so the bridge, its real execution-point changes, and the corresponding
AgentScope SDK version can be reviewed together.

- Fork source commit: `02a438eb5b04009bebfc3c23c60d03b347e6b133`
- Upstream Hermes baseline: `015718066ab8e9499c3caea3cda9f7ea469036fc`
- Upstream remote: `https://github.com/NousResearch/hermes-agent.git`
- Native bridge: `hermes_agentscope/bridge.py`
- Real integration points: `agent/agent_init.py`, `agent/conversation_loop.py`,
  and `agent/tool_executor.py`
- Native regressions: `tests/test_agentscope_bridge.py` and
  `tests/agent/test_agentscope_native_lifecycle.py`

The source is imported without a nested `.git` directory. When synchronizing with a
new fork revision, update the complete source snapshot, this provenance record, and
the related AgentScope integration documents in the same change. Do not replace the
native bridge with the external `HermesAdapter` compatibility path.
