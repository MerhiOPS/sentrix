/** How close a usage figure is to its limit: warn from 80 %. */
export function usageLevel(used: number, limit: number): "ok" | "stale" | "error" {
  if (limit <= 0) return used > 0 ? "error" : "ok";
  const r = used / limit;
  return r >= 1 ? "error" : r >= 0.8 ? "stale" : "ok";
}
