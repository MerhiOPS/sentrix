import { RANGES } from "../lib/timeRange";

export function RangePicker({ value, onChange }: { value: string; onChange: (key: string) => void }) {
  return (
    <div className="segmented" role="group" aria-label="Time range">
      {RANGES.map((r) => (
        <button key={r.key} className={r.key === value ? "active" : ""} onClick={() => onChange(r.key)}>
          {r.key}
        </button>
      ))}
    </div>
  );
}
