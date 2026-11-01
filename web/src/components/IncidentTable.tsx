import type { IncidentList } from "../api/monitors";
import { classText, formatDuration } from "../lib/uptime";

const eventText = { down: "Down", up: "Recovered", cert_expiring: "Certificate expiring", cert_renewed: "Certificate renewed" } as const;

/** Incidents, and what was notified about them. */
export function IncidentTable({ data }: { data: IncidentList | undefined }) {
  if (!data) return null;
  return (
    <div className="mini-grid two">
      <section className="card">
        <h3>Incidents</h3>
        {data.incidents.length === 0 ? <p className="muted">No incidents.</p> : (
          <table className="procs">
            <thead><tr><th>Started</th><th>Duration</th><th>Cause</th><th>Failed checks</th></tr></thead>
            <tbody>
              {data.incidents.map((i) => (
                <tr key={i.id}>
                  <td className="nowrap">{new Date(i.started).toLocaleString()}</td>
                  <td>{formatDuration(i.duration_s)}{i.ended === null && <span className="badge badge-m-down">ongoing</span>}</td>
                  <td>{classText[i.error]}</td>
                  <td>{i.failures}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
      <section className="card">
        <h3>Notifications</h3>
        {data.events.length === 0 ? <p className="muted">Nothing to report yet.</p> : (
          <ul className="events">
            {data.events.map((e) => (
              <li key={e.id}>
                <strong>{eventText[e.kind]}</strong>{" "}
                <span className="muted small">{new Date(e.at).toLocaleString()}</span>{" "}
                {e.error && <span className="small">· {classText[e.error]}</span>}
                {e.down_for_s !== null && <span className="small"> · down {formatDuration(e.down_for_s)}</span>}
                {e.cert_expires_at && <span className="small"> · expires {new Date(e.cert_expires_at).toLocaleDateString()}</span>}{" "}
                <span className={`small ${e.outcome === "failed" ? "error" : "muted"}`}>
                  {!e.notified ? "(not sent: no channel or silenced)" : e.outcome === "" ? "(sending…)" : e.outcome === "failed" ? `(failed: ${e.delivery})` : "(delivered)"}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
