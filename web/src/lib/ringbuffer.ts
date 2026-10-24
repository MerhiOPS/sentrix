/** One timestamped point with named numeric values. */
export interface Point {
  tsMs: number;
  values: Record<string, number>;
}

/**
 * A bounded, time-ordered buffer of points keyed by timestamp. Upserting a
 * timestamp that is already present replaces it — reconnect backfill
 * overlaps what the chart already has, and must not duplicate it (OLD B7).
 */
export class TimeBuffer {
  private points: Point[] = [];

  constructor(private readonly capacity: number) {}

  upsert(incoming: Point[]): void {
    for (const p of incoming) {
      const i = this.lowerBound(p.tsMs);
      if (i < this.points.length && this.points[i]!.tsMs === p.tsMs) {
        this.points[i] = p;
      } else {
        this.points.splice(i, 0, p);
      }
    }
    if (this.points.length > this.capacity) {
      this.points.splice(0, this.points.length - this.capacity);
    }
  }

  /** Newest timestamp held, or 0 when empty — what to resume from. */
  latestMs(): number {
    return this.points.at(-1)?.tsMs ?? 0;
  }

  /** The newest point, if any. */
  latest(): Point | undefined {
    return this.points.at(-1);
  }

  size(): number {
    return this.points.length;
  }

  /** Columnar data for uPlot: seconds on the x axis, one column per key. */
  columns(keys: string[]): [number[], ...(number | null)[][]] {
    const xs = this.points.map((p) => p.tsMs / 1000);
    const cols = keys.map((k) => this.points.map((p) => p.values[k] ?? null));
    return [xs, ...cols];
  }

  private lowerBound(ts: number): number {
    let lo = 0;
    let hi = this.points.length;
    while (lo < hi) {
      const mid = (lo + hi) >>> 1;
      if (this.points[mid]!.tsMs < ts) lo = mid + 1;
      else hi = mid;
    }
    return lo;
  }
}
