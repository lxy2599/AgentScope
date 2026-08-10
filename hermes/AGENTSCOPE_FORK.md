# AgentScope Hermes native fork provenance

This directory is the controlled Hermes fork used by AgentLab. It originated from
an AgentScope-managed snapshot so the initial source baseline and the matching
AgentScope SDK can be reviewed together; it is now an independent repository
under `AgentLab/hermes`.

- Fork source commit: `02a438eb5b04009bebfc3c23c60d03b347e6b133`
- Upstream Hermes baseline: `015718066ab8e9499c3caea3cda9f7ea469036fc`
- Upstream remote: `https://github.com/NousResearch/hermes-agent.git`
- Native direct-SDK integration points: `agent/agent_init.py`,
  `agent/conversation_loop.py`, and `agent/tool_executor.py`
- Native direct-SDK regressions: `tests/agent/test_agentscope_native_lifecycle.py`

Hermes directly imports the public `agentscope_sdk` Python API from its real
execution paths. It must not reintroduce a bridge, adapter, plugin, or observer
layer between those paths and the SDK.

When synchronizing with a new fork revision, update the complete source snapshot,
this provenance record, the SDK dependency, and the related AgentLab integration
documents in the same change.
