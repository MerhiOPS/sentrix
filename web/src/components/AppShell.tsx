import type { Me } from "../api/endpoints";
import { useRoute } from "../hooks/useRoute";
import { useLogout } from "../hooks/useSession";
import { productName } from "../lib/brand";
import { FleetGrid } from "./FleetGrid";
import { HostDetailPage } from "./HostDetailPage";
import { LogsPage } from "./LogsPage";
import { TokenButton } from "./TokenButton";

export function AppShell({ me }: { me: Me }) {
  const logoutM = useLogout();
  const route = useRoute();
  return (
    <div className="shell">
      <header className="topbar">
        <a href="#/" className="brand"><strong>{productName}</strong></a>
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
        {route.page === "host" && <HostDetailPage hostId={route.hostId} tab={route.tab} range={route.range} />}
        {route.page === "logs" && <LogsPage hostId={route.hostId} atMs={route.atMs} />}
      </main>
    </div>
  );
}
