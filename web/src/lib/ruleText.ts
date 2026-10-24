import type { AlertRule } from "../api/alerts";

/** The metrics a rule may watch, with how to read their values. */
export const alertMetrics: { id: string; label: string; unit: string }[] = [
  { id: "cpu", label: "CPU", unit: "%" },
  { id: "mem", label: "Memory", unit: "%" },
  { id: "disk", label: "Disk", unit: "%" },
  { id: "load1", label: "Load 1m", unit: "" },
  { id: "load5", label: "Load 5m", unit: "" },
  { id: "load15", label: "Load 15m", unit: "" },
  { id: "net_rx_rate", label: "Network in", unit: " B/s" },
  { id: "net_tx_rate", label: "Network out", unit: " B/s" },
  { id: "disk_r_rate", label: "Disk read", unit: " B/s" },
  { id: "disk_w_rate", label: "Disk write", unit: " B/s" },
];

export function isRate(metric: string): boolean {
  return metric.endsWith("_rate");
}

/** "45m", "2h", "1h 30m": durations are whole minutes. */
export function formatMinutes(seconds: number): string {
  const m = Math.round(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return m % 60 === 0 ? `${h}h` : `${h}h ${m % 60}m`;
}

/** A rule in one sentence, as the list shows it. */
export function describeRule(r: Pick<AlertRule, "kind" | "metric" | "aggregation" | "comparator" | "threshold" | "duration_s" | "on_no_data">): string {
  const dur = formatMinutes(r.duration_s);
  if (r.kind === "no_data") return `no data for ${dur}`;
  const m = alertMetrics.find((x) => x.id === r.metric);
  const name = m?.label ?? r.metric;
  const agg = isRate(r.metric) ? "" : `${r.aggregation} `;
  const cmp = r.comparator === "lt" ? "<" : ">";
  return `${agg}${name} ${cmp} ${r.threshold}${m?.unit ?? ""} for ${dur} · no data: ${r.on_no_data}`;
}
