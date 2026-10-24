import { describe, expect, it } from "vitest";
import { usageLevel } from "./usage";

describe("usageLevel", () => {
  it("warns from 80 % and flags the limit", () => {
    expect(usageLevel(79, 100)).toBe("ok");
    expect(usageLevel(80, 100)).toBe("stale");
    expect(usageLevel(100, 100)).toBe("error");
    expect(usageLevel(0, 0)).toBe("ok");
    expect(usageLevel(1, 0)).toBe("error");
  });
});
