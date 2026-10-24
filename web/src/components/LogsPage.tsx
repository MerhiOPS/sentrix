/** The logs route exists so chart clicks have a destination; Phase 4 fills it. */
export function LogsPage({ hostId, atMs }: { hostId: string | null; atMs: number | null }) {
  return (
    <div className="card">
      <a href="#/" className="muted">← Fleet</a>
      <h1>Logs</h1>
      <p className="muted">
        Log search arrives in the next phase.
        {atMs && <> Requested: {hostId ? "this host" : "all hosts"} around {new Date(atMs).toLocaleString()}.</>}
      </p>
    </div>
  );
}
