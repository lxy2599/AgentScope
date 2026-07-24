"""Explicit Runtime Contract v2 snapshot builder."""

from __future__ import annotations

import copy
import hashlib
from typing import Any


class RunSnapshotBuilder:
    """Build a reproducible run snapshot without reading process environment."""

    def __init__(self) -> None:
        self._payload: dict[str, Any] = {
            "runtime_contract": {"schema_version": "runtime-contract-v2"},
            "models": [],
            "prompts": [],
            "tools": [],
            "skills": [],
            "mcp_servers": [],
            "knowledge_bases": [],
            "dependencies": [],
            "feature_flags": {},
            "ablation_flags": {},
            "capture_policy": {},
        }

    def framework(
        self,
        name: str,
        *,
        version: str = "",
        upstream_commit: str = "",
        sdk_version: str = "",
    ) -> RunSnapshotBuilder:
        self._payload["framework"] = {
            "name": name,
            "version": version,
            "upstream_commit": upstream_commit,
            "sdk_version": sdk_version,
        }
        return self

    def model(self, provider: str, name: str, **parameters: Any) -> RunSnapshotBuilder:
        self._payload["models"].append({"provider": provider, "name": name, "parameters": parameters})
        return self

    def prompt(self, name: str, *, version: str = "", content_hash: str = "") -> RunSnapshotBuilder:
        self._payload["prompts"].append({"name": name, "version": version, "content_hash": content_hash})
        return self

    def capability(
        self,
        kind: str,
        name: str,
        *,
        version: str = "",
        schema_hash: str = "",
    ) -> RunSnapshotBuilder:
        target = {
            "tool": "tools",
            "skill": "skills",
            "mcp": "mcp_servers",
            "knowledge_base": "knowledge_bases",
        }.get(kind)
        if target is None:
            raise ValueError(f"unsupported capability kind: {kind}")
        self._payload[target].append({"name": name, "version": version, "schema_hash": schema_hash})
        return self

    def dependency(self, name: str, version: str) -> RunSnapshotBuilder:
        self._payload["dependencies"].append({"name": name, "version": version})
        return self

    def flags(
        self,
        *,
        feature_flags: dict[str, Any] | None = None,
        ablation_flags: dict[str, Any] | None = None,
    ) -> RunSnapshotBuilder:
        self._payload["feature_flags"] = copy.deepcopy(feature_flags or {})
        self._payload["ablation_flags"] = copy.deepcopy(ablation_flags or {})
        return self

    def capture_policy(self, **policy: Any) -> RunSnapshotBuilder:
        self._payload["capture_policy"] = copy.deepcopy(policy)
        return self

    def input_hash(self, value: str) -> RunSnapshotBuilder:
        self._payload["input_hash"] = hashlib.sha256(value.encode()).hexdigest()
        return self

    def extra(self, **values: Any) -> RunSnapshotBuilder:
        self._payload.update(copy.deepcopy(values))
        return self

    def build(self) -> dict[str, Any]:
        return copy.deepcopy(self._payload)
