const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

/** Bytes in binary units, one decimal: 1536 → "1.5 KiB". */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "—";
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i === 0 ? `${v} B` : `${v.toFixed(1)} ${units[i]}`;
}

/** Percent with one decimal; null (no data) renders as a dash. */
export function formatPercent(v: number | null | undefined): string {
  return v === null || v === undefined || !Number.isFinite(v) ? "—" : `${v.toFixed(1)}%`;
}

/** "3m ago" style age; under 5 s is "just now". */
export function formatAge(ms: number): string {
  if (ms < 5000) return "just now";
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  return h < 48 ? `${h}h ago` : `${Math.round(h / 24)}d ago`;
}
