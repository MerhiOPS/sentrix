import { describe, expect, it } from "vitest";
import { describeRule, formatMinutes } from "./ruleText";

describe("describeRule", () => {
  it("reads a threshold rule as a sentence", () => {
    expect(describeRule({ kind: "threshold", metric: "cpu", aggregation: "max", comparator: "gt", threshold: 90, duration_s: 300, on_no_data: "hold" }))
      .toBe("max CPU > 90% for 5m · no data: hold");
  });
  it("leaves the aggregation off a rate", () => {
    expect(describeRule({ kind: "threshold", metric: "net_rx_rate", aggregation: "avg", comparator: "lt", threshold: 10, duration_s: 3600, on_no_data: "resolve" }))
      .toBe("Network in < 10 B/s for 1h · no data: resolve");
  });
  it("reads a no_data rule", () => {
    expect(describeRule({ kind: "no_data", metric: "", aggregation: "avg", comparator: "gt", threshold: 0, duration_s: 600, on_no_data: "hold" }))
      .toBe("no data for 10m");
  });
});

describe("formatMinutes", () => {
  it("formats whole minutes", () => {
    expect(formatMinutes(60)).toBe("1m");
    expect(formatMinutes(5400)).toBe("1h 30m");
    expect(formatMinutes(7200)).toBe("2h");
  });
});
