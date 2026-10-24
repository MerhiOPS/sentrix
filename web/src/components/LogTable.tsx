import { LevelBadge } from "./LevelBadge";

export interface Row {
  key: string | number;
  ts: string;
  hostName: string;
  level: string;
  source: string;
  message: string;
}

/** Log rows, newest first. Messages keep their line breaks (stack traces). */
export function LogTable({ rows }: { rows: Row[] }) {
  if (rows.length === 0) return <p className="muted">No matching lines.</p>;
  return (
    <table className="logs">
      <thead><tr><th>Time</th><th>Host</th><th>Level</th><th>Source</th><th>Message</th></tr></thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.key}>
            <td className="nowrap">{new Date(r.ts).toLocaleTimeString()}</td>
            <td className="nowrap">{r.hostName}</td>
            <td><LevelBadge level={r.level} /></td>
            <td className="muted nowrap" title={r.source}>{r.source}</td>
            <td><pre>{r.message}</pre></td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
