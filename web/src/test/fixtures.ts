import type { AlertRule, Channel } from "../api/alerts";
import type { Host } from "../api/endpoints";

export const quietHealth: Host["health"] = {
  buffer_depth: 0, dropped_total: 0, clock_skew_ms: 0, rejected_total: 0, conflict_total: 0,
};

export function host(p: Partial<Host> = {}): Host {
  return {
    id: "6f1c1d8e-0000-4000-8000-000000000001", name: "web-1", os: "linux", platform: "ubuntu 24.04",
    agent_version: "1.0.0", interval_ms: 5000, status: "ok", created_at: "2026-03-01T00:00:00Z",
    health: quietHealth, latest: null, ...p,
  };
}

export function rule(p: Partial<AlertRule> = {}): AlertRule {
  return {
    id: 1, name: "CPU high", kind: "threshold", metric: "cpu", aggregation: "avg", comparator: "gt", threshold: 90,
    duration_s: 300, hosts: [], on_no_data: "hold", cooldown_s: 900, channels: [], enabled: true, inert: false, ...p,
  };
}

export const slack: Channel = { id: 3, name: "ops", kind: "slack", url: "https://hooks.slack.com/••••" };
