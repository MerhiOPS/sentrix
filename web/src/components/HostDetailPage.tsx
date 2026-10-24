import { hostHref, logsHref } from "../hooks/useRoute";
import { useHost } from "../hooks/useHostData";
import { HOST_TABS } from "../lib/hostTabs";
import { statusLabel } from "../lib/status";
import { CorePanel } from "./CorePanel";
import { HostLifecycleButton } from "./HostLifecycleButton";
import { ProcessTable } from "./ProcessTable";
import { RangePicker } from "./RangePicker";
import { SeriesPanel } from "./SeriesPanel";
import { StatusBadge } from "./StatusBadge";
import { Tabs } from "./Tabs";

interface Props { hostId: string; tab: string; range: string; isAdmin: boolean }

export function HostDetailPage({ hostId, tab, range, isAdmin }: Props) {
  const host = useHost(hostId);
  const current = HOST_TABS.find((t) => t.key === tab) ?? HOST_TABS[0]!;
  const go = (t: string, r: string) => { location.hash = hostHref(hostId, t, r); };
  const pick = (ts: number) => { location.hash = logsHref(hostId, ts); };

  if (host.isError) return <p className="error">Host not found.</p>;
  return (
    <div className="host-detail">
      <header className="detail-head">
        <a href="#/" className="muted">← Fleet</a>
        <h1>{host.data?.name ?? "…"}</h1>
        {host.data && <StatusBadge status={host.data.status} />}
        {host.data && <span className="muted">{host.data.platform} · agent {host.data.agent_version} · {statusLabel[host.data.status]}</span>}
        {host.data && isAdmin && <HostLifecycleButton hostId={hostId} disabled={host.data.status === "disabled"} />}
      </header>
      <div className="toolbar">
        <Tabs tabs={HOST_TABS} value={current.key} onChange={(t) => go(t, range)} />
        {current.key !== "processes" && <RangePicker value={range} onChange={(r) => go(current.key, r)} />}
      </div>
      {current.key === "processes" && <ProcessTable hostId={hostId} />}
      {current.panels.map((p) =>
        p.kind === "core" ? (
          <CorePanel key={p.title} hostId={hostId} title={p.title} gauges={p.gauges} labels={p.labels}
            unit={p.unit} range={range} onPick={pick} />
        ) : (
          <SeriesPanel key={p.title} hostId={hostId} metric={p.metric} title={p.title} unit={p.unit} range={range} />
        ),
      )}
      <p className="muted small">Click a chart to see this host's logs at that moment.</p>
    </div>
  );
}
