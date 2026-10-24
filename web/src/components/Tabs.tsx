export function Tabs({ tabs, value, onChange }: {
  tabs: { key: string; label: string }[];
  value: string;
  onChange: (key: string) => void;
}) {
  return (
    <nav className="tabs" role="tablist">
      {tabs.map((t) => (
        <button key={t.key} role="tab" aria-selected={t.key === value} className={t.key === value ? "active" : ""}
          onClick={() => onChange(t.key)}>
          {t.label}
        </button>
      ))}
    </nav>
  );
}
