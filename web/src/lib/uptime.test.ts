import { describe, expect, it } from "vitest";
import { barLabel, barLevel, blankFields, certDaysLeft, formatDuration, formatLatency, formatUptime, latencySeries, toInput } from "./uptime";

describe("uptime formatting", () => {
  it("never rounds up to 100 % and shows no data as a dash", () => {
    expect(formatUptime(1)).toBe("100%");
    expect(formatUptime(0.99996)).toBe("99.99%");
    expect(formatUptime(0.5)).toBe("50.00%");
    expect(formatUptime(undefined)).toBe("—");
  });
  it("shows a sub-millisecond mean as measured, and nothing measured as a dash", () => {
    expect(formatLatency(0)).toBe("<1 ms");
    expect(formatLatency(84)).toBe("84 ms");
    expect(formatLatency(null)).toBe("—");
  });
  it("formats durations at a useful precision", () => {
    expect(formatDuration(45)).toBe("45s");
    expect(formatDuration(200)).toBe("3m 20s");
    expect(formatDuration(7500)).toBe("2h 5m");
    expect(formatDuration(3 * 86_400 + 4 * 3600)).toBe("3d 4h");
  });
  it("treats a day without monitored time as a gap, not as up", () => {
    expect(barLevel({ day: "2026-10-30", up_s: 0, down_s: 0, failures: 0 })).toBe("gap");
    expect(barLevel({ day: "2026-10-30", up_s: 86_400, down_s: 0, failures: 2 })).toBe("good");
    expect(barLevel({ day: "2026-10-30", up_s: 86_000, down_s: 400, failures: 3 })).toBe("warning");
    expect(barLevel({ day: "2026-10-30", up_s: 80_000, down_s: 6400, failures: 9 })).toBe("critical");
    expect(barLabel({ day: "2026-10-30", up_s: 0, down_s: 0, failures: 0 })).toBe("2026-10-30: no data");
    expect(barLabel({ day: "2026-10-30", up_s: 86_000, down_s: 400, failures: 1 })).toBe("2026-10-30: 99.53% up, 6m 40s down, 1 failed check");
  });
  it("counts certificate days left", () => {
    const now = Date.parse("2026-11-01T00:00:00Z");
    expect(certDaysLeft("2026-11-15T12:00:00Z", now)).toBe(14);
    expect(certDaysLeft("2026-10-31T00:00:00Z", now)).toBe(-1);
    expect(certDaysLeft(null, now)).toBeNull();
  });
  it("draws failed and unknown checks as gaps in the latency chart", () => {
    const s = latencySeries([
      { at: "2026-11-01T00:00:00Z", outcome: "up", error: null, latency_ms: 80, ttfb_ms: 20, status: 200, loss: null, cert_expires_at: null },
      { at: "2026-11-01T00:01:00Z", outcome: "down", error: "timeout", latency_ms: null, ttfb_ms: null, status: null, loss: null, cert_expires_at: null },
      { at: "2026-11-01T00:02:00Z", outcome: "unknown", error: "connect", latency_ms: null, ttfb_ms: null, status: null, loss: null, cert_expires_at: null },
    ]);
    expect(s.values).toEqual([80, null, null]);
  });
});

describe("monitor form body", () => {
  it("sends http options only for http monitors", () => {
    const tcp = toInput({ ...blankFields, kind: "tcp", name: " db ", target: "db:5432", keyword: "x", keywordMode: "present" });
    expect(tcp).toEqual({ name: "db", kind: "tcp", target: "db:5432", interval_s: 60, timeout_s: 10, confirm_retries: 1, channels: [] });
  });
  it("turns certificate warnings off for plain http and drops an unused keyword", () => {
    const body = toInput({ ...blankFields, name: "a", target: "http://a.example/", keyword: "stale" });
    expect(body.cert_warn_days).toBe(0);
    expect(body.keyword).toBe("");
    expect(toInput({ ...blankFields, name: "a", target: "https://a.example/" }).cert_warn_days).toBe(14);
  });
});
