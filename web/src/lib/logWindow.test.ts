import { describe, expect, it } from "vitest";
import { AROUND_MS, logWindow, searchProblem } from "./logWindow";

describe("logWindow", () => {
  it("centres on a chart click", () => {
    const at = Date.UTC(2026, 9, 24, 12, 0, 0);
    const w = logWindow(at, "1h", 0);
    expect(new Date(w.to).getTime() - new Date(w.from).getTime()).toBe(2 * AROUND_MS);
  });
  it("otherwise ends now", () => {
    const now = Date.UTC(2026, 9, 24, 12, 0, 0);
    expect(logWindow(null, "1h", now).to).toBe("2026-10-24T12:00:00.000Z");
  });
});

describe("searchProblem", () => {
  it("mirrors the 3-character rule", () => {
    expect(searchProblem("timeout")).toBeNull();
    expect(searchProblem("refused OR timeout")).toBeNull();
    expect(searchProblem('"by peer"')).toBeNull();
    expect(searchProblem("ab timeout")).toContain('"ab"');
  });
});
