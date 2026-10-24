import { rangeMs } from "./timeRange";

/** Minutes around a chart click shown in the logs view. */
export const AROUND_MS = 5 * 60_000;

/**
 * The search window: centred on a picked moment (chart click), otherwise
 * the chosen range ending now.
 */
export function logWindow(atMs: number | null, range: string, nowMs: number): { from: string; to: string } {
  if (atMs) {
    return { from: new Date(atMs - AROUND_MS).toISOString(), to: new Date(atMs + AROUND_MS).toISOString() };
  }
  return { from: new Date(nowMs - rangeMs(range)).toISOString(), to: new Date(nowMs).toISOString() };
}

/** Client-side mirror of the server's term rule, for instant feedback. */
export function searchProblem(q: string): string | null {
  const terms = q.match(/"[^"]*"|\S+/g) ?? [];
  for (const t of terms) {
    const term = t.startsWith('"') ? t.slice(1, -1) : t;
    if (t === "OR") continue;
    if ([...term].length < 3) return `"${term}" is shorter than 3 characters`;
  }
  return null;
}
