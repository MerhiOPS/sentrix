import type { CoreFrame, Sample } from "../api/endpoints";
import { TimeBuffer, type Point } from "../lib/ringbuffer";

/** Points kept per host: 30 minutes at a 5 s interval. */
export const POINTS_PER_HOST = 360;

export const CHART_KEYS = ["cpu", "mem"] as const;

export function toPoint(s: Sample): Point {
  return { tsMs: s.ts_ms, values: { cpu: s.cpu, mem: s.mem, disk: s.disk, load1: s.load1 } };
}

/** Per-host buffers fed by WebSocket frames; pure, so it is testable. */
export class LiveFeed {
  readonly buffers = new Map<string, TimeBuffer>();

  apply(frame: CoreFrame): void {
    let b = this.buffers.get(frame.host_id);
    if (!b) {
      b = new TimeBuffer(POINTS_PER_HOST);
      this.buffers.set(frame.host_id, b);
    }
    b.upsert(frame.samples.map(toPoint));
  }

  /**
   * Where a reconnect resumes: the oldest "latest" across hosts, so no host
   * is left with a gap. Newer hosts get overlap, which the buffers dedupe.
   */
  resumeFromMs(): number {
    let min = 0;
    for (const b of this.buffers.values()) {
      const l = b.latestMs();
      if (l > 0 && (min === 0 || l < min)) min = l;
    }
    return min;
  }
}
