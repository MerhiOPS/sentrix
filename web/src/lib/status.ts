import type { components } from "../api/types.gen";

export type HostStatus = components["schemas"]["HostStatus"];

/**
 * Human labels. With push collection the server cannot tell "agent down"
 * from "network down", so nothing here ever says "down".
 */
export const statusLabel: Record<HostStatus, string> = {
  ok: "Reporting",
  stale: "Delayed",
  no_data: "No data from host",
  disabled: "Disabled",
};

/** Status derived client-side from the newest live sample, between polls. */
export function liveStatus(lastMs: number, intervalMs: number, nowMs: number, disabled: boolean): HostStatus {
  if (disabled) return "disabled";
  if (lastMs <= 0) return "no_data";
  const iv = intervalMs > 0 ? intervalMs : 5000;
  const age = nowMs - lastMs;
  if (age > 10 * iv) return "no_data";
  if (age > 3 * iv) return "stale";
  return "ok";
}
