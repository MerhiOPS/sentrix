import { useState } from "react";
import { useMonitors } from "../hooks/useMonitors";
import { MonitorForm } from "./MonitorForm";
import { MonitorRow } from "./MonitorRow";

/** Every uptime monitor: status, uptime, latency and the last 30 days. */
export function UptimePage({ isAdmin }: { isAdmin: boolean }) {
  const q = useMonitors();
  const [adding, setAdding] = useState(false);
  const monitors = q.data?.monitors ?? [];
  const down = monitors.filter((m) => m.state.status === "down").length;
  const now = Date.now();
  return (
    <div className="uptime-page">
      <div className="section-head">
        <h2>Uptime <span className="count">{monitors.length}</span>
          {down > 0 && <span className="badge badge-m-down">{down} down</span>}</h2>
        {isAdmin && !adding && <button onClick={() => setAdding(true)}>Add monitor</button>}
      </div>
      {adding && <MonitorForm pingAvailable={q.data?.ping_available ?? true} onDone={() => setAdding(false)} />}
      {q.isError && <p className="error">{q.error.message}</p>}
      {q.data && monitors.length === 0 && !adding && (
        <p className="muted">No monitors yet. {isAdmin ? "Add one to check a website, a port or a host from this server." : ""}</p>
      )}
      {monitors.length > 0 && (
        <table className="monitors">
          <thead>
            <tr><th>Status</th><th>Monitor</th><th>24 h</th><th>30 d</th><th>Response</th><th>Last 30 days</th><th>Cert</th><th>Checked</th></tr>
          </thead>
          <tbody>{monitors.map((m) => <MonitorRow key={m.id} m={m} now={now} />)}</tbody>
        </table>
      )}
      <p className="muted small">
        Checks run from this server. Uptime counts monitored time only: paused periods and times the server was not
        checking count as neither up nor down. A failed check is re-checked before an incident opens.
      </p>
    </div>
  );
}
