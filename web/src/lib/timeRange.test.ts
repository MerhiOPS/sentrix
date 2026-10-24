import { describe, expect, it } from "vitest";
import { rangeMs, window } from "./timeRange";

describe("timeRange", () => {
  it("unknown keys fall back to 1 hour", () => {
    expect(rangeMs("bogus")).toBe(3600_000);
  });
  it("window ends now, rounded to the second", () => {
    const w = window("1h", Date.UTC(2026, 9, 24, 12, 0, 0, 750));
    expect(w.to).toBe("2026-10-24T12:00:00.000Z");
    expect(w.from).toBe("2026-10-24T11:00:00.000Z");
  });
});
