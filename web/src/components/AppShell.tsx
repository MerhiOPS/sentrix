import type { Me } from "../api/endpoints";
import { useRoute } from "../hooks/useRoute";
import { useLogout } from "../hooks/useSession";
import { productName } from "../lib/brand";
import { FleetGrid } from "./FleetGrid";
import { HostDetailPage } from "./HostDetailPage";
import { AlertsPage } from "./AlertsPage";
import { BrandMark } from "./BrandMark";
import { LogsPage } from "./LogsPage";
import { SettingsPage } from "./SettingsPage";
import { TokenButton } from "./TokenButton";

const NAV = [
  { page: "fleet", pages: ["fleet", "host"], href: "#/", label: "Fleet" },
  { page: "logs", pages: ["logs"], href: "#/logs", label: "Logs" },
  { page: "alerts", pages: ["alerts"], href: "#/alerts", label: "Alerts" },
  { page: "settings", pages: ["settings"], href: "#/settings", label: "Settings" },
];

export function AppShell({ me }: { me: Me }) {
  const logoutM = useLogout();
  const route = useRoute();
  return (
    <div className="shell">
      <header className="topbar">
        <a href="#/" className="brand"><BrandMark /><strong>{productName}</strong></a>
        <nav className="nav">
          {NAV.map((n) => (
            <a key={n.page} href={n.href} className={n.pages.includes(route.page) ? "active" : ""}>{n.label}</a>
          ))}
        </nav>
        <span className="who"><span className="avatar" aria-hidden>{me.email.slice(0, 1).toUpperCase()}</span>
          <span className="muted small">{me.email} · {me.role}</span></span>
        <button className="link" onClick={() => logoutM.mutate()}>Sign out</button>
      </header>
      <main key={route.page} className="page-enter">
        {route.page === "fleet" && (
          <>
            {me.role === "admin" && <TokenButton />}
            <FleetGrid />
          </>
        )}
        {route.page === "host" && <HostDetailPage hostId={route.hostId} tab={route.tab} range={route.range} isAdmin={me.role === "admin"} />}
        {route.page === "settings" && <SettingsPage />}
        {route.page === "alerts" && <AlertsPage isAdmin={me.role === "admin"} />}
        {route.page === "logs" && <LogsPage hostId={route.hostId} atMs={route.atMs} isAdmin={me.role === "admin"} />}
      </main>
    </div>
  );
}
