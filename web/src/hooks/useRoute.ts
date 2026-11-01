import { useEffect, useState } from "react";

export type Route =
  | { page: "fleet" }
  | { page: "host"; hostId: string; tab: string; range: string }
  | { page: "logs"; hostId: string | null; atMs: number | null }
  | { page: "alerts" }
  | { page: "uptime" }
  | { page: "monitor"; monitorId: number }
  | { page: "settings" };

/** Parses "#/hosts/<id>?tab=cpu&range=6h" and friends. Pure, so testable. */
export function parseRoute(hash: string): Route {
  const [path = "", query = ""] = hash.replace(/^#/, "").split("?");
  const q = new URLSearchParams(query);
  const parts = path.split("/").filter(Boolean);
  if (parts[0] === "hosts" && parts[1]) {
    return { page: "host", hostId: parts[1], tab: q.get("tab") ?? "cpu", range: q.get("range") ?? "1h" };
  }
  if (parts[0] === "logs") {
    const at = Number(q.get("at"));
    return { page: "logs", hostId: q.get("host"), atMs: Number.isFinite(at) && at > 0 ? at : null };
  }
  if (parts[0] === "alerts") return { page: "alerts" };
  if (parts[0] === "uptime") {
    const id = Number(parts[1]);
    return Number.isInteger(id) && id > 0 ? { page: "monitor", monitorId: id } : { page: "uptime" };
  }
  if (parts[0] === "settings") return { page: "settings" };
  return { page: "fleet" };
}

export function hostHref(hostId: string, tab = "cpu", range = "1h"): string {
  return `#/hosts/${hostId}?tab=${tab}&range=${range}`;
}

export function monitorHref(id: number): string {
  return `#/uptime/${id}`;
}

/** Where a chart click goes: the logs around that moment on that host. */
export function logsHref(hostId: string, atMs: number): string {
  return `#/logs?host=${hostId}&at=${Math.round(atMs)}`;
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(location.hash));
  useEffect(() => {
    const on = () => setRoute(parseRoute(location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return route;
}
