import { useProcesses } from "../hooks/useHostData";
import { formatBytes } from "../lib/format";

export function ProcessTable({ hostId }: { hostId: string }) {
  const p = useProcesses(hostId);
  if (p.isPending) return <p className="muted">Loading processes…</p>;
  if (p.isError) return <p className="muted">No process snapshot yet (taken every 30 s).</p>;
  const rows = [...p.data.procs].sort((a, b) => b.cpu - a.cpu);
  return (
    <section className="card">
      <p className="muted">Snapshot at {new Date(p.data.ts).toLocaleTimeString()} · top by CPU and by memory · names only</p>
      <table className="procs">
        <thead><tr><th>Process</th><th>CPU</th><th>Memory</th></tr></thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={`${r.name}-${i}`}><td>{r.name}</td><td>{r.cpu.toFixed(1)}%</td><td>{formatBytes(r.rss)}</td></tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
