import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { components } from "../api/types.gen";
import { formatBytes } from "../lib/format";
import { usageLevel } from "../lib/usage";

type Usage = components["schemas"]["Usage"];

function Meter({ label, used, limit, fmt }: { label: string; used: number; limit: number; fmt: (n: number) => string }) {
  const level = usageLevel(used, limit);
  return (
    <div className="meter">
      <div className="meter-head"><span>{label}</span><span className={level === "ok" ? "muted" : level}>{fmt(used)} / {fmt(limit)}</span></div>
      <div className="meter-bar"><div className={`meter-fill ${level}`} style={{ width: `${Math.min(100, (used / Math.max(limit, 1)) * 100)}%` }} /></div>
    </div>
  );
}

/** Plan limits and metered usage, shown before a limit is hit. */
export function SettingsPage() {
  const q = useQuery({ queryKey: ["usage"], queryFn: () => api<Usage>("GET", "/usage"), refetchInterval: 60_000 });
  if (!q.data) return <p className="muted">{q.isError ? q.error.message : "Loading…"}</p>;
  const { plan, usage } = q.data;
  return (
    <div className="settings">
      <section className="card">
        <h3>Plan</h3>
        <Meter label="Hosts" used={usage.hosts} limit={plan.max_hosts} fmt={String} />
        <Meter label="Logs stored today" used={usage.log_bytes_today} limit={plan.log_bytes_per_day} fmt={formatBytes} />
        <p className="muted small">
          Over the host limit, new enrollments are refused and existing hosts keep reporting. Over the daily log
          budget, logs are dropped until midnight UTC and metrics keep flowing.
        </p>
        <dl className="stats">
          <div><dt>Metric history</dt><dd>{plan.metrics_retention_days} d</dd></div>
          <div><dt>Logs kept</dt><dd>{plan.logs_retention_days} d</dd></div>
        </dl>
      </section>
      <section className="card">
        <h3>This month (UTC)</h3>
        <dl className="stats">
          <div><dt>Samples</dt><dd>{usage.month_samples.toLocaleString()}</dd></div>
          <div><dt>Logs</dt><dd>{formatBytes(usage.month_log_bytes)}</dd></div>
          <div><dt>Peak hosts</dt><dd>{usage.month_peak_hosts}</dd></div>
        </dl>
      </section>
    </div>
  );
}
