import { useState } from "react";
import { useRuleMutations, useRules } from "../hooks/useLogs";

/** Admin: the tenant's own redaction rules (built-in rules are always on). */
export function RulesPanel() {
  const rules = useRules(true);
  const { create, remove } = useRuleMutations();
  const [pattern, setPattern] = useState("");
  const [action, setAction] = useState<"mask" | "drop">("mask");
  return (
    <details className="card rules">
      <summary>Redaction rules</summary>
      <p className="muted small">
        Applied at ingest to message, source and every label, after the built-in rules (tokens, passwords,
        emails, IP addresses). RE2 syntax. Redacted data is never stored.
      </p>
      <ul>
        {rules.data?.rules.map((r) => (
          <li key={r.id}><code>{r.pattern}</code> → {r.action === "drop" ? "drop line" : r.replacement || "[REDACTED]"}{" "}
            <button className="link" onClick={() => remove.mutate(r.id)}>remove</button></li>
        ))}
      </ul>
      <form onSubmit={(e) => { e.preventDefault(); create.mutate({ pattern, action }, { onSuccess: () => setPattern("") }); }}>
        <input placeholder="pattern, e.g. acct-\d{6}" value={pattern} onChange={(e) => setPattern(e.target.value)} />
        <select value={action} onChange={(e) => setAction(e.target.value as "mask" | "drop")}>
          <option value="mask">mask</option><option value="drop">drop line</option>
        </select>
        <button type="submit" disabled={!pattern}>Add rule</button>
      </form>
      {create.isError && <p className="error small">{create.error.message}</p>}
    </details>
  );
}
