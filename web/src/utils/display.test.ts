import { describe, expect, it } from "vitest";
import { displayJsonValue } from "./display";

describe("displayJsonValue", () => {
  it("renders structured values without returning React objects", () => {
    expect(displayJsonValue({ input: "query" })).toContain('"input"');
    expect(displayJsonValue(["one", "two"])).toContain("one");
  });

  it("parses JSON strings and preserves plain text", () => {
    expect(displayJsonValue('"hello"')).toBe("hello");
    expect(displayJsonValue("plain text")).toBe("plain text");
  });
});
