import type { Monitor } from "../api/monitors";
import { monitorHref } from "../hooks/useRoute";
import { formatAge } from "../lib/format";
import { certDaysLeft, formatLatency, formatUptime, kindLabel } from "../lib/uptime";
import { MonitorStatusBadge } from "./MonitorStatusBadge";
import { UptimeBars } from "./UptimeBars";

/** One monitor in the uptime list. */
export function MonitorRow({ m, now }: { m: Monitor; now: number }) {
  const days = certDaysLeft(m.state.cert_expires_at, now);
  return (
    <tr className={`monitor-row status-${m.state.status}`}>
      <td><MonitorStatusBadge status={m.state.status} /></td>
      <td>
        <a href={monitorHref(m.id)} className="monitor-name">{m.name}</a>
        <div className="muted small nowrap" title={m.target}>{kindLabel(m)} · {m.target}</div>
      </td>
      <td className="num">{formatUptime(m.uptime["24h"])}</td>
      <td className="num">{formatUptime(m.uptime["30d"])}</td>
      <td className="num">{formatLatency(m.avg_latency_ms)}</td>
      <td className="bars-cell"><UptimeBars bars={m.bars} /></td>
      <td className={`num ${m.state.cert_status === "expiring" ? "cert-expiring" : ""}`}>
        {days === null ? "—" : `${days} d`}
      </td>
      <td className="muted small nowrap">{m.state.last_check ? formatAge(now - Date.parse(m.state.last_check)) : "not yet"}</td>
    </tr>
  );
}
