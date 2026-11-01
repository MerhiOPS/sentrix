import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { host, rule, slack } from "../test/fixtures";
import { mockApi, renderWithClient } from "../test/harness";
import { AlertsPage } from "./AlertsPage";

function serve() {
  return mockApi({
    "GET /alerts?limit=100": { status: 200, body: { states: [], events: [] } },
    "GET /alert-rules": {
      status: 200,
      body: { rules: [rule(), rule({ id: 2, name: "Silent", kind: "no_data", inert: true }), rule({ id: 3, name: "Old", enabled: false, inert: true, channels: [3] })] },
    },
    "GET /channels": { status: 200, body: { channels: [slack] } },
    "GET /silences": { status: 200, body: { silences: [] } },
    "GET /hosts": { status: 200, body: { hosts: [host()], server_time: "2026-03-01T00:00:00Z" } },
    "GET /monitors": { status: 200, body: { monitors: [], ping_available: true } },
  });
}

describe("AlertsPage", () => {
  it("gives a viewer no way to change anything", async () => {
    serve();
    renderWithClient(<AlertsPage isAdmin={false} />);
    await screen.findByText("CPU high");
    expect(screen.queryByRole("button", { name: /remove|add/i })).toBeNull();
    expect(screen.queryByPlaceholderText("rule name")).toBeNull();
  });

  it("gives an admin the rule, channel and silence controls", async () => {
    serve();
    renderWithClient(<AlertsPage isAdmin />);
    await screen.findByText("CPU high");
    expect(screen.getByRole("button", { name: "Add rule" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add channel" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "remove" }).length).toBeGreaterThanOrEqual(4);
  });

  it("marks inert, disabled and channel-less rules", async () => {
    serve();
    renderWithClient(<AlertsPage isAdmin={false} />);
    const silent = (await screen.findByText("Silent")).closest("li")!;
    expect(within(silent).getByText("inert")).toBeInTheDocument();
    expect(within(silent).getByText("(no channel: records only)")).toBeInTheDocument();
    const old = screen.getByText("Old").closest("li")!;
    expect(within(old).getByText("disabled")).toBeInTheDocument();
    expect(within(old).queryByText("inert")).toBeNull();
    expect(within(old).queryByText(/no channel/)).toBeNull();
  });

  it("shows a channel's URL only as the server masked it", async () => {
    serve();
    renderWithClient(<AlertsPage isAdmin />);
    expect(await screen.findByText(/hooks\.slack\.com\/••••/)).toBeInTheDocument();
  });
});
