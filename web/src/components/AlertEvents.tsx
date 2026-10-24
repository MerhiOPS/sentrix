import type { AlertEvent } from "../api/alerts";

interface Props {
  events: AlertEvent[];
  ruleName: Map<number, string>;
  hostName: Map<string, string>;
}

/** Recent transitions with what happened to their notification. */
export function AlertEvents({ events, ruleName, hostName }: Props) {
  return (
    <section className="card">
      <h3>Recent transitions</h3>
      {events.length === 0 && <p className="muted">No transitions yet.</p>}
      {events.length > 0 && (
        <table className="logs">
          <thead><tr><th>At</th><th>Rule</th><th>Host</th><th>Change</th><th>Value</th><th>Notification</th></tr></thead>
          <tbody>
            {events.map((e) => (
              <tr key={e.id}>
                <td className="nowrap">{new Date(e.at).toLocaleString()}</td>
                <td>{ruleName.get(e.rule_id) ?? e.rule_id}</td>
                <td>{hostName.get(e.host_id) ?? e.host_id}</td>
                <td>{e.from} → <strong>{e.to === "ok" ? "resolved" : e.to}</strong></td>
                <td>{e.value === null ? "—" : e.value.toFixed(2)}</td>
                <td>{notification(e)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function notification(e: AlertEvent) {
  if (!e.notified) return <span className="muted">not sent (silenced, cooldown or no channel)</span>;
  if (e.outcome === "") return <span className="muted">pending</span>;
  if (e.outcome === "delivered") return <span className="ok">delivered</span>;
  return <span className="error" title={e.delivery}>failed: {e.delivery}</span>;
}
