import { useEffect, useState } from "react";
import { useHosts } from "../hooks/useHosts";
import { useLiveStream } from "../hooks/useLiveStream";
import { HostCard } from "./HostCard";

export function FleetGrid() {
  const hosts = useHosts();
  const live = useLiveStream(true);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(t);
  }, []);

  if (hosts.isPending) {
    return (
      <section className="grid">
        {[0, 1, 2].map((i) => <div key={i} className="card skeleton" style={{ height: 260 }} />)}
      </section>
    );
  }
  if (hosts.isError) return <p className="error">Could not load hosts: {hosts.error.message}</p>;
  if (hosts.data.hosts.length === 0) {
    return <p className="muted">No hosts yet. Create an enrollment token and run the agent on a server.</p>;
  }
  return (
    <>
      <div className="section-head">
        <h2>Fleet <span className="count">{hosts.data.hosts.length}</span></h2>
        <span className={`live ${live.connected ? "on" : ""}`}>
          <i aria-hidden />{live.connected ? "Live" : "Reconnecting…"}
        </span>
      </div>
      <section className="grid">
        {hosts.data.hosts.map((h, i) => (
          <HostCard key={h.id} host={h} buffer={live.feed.buffers.get(h.id)} version={live.version} now={now} index={i} />
        ))}
      </section>
    </>
  );
}
