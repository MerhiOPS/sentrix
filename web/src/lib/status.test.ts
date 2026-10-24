import { describe, expect, it } from "vitest";
import { liveStatus, statusLabel } from "./status";

describe("liveStatus", () => {
  const now = 1_000_000;
  it("matches the server's thresholds", () => {
    expect(liveStatus(now - 15_000, 5000, now, false)).toBe("ok");
    expect(liveStatus(now - 16_000, 5000, now, false)).toBe("stale");
    expect(liveStatus(now - 51_000, 5000, now, false)).toBe("no_data");
    expect(liveStatus(0, 5000, now, false)).toBe("no_data");
    expect(liveStatus(now, 5000, now, true)).toBe("disabled");
  });
  it("never claims a host is down", () => {
    for (const label of Object.values(statusLabel)) {
      expect(label.toLowerCase()).not.toContain("down");
    }
  });
});
