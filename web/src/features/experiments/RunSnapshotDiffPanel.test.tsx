import { describe, expect, it } from "vitest";
import { buildSnapshotDiff } from "./RunSnapshotDiffPanel";

describe("buildSnapshotDiff", () => {
  it("returns stable recursive paths and omits unchanged values", () => {
    const rows = buildSnapshotDiff(
      {
        model: { name: "glm", temperature: 0.2 },
        prompt: { version: "v1" },
      },
      {
        model: { name: "glm", temperature: 0.4 },
        prompt: { version: "v2" },
      },
    );

    expect(rows.map((row) => row.path)).toEqual([
      "model.temperature",
      "prompt.version",
    ]);
  });
});
