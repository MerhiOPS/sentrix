import type { Me } from "../api/endpoints";
import { useLogout } from "../hooks/useSession";
import { productName } from "../lib/brand";
import { FleetGrid } from "./FleetGrid";
import { TokenButton } from "./TokenButton";

export function AppShell({ me }: { me: Me }) {
  const logoutM = useLogout();
  return (
    <div className="shell">
      <header className="topbar">
        <strong>{productName}</strong>
        <span className="muted">{me.email} · {me.role}</span>
        <button className="link" onClick={() => logoutM.mutate()}>Sign out</button>
      </header>
      <main>
        {me.role === "admin" && <TokenButton />}
        <FleetGrid />
      </main>
    </div>
  );
}
