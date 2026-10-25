import { describe, expect, it } from "vitest";
import { OTHER, SERIES, seriesColor } from "./palette";

describe("seriesColor", () => {
  it("takes slots in fixed order and folds past the last instead of cycling", () => {
    expect(seriesColor(0)).toBe(SERIES[0]);
    expect(seriesColor(SERIES.length - 1)).toBe(SERIES[SERIES.length - 1]);
    expect(seriesColor(SERIES.length)).toBe(OTHER);
  });
});
