# AgentLab Hermes migration provenance

This repository was initialized from the vendored Hermes source snapshot at
`agentscope/integrations/hermes-agent/` in the AgentScope commit recorded below,
then extracted into the independent `AgentLab/hermes` Git repository.

- AgentScope snapshot commit: `06a0dd389b88c8ed5e3f2c530c34ad0d79c0be2e`
- Original Hermes fork commit: see `AGENTSCOPE_FORK.md`
- Upstream baseline: see `AGENTSCOPE_FORK.md`

The AgentLab integration calls the public `agentscope_sdk` Python API directly
from Hermes source execution points. The historical bridge implementation was
migration-only material and has been removed from this repository; it must not be
restored as a production runtime dependency.
