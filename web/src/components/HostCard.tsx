import type { Host } from "../api/endpoints";
import { formatAge, formatBytes, formatPercent } from "../lib/format";
import type { TimeBuffer } from "../lib/ringbuffer";
import { liveStatus } from "../lib/status";
import { LiveChart } from "./LiveChart";
import { StatusBadge } from "./StatusBadge";

/** One host in the fleet grid: status, latest values and a live chart. */
export function HostCard({ host, buffer, version, now }: {
  host: Host;
  buffer: TimeBuffer | undefined;
  version: number;
  now: number;
}) {
  const lastMs = Math.max(buffer?.latestMs() ?? 0, host.latest?.ts_ms ?? 0);
  const status = liveStatus(lastMs, host.interval_ms, now, host.status === "disabled");
  // Prefer the newest live point; the polled host list fills in the rest.
  const live = buffer?.latest();
  const fresh = live && live.tsMs >= (host.latest?.ts_ms ?? 0);
  const l = host.latest;
  const cpu = fresh ? live.values.cpu : l?.cpu;
  const mem = fresh ? live.values.mem : l?.mem;
  const disk = fresh ? live.values.disk : l?.disk;
  return (
    <article className="card host">
      <header>
        <h2 title={host.platform}>{host.name}</h2>
        <StatusBadge status={status} />
      </header>
      <dl className="stats">
        <div><dt>CPU</dt><dd>{formatPercent(cpu)}</dd></div>
        <div><dt>Memory</dt><dd>{formatPercent(mem)}</dd></div>
        <div><dt>Disk</dt><dd>{formatPercent(disk)}</dd></div>
        <div><dt>Used</dt><dd>{l ? formatBytes(l.mem_used) : "—"}</dd></div>
      </dl>
      <LiveChart buffer={buffer} version={version} />
      <footer>
        {host.os} · agent {host.agent_version} · {lastMs ? formatAge(now - lastMs) : "never reported"}
      </footer>
    </article>
  );
}
