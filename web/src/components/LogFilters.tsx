import type { Host } from "../api/endpoints";
import { RangePicker } from "./RangePicker";

export const LEVELS = ["fatal", "error", "warn", "info", "debug", "unknown"];

export interface FilterState {
  host: string; // "" = all
  levels: string[];
  q: string;
  range: string;
  live: boolean;
}

export function LogFilters({ value, onChange, hosts, problem, around }: {
  value: FilterState;
  onChange: (v: FilterState) => void;
  hosts: Host[];
  problem: string | null;
  around: number | null;
}) {
  const set = (p: Partial<FilterState>) => onChange({ ...value, ...p });
  const toggle = (l: string) =>
    set({ levels: value.levels.includes(l) ? value.levels.filter((x) => x !== l) : [...value.levels, l] });
  return (
    <div className="card log-filters">
      <input className="search" placeholder='Search: words (AND), OR, "exact phrase" — 3+ characters each'
        value={value.q} onChange={(e) => set({ q: e.target.value })} aria-invalid={problem !== null} />
      <select value={value.host} onChange={(e) => set({ host: e.target.value })} aria-label="Host">
        <option value="">All hosts</option>
        {hosts.map((h) => <option key={h.id} value={h.id}>{h.name}</option>)}
      </select>
      <div className="segmented" role="group" aria-label="Levels">
        {LEVELS.map((l) => (
          <button key={l} className={value.levels.includes(l) ? "active" : ""} onClick={() => toggle(l)}>{l}</button>
        ))}
      </div>
      {around ? <span className="muted">±5 min around {new Date(around).toLocaleString()}</span>
        : !value.live && <RangePicker value={value.range} onChange={(range) => set({ range })} />}
      <label className="live-toggle"><input type="checkbox" checked={value.live} onChange={(e) => set({ live: e.target.checked })} /> Live</label>
      {problem && <p className="error small">{problem}</p>}
    </div>
  );
}
