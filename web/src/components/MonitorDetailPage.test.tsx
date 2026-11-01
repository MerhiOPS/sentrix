import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { mockApi, renderWithClient } from "../test/harness";
import { bars, monitor } from "../test/monitors";
import { MonitorDetailPage } from "./MonitorDetailPage";

// uPlot needs a canvas; the page's own logic is what is under test.
vi.mock("./MetricChart", () => ({ MetricChart: ({ title }: { title: string }) => <figure>{title}</figure> }));

function serve(extra: Record<string, { status: number; body?: unknown }> = {}) {
  return mockApi({
    "GET /monitors/1": { status: 200, body: monitor({ bars: bars(90, (i) => (i === 10 ? { up_s: 0 } : i === 80 ? { down_s: 180, failures: 2 } : {})),
      uptime: { "24h": 1, "7d": 0.9995, "30d": 0.9999, "90d": 0.99996 } }) },
    "GET /monitors": { status: 200, body: { monitors: [], ping_available: true } },
    "GET /monitors/1/incidents?limit=50": { status: 200, body: { incidents: [
      { id: 4, started: "2026-10-20T10:00:00Z", ended: "2026-10-20T10:03:00Z", error: "timeout", failures: 3, duration_s: 180 }],
      events: [{ id: 8, kind: "up", at: "2026-10-20T10:03:00Z", error: null, down_for_s: 180, cert_expires_at: null, notified: true,
        outcome: "failed", delivery: "ops: destination answered 500" }] } },
    "GET /monitors/1/results?*": { status: 200, body: { results: [] } },
    ...extra,
  });
}

describe("MonitorDetailPage", () => {
  it("shows uptime per window, the 90 days and what happened", async () => {
    serve();
    renderWithClient(<MonitorDetailPage id={1} isAdmin={false} />);
    expect(await screen.findByRole("heading", { name: "shop" })).toBeInTheDocument();
    expect(screen.getByText("99.95%")).toBeInTheDocument();
    // 30 d at 99.99 % and 90 d at 99.996 %: truncated, both read 99.99 %, never 100 %.
    expect(screen.getAllByText("99.99%", { selector: "dd" })).toHaveLength(2);
    expect(screen.getByLabelText("2026-10-11: no data")).toBeInTheDocument();
    expect(screen.getByLabelText(/3m 0s down, 2 failed checks/)).toBeInTheDocument();
    expect(await screen.findByText("Timed out")).toBeInTheDocument();
    expect(screen.getByText(/failed: ops: destination answered 500/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
  });

  it("lets an admin pause the monitor", async () => {
    const calls = serve({ "POST /monitors/1/pause": { status: 204 } });
    renderWithClient(<MonitorDetailPage id={1} isAdmin />);
    await userEvent.click(await screen.findByRole("button", { name: "Pause" }));
    expect(calls.some((c) => c.method === "POST" && c.path === "/monitors/1/pause")).toBe(true);
  });
});

describe("deleting a monitor", () => {
  it("returns to the list instead of showing the deleted monitor as missing", async () => {
    let deleted = false;
    mockApi({
      "GET /monitors/1": () => (deleted ? { status: 404, body: { error: "not found" } } : { status: 200, body: monitor() }),
      "GET /monitors": { status: 200, body: { monitors: [], ping_available: true } },
      "GET /monitors/1/incidents?limit=50": { status: 200, body: { incidents: [], events: [] } },
      "GET /monitors/1/results?*": { status: 200, body: { results: [] } },
      "DELETE /monitors/1": () => { deleted = true; return { status: 204 }; },
    });
    vi.stubGlobal("confirm", () => true);
    location.hash = "#/uptime/1";
    renderWithClient(<MonitorDetailPage id={1} isAdmin />);
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await vi.waitFor(() => expect(location.hash).toBe("#/uptime"));
    expect(deleted).toBe(true);
    expect(screen.queryByText("Monitor not found.")).toBeNull();
  });
});
