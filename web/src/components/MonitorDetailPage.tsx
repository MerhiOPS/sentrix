import { useMemo, useState } from "react";
import { useMonitor, useMonitorIncidents, useMonitorResults, useMonitors } from "../hooks/useMonitors";
import { formatAge } from "../lib/format";
import { seriesColor } from "../lib/palette";
import { certDaysLeft, classText, formatDuration, formatLatency, formatUptime, kindLabel, latencySeries } from "../lib/uptime";
import { IncidentTable } from "./IncidentTable";
import { MetricChart } from "./MetricChart";
import { MonitorActions } from "./MonitorActions";
import { MonitorForm } from "./MonitorForm";
import { MonitorStatusBadge } from "./MonitorStatusBadge";
import { UptimeBars } from "./UptimeBars";

const windows = [["24h", "24 hours"], ["7d", "7 days"], ["30d", "30 days"], ["90d", "90 days"]] as const;

/** One monitor: state, uptime by window, 90 days, response time, incidents. */
export function MonitorDetailPage({ id, isAdmin }: { id: number; isAdmin: boolean }) {
  const q = useMonitor(id);
  const results = useMonitorResults(id);
  const incidents = useMonitorIncidents(id);
  const list = useMonitors();
  const [editing, setEditing] = useState(false);
  const chart = useMemo(() => latencySeries(results.data?.results ?? []), [results.data]);
  const series = useMemo(() => [{ label: "Response time", values: chart.values, color: seriesColor(0) }], [chart]);
  if (q.isError) return <p className="error">Monitor not found.</p>;
  const m = q.data;
  if (!m) return <p className="muted">Loading…</p>;
  const now = Date.now();
  const last = results.data?.results.at(-1);
  const days = certDaysLeft(m.state.cert_expires_at, now);
  return (
    <div className="monitor-detail">
      <header className="detail-head">
        <a href="#/uptime" className="muted">← Uptime</a>
        <h1>{m.name}</h1>
        <MonitorStatusBadge status={m.state.status} />
        <span className="muted">{kindLabel(m)} · {m.target} · every {m.interval_s}s</span>
        {isAdmin && <MonitorActions m={m} onEdit={() => setEditing(true)} />}
      </header>
      {editing && <MonitorForm monitor={m} pingAvailable={list.data?.ping_available ?? true} onDone={() => setEditing(false)} />}
      <p className="muted small">
        {m.state.status === "down" && last?.error ? `${classText[last.error]} · ` : ""}
        {m.state.status === "paused" ? "Paused" : m.state.status === "pending" ? "Waiting for the first check" : `${m.state.status === "down" ? "Down" : "Up"} for ${formatDuration((now - Date.parse(m.state.since)) / 1000)}`}
        {m.state.last_check && ` · last checked ${formatAge(now - Date.parse(m.state.last_check))}`}
      </p>
      <section className="card">
        <dl className="stats uptime-tiles">
          {windows.map(([k, label]) => <div key={k}><dt>Uptime, {label}</dt><dd>{formatUptime(m.uptime[k])}</dd></div>)}
        </dl>
        <dl className="stats uptime-tiles">
          <div><dt>Response, 24 h</dt><dd>{formatLatency(m.avg_latency_ms)}</dd></div>
          <div><dt>Certificate</dt><dd className={m.state.cert_status === "expiring" ? "cert-expiring" : ""}>{days === null ? "—" : `${days} days left`}</dd></div>
          <div><dt>Last status</dt><dd>{last?.status ?? "—"}</dd></div>
          <div><dt>Re-checks</dt><dd>{m.confirm_retries}</dd></div>
        </dl>
        <h3>Last 90 days</h3>
        <UptimeBars bars={m.bars} legend />
      </section>
      <MetricChart title="Response time, last 24 hours" xsMs={chart.xsMs} series={series} unit="ms" readout />
      <p className="muted small">Gaps are failed checks, or times the server could not tell (its own network was down).</p>
      <IncidentTable data={incidents.data} />
    </div>
  );
}
