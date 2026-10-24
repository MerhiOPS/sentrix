import { useState } from "react";
import type { AlertRuleInput, Channel } from "../api/alerts";
import type { Host } from "../api/endpoints";
import { useAlertMutations } from "../hooks/useAlerts";
import { alertMetrics, isRate } from "../lib/ruleText";

const blank: AlertRuleInput = {
  name: "", kind: "threshold", metric: "cpu", aggregation: "avg", comparator: "gt", threshold: 90,
  duration_s: 300, hosts: [], on_no_data: "hold", cooldown_s: 900, channels: [], enabled: true,
};

/** Admin: create a rule. Every field the evaluator uses is visible here,
 * on_no_data included: no default is right everywhere. */
export function RuleForm({ hosts, channels }: { hosts: Host[]; channels: Channel[] }) {
  const { createRule } = useAlertMutations();
  const [r, setR] = useState<AlertRuleInput>(blank);
  const set = (p: Partial<AlertRuleInput>) => setR({ ...r, ...p });
  const threshold = r.kind === "threshold";
  return (
    <form className="rule-form" onSubmit={(e) => { e.preventDefault(); createRule.mutate(r, { onSuccess: () => setR(blank) }); }}>
      <input placeholder="rule name" value={r.name} onChange={(e) => set({ name: e.target.value })} />
      <select value={r.kind} onChange={(e) => set({ kind: e.target.value as AlertRuleInput["kind"] })}>
        <option value="threshold">metric threshold</option><option value="no_data">host sends no data</option>
      </select>
      {threshold && (
        <>
          {!isRate(r.metric ?? "") && (
            <select value={r.aggregation} onChange={(e) => set({ aggregation: e.target.value as AlertRuleInput["aggregation"] })}>
              <option value="avg">avg</option><option value="max">max</option><option value="min">min</option>
            </select>
          )}
          <select value={r.metric} onChange={(e) => set({ metric: e.target.value })}>
            {alertMetrics.map((m) => <option key={m.id} value={m.id}>{m.label}</option>)}
          </select>
          <select value={r.comparator} onChange={(e) => set({ comparator: e.target.value as AlertRuleInput["comparator"] })}>
            <option value="gt">&gt;</option><option value="lt">&lt;</option>
          </select>
          <input type="number" step="any" value={r.threshold} onChange={(e) => set({ threshold: Number(e.target.value) })} aria-label="threshold" />
        </>
      )}
      <label className="inline">for (minutes)
        <input type="number" min={1} max={1440} value={r.duration_s / 60} onChange={(e) => set({ duration_s: Math.round(Number(e.target.value)) * 60 })} />
      </label>
      {threshold && (
        <label className="inline">if no data
          <select value={r.on_no_data} onChange={(e) => set({ on_no_data: e.target.value as AlertRuleInput["on_no_data"] })}>
            <option value="hold">keep state</option><option value="resolve">resolve</option><option value="fire">fire</option>
          </select>
        </label>
      )}
      <label className="inline">repeat after (min)
        <input type="number" min={0} max={1440} value={(r.cooldown_s ?? 0) / 60} onChange={(e) => set({ cooldown_s: Math.round(Number(e.target.value)) * 60 })} />
      </label>
      <select multiple value={r.hosts} aria-label="hosts (none selected: all)"
        onChange={(e) => set({ hosts: Array.from(e.target.selectedOptions, (o) => o.value) })}>
        {hosts.map((h) => <option key={h.id} value={h.id}>{h.name}</option>)}
      </select>
      <select multiple value={(r.channels ?? []).map(String)} aria-label="channels"
        onChange={(e) => set({ channels: Array.from(e.target.selectedOptions, (o) => Number(o.value)) })}>
        {channels.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
      </select>
      <button type="submit" disabled={!r.name.trim()}>Add rule</button>
      {createRule.isError && <p className="error small">{createRule.error.message}</p>}
      <p className="muted small">No host selected applies the rule to every host, including ones enrolled later.</p>
    </form>
  );
}
