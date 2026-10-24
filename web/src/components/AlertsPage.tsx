import { useAlertConfig, useAlerts, useAlertMutations } from "../hooks/useAlerts";
import { useHosts } from "../hooks/useHosts";
import { hostHref } from "../hooks/useRoute";
import { formatAge } from "../lib/format";
import { describeRule } from "../lib/ruleText";
import { AlertEvents } from "./AlertEvents";
import { ChannelsPanel } from "./ChannelsPanel";
import { RuleForm } from "./RuleForm";
import { SilencesPanel } from "./SilencesPanel";

/** Current alerts, recent transitions, and (admin) rule management. */
export function AlertsPage({ isAdmin }: { isAdmin: boolean }) {
  const alerts = useAlerts();
  const { rules, channels, silences } = useAlertConfig();
  const { deleteRule } = useAlertMutations();
  const hosts = useHosts();
  const hostName = new Map(hosts.data?.hosts.map((h) => [h.id, h.name]));
  const ruleName = new Map(rules.data?.rules.map((r) => [r.id, r.name]));
  const now = Date.now();
  return (
    <div className="alerts-page">
      <p className="muted small">
        Rules are evaluated once a minute against the last complete 1-minute rollup, so an alert fires at the
        earliest about 1.5 minutes after its condition starts to hold for the rule's duration.
      </p>
      <section className="card">
        <h3>Active</h3>
        {alerts.data && alerts.data.states.length === 0 && <p className="muted">Nothing pending or firing.</p>}
        <ul className="active-alerts">
          {alerts.data?.states.map((s) => (
            <li key={`${s.rule_id}-${s.host_id}`}>
              <span className={`badge badge-${s.state}`}>{s.state}</span>{" "}
              <strong>{ruleName.get(s.rule_id) ?? `rule ${s.rule_id}`}</strong> on{" "}
              <a href={hostHref(s.host_id)}>{hostName.get(s.host_id) ?? s.host_id}</a>{" "}
              <span className="muted small">since {formatAge(now - Date.parse(s.since))}</span>
            </li>
          ))}
        </ul>
      </section>
      <section className="card">
        <h3>Rules</h3>
        <ul className="rule-list">
          {rules.data?.rules.map((r) => (
            <li key={r.id}>
              <strong>{r.name}</strong> <span className="muted">{describeRule(r)}</span>{" "}
              {!r.enabled && <span className="badge badge-disabled">disabled</span>}{" "}
              {r.enabled && r.inert && <span className="badge badge-stale" title="Matches no enabled host">inert</span>}{" "}
              {r.channels.length === 0 && <span className="muted small">(no channel: records only)</span>}{" "}
              {isAdmin && <button className="link" onClick={() => deleteRule.mutate(r.id)}>remove</button>}
            </li>
          ))}
        </ul>
        {isAdmin && <RuleForm hosts={hosts.data?.hosts ?? []} channels={channels.data?.channels ?? []} />}
      </section>
      <AlertEvents events={alerts.data?.events ?? []} ruleName={ruleName} hostName={hostName} />
      <div className="mini-grid">
        <ChannelsPanel channels={channels.data?.channels ?? []} isAdmin={isAdmin} />
        <SilencesPanel silences={silences.data?.silences ?? []} isAdmin={isAdmin} ruleName={ruleName} hostName={hostName} />
      </div>
    </div>
  );
}
