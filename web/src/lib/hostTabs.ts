import type { Unit } from "./units";

/** A core chart: metrics from /hosts/{id}/metrics. */
export interface CoreChart {
  kind: "core";
  title: string;
  gauges: string[];
  labels: string[];
  unit: Unit;
}

/** Per-series small multiples of one detail metric. */
export interface SeriesCharts {
  kind: "series";
  title: string;
  metric: string;
  unit: Unit;
}

export type Panel = CoreChart | SeriesCharts;

/** What each host tab shows. Declarative, so the page has no branching. */
export const HOST_TABS: { key: string; label: string; panels: Panel[] }[] = [
  {
    key: "cpu", label: "CPU", panels: [
      { kind: "core", title: "CPU", gauges: ["cpu"], labels: ["CPU %"], unit: "percent" },
      { kind: "core", title: "Load", gauges: ["load1", "load5", "load15"], labels: ["1m", "5m", "15m"], unit: "load" },
      { kind: "series", title: "Per core", metric: "cpu_core_percent", unit: "percent" },
    ],
  },
  {
    key: "memory", label: "Memory", panels: [
      { kind: "core", title: "Memory", gauges: ["mem"], labels: ["Used %"], unit: "percent" },
      { kind: "core", title: "Memory used", gauges: ["mem_used"], labels: ["Used"], unit: "bytes" },
    ],
  },
  {
    key: "disk", label: "Disk", panels: [
      { kind: "core", title: "Disk I/O", gauges: ["disk_r_rate", "disk_w_rate"], labels: ["Read", "Write"], unit: "rate" },
      { kind: "series", title: "Usage per mount", metric: "disk_used_percent", unit: "percent" },
      { kind: "series", title: "Reads per device", metric: "disk_read_bytes", unit: "rate" },
      { kind: "series", title: "Writes per device", metric: "disk_write_bytes", unit: "rate" },
    ],
  },
  {
    key: "network", label: "Network", panels: [
      { kind: "core", title: "Network", gauges: ["net_rx_rate", "net_tx_rate"], labels: ["Received", "Sent"], unit: "rate" },
      { kind: "series", title: "Received per interface", metric: "net_rx_bytes", unit: "rate" },
      { kind: "series", title: "Sent per interface", metric: "net_tx_bytes", unit: "rate" },
    ],
  },
  { key: "processes", label: "Processes", panels: [] },
];
