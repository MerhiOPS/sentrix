import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

/** A recorded API call. */
export interface Call {
  method: string;
  path: string;
  body: unknown;
}

type Reply = { status: number; body?: unknown };
type Route = Reply | ((body: unknown) => Reply | Promise<Reply>);

/**
 * Replaces fetch with a fixed set of API routes, keyed "METHOD /path" (the
 * part after /api/v1; a trailing * matches any rest, for query strings
 * carrying times). A request to any other route fails the test: a
 * component calling an endpoint nobody expected is a bug, not a no-op.
 */
export function mockApi(routes: Record<string, Route>): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal("fetch", async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? "GET";
    const path = url.replace(/^\/api\/v1/, "");
    const body = typeof init.body === "string" ? JSON.parse(init.body) : undefined;
    calls.push({ method, path, body });
    const key = `${method} ${path}`;
    const route = routes[key] ?? Object.entries(routes).find(([k]) => k.endsWith("*") && key.startsWith(k.slice(0, -1)))?.[1];
    if (!route) throw new Error(`unexpected request: ${method} ${path}`);
    const reply = await (typeof route === "function" ? route(body) : route);
    return new Response(reply.status === 204 ? null : JSON.stringify(reply.body ?? {}), {
      status: reply.status,
      headers: { "Content-Type": "application/json" },
    });
  });
  return calls;
}

/** Renders ui with a fresh query client that never retries. */
export function renderWithClient(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}
