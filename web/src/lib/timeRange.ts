/** Chart window presets. The server picks raw / 1m / 1h data to match. */
export const RANGES = [
  { key: "1h", label: "1 hour", ms: 3600_000 },
  { key: "6h", label: "6 hours", ms: 6 * 3600_000 },
  { key: "24h", label: "24 hours", ms: 24 * 3600_000 },
  { key: "7d", label: "7 days", ms: 7 * 86400_000 },
  { key: "30d", label: "30 days", ms: 30 * 86400_000 },
] as const;

export type RangeKey = (typeof RANGES)[number]["key"];

export function rangeMs(key: string): number {
  return RANGES.find((r) => r.key === key)?.ms ?? RANGES[0].ms;
}

/** [from, to] as RFC 3339, ending now, rounded down to the second. */
export function window(key: string, nowMs: number): { from: string; to: string } {
  const to = Math.floor(nowMs / 1000) * 1000;
  return { from: new Date(to - rangeMs(key)).toISOString(), to: new Date(to).toISOString() };
}
