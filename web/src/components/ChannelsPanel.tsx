import { useState } from "react";
import type { Channel, ChannelInput } from "../api/alerts";
import { useAlertMutations } from "../hooks/useAlerts";

/** Notification channels. URLs come back masked for every role: a Slack
 * webhook URL is a credential. */
export function ChannelsPanel({ channels, isAdmin }: { channels: Channel[]; isAdmin: boolean }) {
  const { createChannel, deleteChannel } = useAlertMutations();
  const [c, setC] = useState<ChannelInput>({ name: "", kind: "slack", url: "" });
  return (
    <section className="card">
      <h3>Channels</h3>
      <ul>
        {channels.map((ch) => (
          <li key={ch.id}>
            <strong>{ch.name}</strong> <span className="muted">{ch.kind} · {ch.url}</span>{" "}
            {isAdmin && <button className="link" onClick={() => deleteChannel.mutate(ch.id)}>remove</button>}
          </li>
        ))}
      </ul>
      {isAdmin && (
        <form className="rule-form" onSubmit={(e) => { e.preventDefault(); createChannel.mutate(c, { onSuccess: () => setC({ ...c, name: "", url: "" }) }); }}>
          <input placeholder="name" value={c.name} onChange={(e) => setC({ ...c, name: e.target.value })} />
          <select value={c.kind} onChange={(e) => setC({ ...c, kind: e.target.value as ChannelInput["kind"] })}>
            <option value="slack">Slack</option><option value="webhook">webhook</option>
          </select>
          <input placeholder="https://…" value={c.url} onChange={(e) => setC({ ...c, url: e.target.value })} />
          <button type="submit" disabled={!c.name || !c.url}>Add channel</button>
          {createChannel.isError && <p className="error small">{createChannel.error.message}</p>}
        </form>
      )}
    </section>
  );
}
