import { useState } from "react";
import type { Monitor } from "../api/monitors";
import { useAlertConfig } from "../hooks/useAlerts";
import { useMonitorMutations } from "../hooks/useMonitors";
import { blankFields, fieldsOf, targetHint, toInput, type MonitorFields } from "../lib/uptime";

/** Admin: create a monitor, or edit one. The server validates; its reason is shown. */
export function MonitorForm({ monitor, pingAvailable, onDone }: { monitor?: Monitor; pingAvailable: boolean; onDone: () => void }) {
  const { create, update } = useMonitorMutations();
  const { channels } = useAlertConfig();
  const [f, setF] = useState<MonitorFields>(monitor ? fieldsOf(monitor) : blankFields);
  const set = (p: Partial<MonitorFields>) => setF({ ...f, ...p });
  const m = monitor ? update : create;
  const http = f.kind === "http";
  const submit = () => {
    const body = toInput(f);
    if (monitor) update.mutate({ id: monitor.id, m: body }, { onSuccess: onDone });
    else create.mutate(body, { onSuccess: onDone });
  };
  return (
    <form className="card monitor-form" onSubmit={(e) => { e.preventDefault(); submit(); }}>
      <h3>{monitor ? `Edit ${monitor.name}` : "New monitor"}</h3>
      <div className="form-grid">
        <label>Name<input value={f.name} onChange={(e) => set({ name: e.target.value })} required maxLength={100} /></label>
        <label>Type
          <select value={f.kind} onChange={(e) => {
            const kind = e.target.value as MonitorFields["kind"];
            set({ kind, target: kind === "http" ? "https://" : "" });
          }}>
            <option value="http">Website (HTTP/HTTPS)</option>
            <option value="tcp">TCP port</option>
            <option value="ping" disabled={!pingAvailable}>Ping{pingAvailable ? "" : " (unavailable on this server)"}</option>
          </select>
        </label>
        <label className="wide">{http ? "URL" : f.kind === "tcp" ? "Host and port" : "Host"}
          <input value={f.target} placeholder={targetHint[f.kind]} onChange={(e) => set({ target: e.target.value })} required />
        </label>
        <label>Check every (s)<input type="number" min={30} max={86_400} value={f.intervalS} onChange={(e) => set({ intervalS: Number(e.target.value) })} /></label>
        <label>Timeout (s)<input type="number" min={1} max={30} value={f.timeoutS} onChange={(e) => set({ timeoutS: Number(e.target.value) })} /></label>
        <label>Re-checks before down<input type="number" min={0} max={3} value={f.retries} onChange={(e) => set({ retries: Number(e.target.value) })} /></label>
        {http && (
          <>
            <label>Method
              <select value={f.method} onChange={(e) => set({ method: e.target.value as MonitorFields["method"] })}>
                <option>GET</option><option>HEAD</option>
              </select>
            </label>
            <label>Accepted status<input value={f.acceptStatus} onChange={(e) => set({ acceptStatus: e.target.value })} placeholder="200-399" /></label>
            <label>Keyword
              <select value={f.keywordMode} onChange={(e) => set({ keywordMode: e.target.value as MonitorFields["keywordMode"] })}>
                <option value="">no keyword check</option><option value="present">page must contain</option><option value="absent">page must not contain</option>
              </select>
            </label>
            {f.keywordMode && (
              <>
                <label>Text<input value={f.keyword} onChange={(e) => set({ keyword: e.target.value })} required maxLength={200} /></label>
                <label className="inline"><input type="checkbox" checked={f.keywordCase} onChange={(e) => set({ keywordCase: e.target.checked })} /> case-sensitive</label>
              </>
            )}
            {f.target.startsWith("https:") && (
              <label>Warn before certificate expiry (days)
                <input type="number" min={0} max={365} value={f.certWarnDays} onChange={(e) => set({ certWarnDays: Number(e.target.value) })} />
              </label>
            )}
          </>
        )}
        <label className="wide">Notify
          <select multiple value={f.channels.map(String)} aria-label="channels"
            onChange={(e) => set({ channels: Array.from(e.target.selectedOptions, (o) => Number(o.value)) })}>
            {(channels.data?.channels ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
          {channels.data?.channels.length === 0 && <span className="muted small">No channels yet: add Slack or a webhook on the <a href="#/alerts">Alerts</a> page.</span>}
        </label>
      </div>
      {m.isError && <p className="error small" role="alert">{m.error.message}</p>}
      <div className="form-actions">
        <button type="submit" disabled={m.isPending || !f.name.trim()}>{monitor ? "Save" : "Create monitor"}</button>
        <button type="button" className="link" onClick={onDone}>Cancel</button>
      </div>
      <p className="muted small">The first check runs as soon as the monitor is saved. Without a channel, incidents are recorded but nobody is notified.</p>
    </form>
  );
}
