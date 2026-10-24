import { describe, expect, it } from "vitest";
import { formatAge, formatBytes, formatPercent } from "./format";

describe("format", () => {
  it("bytes", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(6.4 * 2 ** 30)).toBe("6.4 GiB");
    expect(formatBytes(-1)).toBe("—");
  });
  it("percent treats null as no data", () => {
    expect(formatPercent(42.345)).toBe("42.3%");
    expect(formatPercent(null)).toBe("—");
  });
  it("age", () => {
    expect(formatAge(1000)).toBe("just now");
    expect(formatAge(42_000)).toBe("42s ago");
    expect(formatAge(3 * 3600_000)).toBe("3h ago");
  });
});
