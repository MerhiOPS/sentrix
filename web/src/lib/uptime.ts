import type { CheckResult, DayBar, ErrorClass, Monitor, MonitorInput, MonitorStatus } from "../api/monitors";

export const monitorStatusLabel: Record<MonitorStatus, string> = {
  up: "Up", down: "Down", pending: "Pending", paused: "Paused",
};

/** What a failure class means, in words. */
export const classText: Record<ErrorClass, string> = {
  dns: "DNS lookup failed",
  connect: "Connection failed",
  timeout: "Timed out",
  tls: "TLS or certificate error",
  status: "Unexpected HTTP status",
  keyword: "Keyword check failed",
  blocked: "Address not allowed by this server",
  ping_loss: "No ping replies",
};

/**
 * Uptime as a percentage with two decimals, truncated rather than rounded:
 * 99.996 % must never read as 100 %. No data is a dash, not 100 %.
 */
export function formatUptime(ratio: number | undefined): string {
  if (ratio === undefined || !Number.isFinite(ratio)) return "—";
  if (ratio >= 1) return "100%";
  return `${(Math.floor(ratio * 10_000) / 100).toFixed(2)}%`;
}

/** A mean response time: "84 ms", "<1 ms" when measured below a millisecond. */
export function formatLatency(ms: number | null): string {
  if (ms === null) return "—";
  return ms < 1 ? "<1 ms" : `${ms} ms`;
}

/** "45s", "3m 20s", "2h 5m", "3d 4h". */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  if (s < 86_400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86_400)}d ${Math.floor((s % 86_400) / 3600)}h`;
}

export type BarLevel = "gap" | "good" | "warning" | "critical";

/** A day's level: no monitored time is a gap, never "up". */
export function barLevel(b: DayBar): BarLevel {
  const total = b.up_s + b.down_s;
  if (total === 0) return "gap";
  if (b.down_s === 0) return "good";
  return b.up_s / total >= 0.99 ? "warning" : "critical";
}

/** The tooltip and accessible text for one day. */
export function barLabel(b: DayBar): string {
  const total = b.up_s + b.down_s;
  if (total === 0) return `${b.day}: no data`;
  const parts = [`${b.day}: ${formatUptime(b.up_s / total)} up`];
  if (b.down_s > 0) parts.push(`${formatDuration(b.down_s)} down`);
  if (b.failures > 0) parts.push(`${b.failures} failed check${b.failures === 1 ? "" : "s"}`);
  return parts.join(", ");
}

/** Whole days until a certificate expires (negative once it has). */
export function certDaysLeft(expiresAt: string | null, nowMs: number): number | null {
  if (!expiresAt) return null;
  return Math.floor((Date.parse(expiresAt) - nowMs) / 86_400_000);
}

export function kindLabel(m: Pick<Monitor, "kind" | "target">): string {
  if (m.kind === "http") return m.target.startsWith("https:") ? "HTTPS" : "HTTP";
  return m.kind === "tcp" ? "TCP" : "Ping";
}

/** Latency series for the chart: failed and unknown checks are gaps. */
export function latencySeries(results: CheckResult[]): { xsMs: number[]; values: (number | null)[] } {
  return {
    xsMs: results.map((r) => Date.parse(r.at)),
    values: results.map((r) => (r.outcome === "up" && r.latency_ms !== null ? r.latency_ms : null)),
  };
}

/** The form's fields. Strings for inputs; toInput turns them into the API body. */
export interface MonitorFields {
  name: string;
  kind: Monitor["kind"];
  target: string;
  intervalS: number;
  timeoutS: number;
  retries: number;
  method: "GET" | "HEAD";
  acceptStatus: string;
  keyword: string;
  keywordMode: "" | "present" | "absent";
  keywordCase: boolean;
  certWarnDays: number;
  channels: number[];
}

export const blankFields: MonitorFields = {
  name: "", kind: "http", target: "https://", intervalS: 60, timeoutS: 10, retries: 1, method: "GET",
  acceptStatus: "200-399", keyword: "", keywordMode: "", keywordCase: false, certWarnDays: 14, channels: [],
};

export function fieldsOf(m: Monitor): MonitorFields {
  return {
    name: m.name, kind: m.kind, target: m.target, intervalS: m.interval_s, timeoutS: m.timeout_s, retries: m.confirm_retries,
    method: m.method === "HEAD" ? "HEAD" : "GET", acceptStatus: m.accept_status || "200-399", keyword: m.keyword,
    keywordMode: m.keyword_mode, keywordCase: m.keyword_case_sensitive, certWarnDays: m.cert_warn_days, channels: m.channels,
  };
}

/** The API body: http-only options are sent for http monitors only. */
export function toInput(f: MonitorFields): MonitorInput {
  const base: MonitorInput = {
    name: f.name.trim(), kind: f.kind, target: f.target.trim(), interval_s: f.intervalS, timeout_s: f.timeoutS,
    confirm_retries: f.retries, channels: f.channels,
  };
  if (f.kind !== "http") return base;
  const https = base.target.startsWith("https:");
  return {
    ...base, method: f.method, accept_status: f.acceptStatus.trim(), cert_warn_days: https ? f.certWarnDays : 0,
    keyword: f.keywordMode ? f.keyword : "", keyword_mode: f.keywordMode, keyword_case_sensitive: f.keywordCase,
  };
}

/** A placeholder per kind, so the target field says what it wants. */
export const targetHint: Record<Monitor["kind"], string> = {
  http: "https://example.com/health", tcp: "db.example.com:5432", ping: "203.0.113.10 or host.example.com",
};
