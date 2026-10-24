import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AppShell } from "./components/AppShell";
import { LoginPage } from "./components/LoginPage";
import { useMe } from "./hooks/useSession";

const queryClient = new QueryClient();

function Root() {
  const me = useMe();
  if (me.isPending) return <p className="muted center">Loading…</p>;
  if (me.isError) return <p className="error center">Server unreachable: {me.error.message}</p>;
  return me.data ? <AppShell me={me.data} /> : <LoginPage />;
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Root />
    </QueryClientProvider>
  );
}
