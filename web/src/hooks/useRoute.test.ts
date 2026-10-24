import { describe, expect, it } from "vitest";
import { hostHref, logsHref, parseRoute } from "./useRoute";

describe("routes", () => {
  it("parses host pages with defaults", () => {
    expect(parseRoute("#/hosts/abc")).toEqual({ page: "host", hostId: "abc", tab: "cpu", range: "1h" });
    expect(parseRoute(hostHref("abc", "disk", "7d"))).toEqual({ page: "host", hostId: "abc", tab: "disk", range: "7d" });
  });
  it("chart clicks carry host and time to the logs route", () => {
    expect(parseRoute(logsHref("abc", 1_790_000_000_123.4))).toEqual({ page: "logs", hostId: "abc", atMs: 1_790_000_000_123 });
  });
  it("anything else is the fleet", () => {
    expect(parseRoute("")).toEqual({ page: "fleet" });
    expect(parseRoute("#/nope")).toEqual({ page: "fleet" });
  });
});
