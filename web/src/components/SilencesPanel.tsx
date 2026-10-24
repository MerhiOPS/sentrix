import { useState } from "react";
import type { Silence } from "../api/alerts";
import { useAlertMutations } from "../hooks/useAlerts";

interface Props {
  silences: Silence[];
  isAdmin: boolean;
  ruleName: Map<number, string>;
  hostName: Map<string, string>;
}

/** Maintenance windows: notifications are held back, transitions still recorded. */
export function SilencesPanel({ silences, isAdmin, ruleName, hostName }: Props) {
  const { createSilence, deleteSilence } = useAlertMutations();
  const [hours, setHours] = useState(2);
  const [reason, setReason] = useState("");
  const now = Date.now();
  return (
    <section className="card">
      <h3>Silences</h3>
      <ul>
        {silences.filter((s) => Date.parse(s.ends) > now).map((s) => (
          <li key={s.id}>
            {s.rule_id === null ? "all rules" : ruleName.get(s.rule_id) ?? `rule ${s.rule_id}`} on{" "}
            {s.host_id === null ? "all hosts" : hostName.get(s.host_id) ?? s.host_id}{" "}
            <span className="muted small">until {new Date(s.ends).toLocaleString()} {s.reason && `· ${s.reason}`}</span>{" "}
            {isAdmin && <button className="link" onClick={() => deleteSilence.mutate(s.id)}>end</button>}
          </li>
        ))}
      </ul>
      {isAdmin && (
        <form className="rule-form" onSubmit={(e) => {
          e.preventDefault();
          const starts = new Date();
          createSilence.mutate({ starts: starts.toISOString(), ends: new Date(starts.getTime() + hours * 3_600_000).toISOString(), reason },
            { onSuccess: () => setReason("") });
        }}>
          <label className="inline">silence everything for (h)
            <input type="number" min={1} max={720} value={hours} onChange={(e) => setHours(Number(e.target.value))} />
          </label>
          <input placeholder="reason" value={reason} onChange={(e) => setReason(e.target.value)} />
          <button type="submit">Silence</button>
          {createSilence.isError && <p className="error small">{createSilence.error.message}</p>}
        </form>
      )}
    </section>
  );
}
