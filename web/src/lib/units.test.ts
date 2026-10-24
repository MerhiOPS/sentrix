import { describe, expect, it } from "vitest";
import { formatUnit } from "./units";

describe("formatUnit", () => {
  it("formats each unit", () => {
    expect(formatUnit(42.4, "percent")).toBe("42%");
    expect(formatUnit(2048, "bytes")).toBe("2.0 KiB");
    expect(formatUnit(1536, "rate")).toBe("1.5 KiB/s");
    expect(formatUnit(0.5, "load")).toBe("0.50");
    expect(formatUnit(null, "rate")).toBe("—");
  });
});
