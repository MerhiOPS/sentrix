import { describe, expect, it } from "vitest";
import type { Sample } from "../api/endpoints";
import { LiveFeed } from "./liveFeed";

const s = (sec: number, cpu = 1): Sample => ({
  ts_ms: sec * 1000, cpu, mem: 2, disk: 3, load1: 0, load5: 0, load15: 0,
  mem_used: 0, uptime: 0, net_rx: 0, net_tx: 0, disk_r: 0, disk_w: 0,
});

describe("LiveFeed", () => {
  it("resumes from the oldest host so no host gets a gap", () => {
    const f = new LiveFeed();
    expect(f.resumeFromMs()).toBe(0);
    f.apply({ type: "core", host_id: "a", samples: [s(10), s(20)] });
    f.apply({ type: "core", host_id: "b", samples: [s(5)] });
    expect(f.resumeFromMs()).toBe(5000);
  });
  it("a reconnect backfill produces no duplicate points", () => {
    const f = new LiveFeed();
    f.apply({ type: "core", host_id: "a", samples: [s(1), s(2), s(3)] });
    f.apply({ type: "core", host_id: "a", samples: [s(2, 9), s(3, 9), s(4)] }); // overlap
    const [xs, cpu] = f.buffers.get("a")!.columns(["cpu"]);
    expect(xs).toEqual([1, 2, 3, 4]);
    expect(cpu).toEqual([1, 9, 9, 1]);
  });
});
