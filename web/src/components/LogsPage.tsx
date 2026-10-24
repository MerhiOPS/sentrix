import { useMemo, useState } from "react";
import { useHosts } from "../hooks/useHosts";
import { useLiveTail } from "../hooks/useLiveTail";
import { useLogSearch } from "../hooks/useLogs";
import { logWindow, searchProblem } from "../lib/logWindow";
import { LogFilters, type FilterState } from "./LogFilters";
import { LogTable, type Row } from "./LogTable";
import { RulesPanel } from "./RulesPanel";

export function LogsPage({ hostId, atMs, isAdmin }: { hostId: string | null; atMs: number | null; isAdmin: boolean }) {
  const [f, setF] = useState<FilterState>({ host: hostId ?? "", levels: [], q: "", range: "1h", live: false });
  const hosts = useHosts();
  const names = useMemo(() => new Map((hosts.data?.hosts ?? []).map((h) => [h.id, h.name])), [hosts.data]);
  const problem = searchProblem(f.q);
  const filter = useMemo(() => ({
    hosts: f.host ? [f.host] : [], levels: f.levels, q: f.q, ...logWindow(atMs, f.range, Date.now()),
  }), [f.host, f.levels, f.q, f.range, atMs]);
  const search = useLogSearch(filter, !f.live && problem === null);
  const tail = useLiveTail(f.live && problem === null, filter.hosts, f.levels, f.q);

  const rows: Row[] = f.live
    ? tail.lines.map((l) => ({ key: l.key, ts: l.ts, hostName: names.get(l.host_id) ?? "?", level: l.level, source: l.source, message: l.message }))
    : (search.data?.pages ?? []).flatMap((p) => p.entries).map((e) => ({
      key: e.id, ts: e.ts, hostName: names.get(e.host_id) ?? "?", level: e.level, source: e.source, message: e.message }));

  return (
    <div className="logs-page">
      <header className="detail-head"><a href="#/" className="muted">← Fleet</a><h1>Logs</h1>
        {f.live && <span className="muted">{tail.connected ? "Live" : "Reconnecting…"}</span>}</header>
      {isAdmin && <RulesPanel />}
      <LogFilters value={f} onChange={setF} hosts={hosts.data?.hosts ?? []} problem={problem} around={atMs} />
      {search.isError && !f.live && <p className="error">{search.error.message}</p>}
      <LogTable rows={rows} />
      {!f.live && search.hasNextPage && (
        <button onClick={() => search.fetchNextPage()} disabled={search.isFetchingNextPage}>Load older</button>
      )}
    </div>
  );
}
