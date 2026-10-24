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

  if (hosts.isPending) return <p className="muted">Loading hosts…</p>;
  if (hosts.isError) return <p className="error">Could not load hosts: {hosts.error.message}</p>;
  if (hosts.data.hosts.length === 0) {
    return <p className="muted">No hosts yet. Create an enrollment token and run the agent on a server.</p>;
  }
  return (
    <>
      <p className="muted">{live.connected ? "Live" : "Reconnecting…"}</p>
      <section className="grid">
        {hosts.data.hosts.map((h) => (
          <HostCard key={h.id} host={h} buffer={live.feed.buffers.get(h.id)} version={live.version} now={now} />
        ))}
      </section>
    </>
  );
}
