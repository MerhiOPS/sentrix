import type { Monitor } from "../api/monitors";
import { useMonitorMutations } from "../hooks/useMonitors";

/** Admin actions on one monitor. */
export function MonitorActions({ m, onEdit }: { m: Monitor; onEdit: () => void }) {
  const { pause, resume, check, remove, mute } = useMonitorMutations();
  const err = [pause, resume, check, remove, mute].find((x) => x.isError)?.error;
  return (
    <div className="monitor-actions">
      {!m.paused && <button onClick={() => check.mutate(m.id)} disabled={check.isPending}>Check now</button>}
      {m.paused
        ? <button onClick={() => resume.mutate(m.id)}>Resume</button>
        : <button onClick={() => pause.mutate(m.id)} title="Stop checking; paused time does not count against uptime">Pause</button>}
      <button onClick={() => mute.mutate({ id: m.id, hours: 1 })} title="Keep checking, hold notifications for an hour">
        {mute.isSuccess ? "Muted for 1 h" : "Mute 1 h"}
      </button>
      <button onClick={onEdit}>Edit</button>
      <button className="danger" onClick={() => {
        if (confirm(`Delete ${m.name} and its history?`)) {
          remove.mutateAsync(m.id).then(() => { location.hash = "#/uptime"; }, () => {});
        }
      }}>Delete</button>
      {err && <span className="error small" role="alert">{err.message}</span>}
    </div>
  );
}
