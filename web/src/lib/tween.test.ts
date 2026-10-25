import { describe, expect, it } from "vitest";
import { easeOut, tweenAt } from "./tween";

describe("tween", () => {
  it("starts at a, ends at b, and clamps outside 0..1", () => {
    expect(tweenAt(10, 20, 0)).toBe(10);
    expect(tweenAt(10, 20, 1)).toBe(20);
    expect(tweenAt(10, 20, 2)).toBe(20);
    expect(easeOut(-1)).toBe(0);
  });
  it("eases out: more than halfway at the midpoint", () => {
    expect(tweenAt(0, 100, 0.5)).toBeGreaterThan(50);
  });
});
