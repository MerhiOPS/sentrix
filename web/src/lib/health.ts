import type { Host } from "../api/endpoints";

/** Notices worth showing about an agent's self-report, worst first. Empty
 * when everything is normal, so a healthy card stays quiet. */
export function healthNotes(h: Host["health"]): { text: string; bad: boolean }[] {
  const out: { text: string; bad: boolean }[] = [];
  if (h.dropped_total > 0) out.push({ text: `${h.dropped_total} batch${h.dropped_total === 1 ? "" : "es"} dropped by the agent`, bad: true });
  if (h.rejected_total > 0) out.push({ text: `${h.rejected_total} samples refused by the server (clock or validity)`, bad: true });
  if (h.buffer_depth > 1) out.push({ text: `${h.buffer_depth} batches waiting on the agent (replaying)`, bad: false });
  if (Math.abs(h.clock_skew_ms) > 30_000) out.push({ text: `agent clock off by ${Math.round(h.clock_skew_ms / 1000)} s`, bad: true });
  return out;
}
