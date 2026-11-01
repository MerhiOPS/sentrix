import type { DayBar, Monitor } from "../api/monitors";

export function bars(n: number, f: (i: number) => Partial<DayBar> = () => ({})): DayBar[] {
  return Array.from({ length: n }, (_, i) => ({ day: `2026-10-${String(i + 1).padStart(2, "0")}`, up_s: 86_400, down_s: 0, failures: 0, ...f(i) }));
}

export function monitor(p: Partial<Monitor> = {}): Monitor {
  return {
    id: 1, name: "shop", kind: "http", target: "https://shop.example.com/health", interval_s: 60, timeout_s: 10,
    confirm_retries: 1, method: "GET", accept_status: "200-399", keyword: "", keyword_mode: "", keyword_case_sensitive: false,
    cert_warn_days: 14, channels: [], paused: false, created_at: "2026-10-01T00:00:00Z",
    state: { status: "up", since: "2026-10-31T00:00:00Z", last_check: "2026-11-01T11:59:30Z", failures: 0, cert_status: "ok",
      cert_expires_at: "2026-12-01T00:00:00Z" },
    uptime: { "24h": 1, "30d": 0.99987 }, avg_latency_ms: 84, bars: bars(30), ...p,
  };
}
