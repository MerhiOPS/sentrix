import type { Me } from "../api/endpoints";
import { useRoute } from "../hooks/useRoute";
import { useLogout } from "../hooks/useSession";
import { productName } from "../lib/brand";
import { FleetGrid } from "./FleetGrid";
import { HostDetailPage } from "./HostDetailPage";
import { AlertsPage } from "./AlertsPage";
import { LogsPage } from "./LogsPage";
import { TokenButton } from "./TokenButton";

export function AppShell({ me }: { me: Me }) {
  const logoutM = useLogout();
  const route = useRoute();
  return (
    <div className="shell">
      <header className="topbar">
        <a href="#/" className="brand"><strong>{productName}</strong></a>
        <a href="#/" className="muted">Fleet</a>
        <a href="#/logs" className="muted">Logs</a>
        <a href="#/alerts" className="muted">Alerts</a>
        <span className="muted">{me.email} · {me.role}</span>
        <button className="link" onClick={() => logoutM.mutate()}>Sign out</button>
      </header>
      <main>
        {route.page === "fleet" && (
          <>
            {me.role === "admin" && <TokenButton />}
            <FleetGrid />
          </>
        )}
        {route.page === "host" && <HostDetailPage hostId={route.hostId} tab={route.tab} range={route.range} isAdmin={me.role === "admin"} />}
        {route.page === "alerts" && <AlertsPage isAdmin={me.role === "admin"} />}
        {route.page === "logs" && <LogsPage hostId={route.hostId} atMs={route.atMs} isAdmin={me.role === "admin"} />}
      </main>
    </div>
  );
}
