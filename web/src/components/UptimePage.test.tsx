import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { slack } from "../test/fixtures";
import { mockApi, renderWithClient } from "../test/harness";
import { bars, monitor } from "../test/monitors";
import { UptimePage } from "./UptimePage";

function serve(ping = true) {
  return mockApi({
    "GET /monitors": {
      status: 200, body: {
        ping_available: ping, monitors: [
          monitor(),
          monitor({ id: 2, name: "api", state: { status: "down", since: "2026-11-01T11:50:00Z", last_check: "2026-11-01T11:59:50Z",
            failures: 4, cert_status: "expiring", cert_expires_at: "2026-11-05T00:00:00Z" }, bars: bars(30, (i) => (i === 29 ? { down_s: 600, failures: 4 } : {})) }),
          monitor({ id: 3, name: "new", kind: "tcp", target: "db.example.com:5432", uptime: {}, avg_latency_ms: null, bars: bars(30, () => ({ up_s: 0 })),
            state: { status: "pending", since: "2026-11-01T12:00:00Z", last_check: null, failures: 0, cert_status: null, cert_expires_at: null } }),
        ],
      },
    },
    "GET /channels": { status: 200, body: { channels: [slack] } },
    "POST /monitors": (b) => ({ status: 201, body: monitor({ ...(b as object), id: 9 }) }),
  });
}

describe("UptimePage", () => {
  it("shows each monitor's state, uptime and the down count", async () => {
    serve();
    renderWithClient(<UptimePage isAdmin={false} />);
    const api = (await screen.findByText("api")).closest("tr")!;
    expect(within(api).getByText("Down")).toBeInTheDocument();
    expect(screen.getByText("1 down")).toBeInTheDocument();
    const shop = screen.getByText("shop").closest("tr")!;
    expect(within(shop).getByText("100%")).toBeInTheDocument();
    expect(within(shop).getByText("99.98%")).toBeInTheDocument(); // truncated, never rounded up
    const fresh = screen.getByText("new").closest("tr")!;
    expect(within(fresh).getAllByText("—").length).toBeGreaterThanOrEqual(2); // no data is not 100 %
    expect(within(fresh).getByText("not yet")).toBeInTheDocument();
    expect(within(fresh).getByText(/TCP · db.example.com:5432/)).toBeInTheDocument();
  });

  it("gives a viewer no way to add monitors", async () => {
    serve();
    renderWithClient(<UptimePage isAdmin={false} />);
    await screen.findByText("shop");
    expect(screen.queryByRole("button", { name: "Add monitor" })).toBeNull();
  });

  it("creates an http monitor with a keyword check", async () => {
    const calls = serve();
    const user = userEvent.setup();
    renderWithClient(<UptimePage isAdmin />);
    await user.click(await screen.findByRole("button", { name: "Add monitor" }));
    await user.type(screen.getByLabelText("Name"), "Shop");
    await user.clear(screen.getByLabelText("URL"));
    await user.type(screen.getByLabelText("URL"), "https://shop.example.com/");
    await user.selectOptions(screen.getByLabelText("Keyword"), "present");
    await user.type(screen.getByLabelText("Text"), "Add to cart");
    await user.selectOptions(screen.getByLabelText("channels"), "ops");
    await user.click(screen.getByRole("button", { name: "Create monitor" }));
    const post = calls.find((c) => c.method === "POST");
    expect(post?.body).toEqual({ name: "Shop", kind: "http", target: "https://shop.example.com/", interval_s: 60, timeout_s: 10,
      confirm_retries: 1, channels: [3], method: "GET", accept_status: "200-399", cert_warn_days: 14, keyword: "Add to cart",
      keyword_mode: "present", keyword_case_sensitive: false });
  });

  it("sends a tcp monitor without http options, and offers ping only where it can run", async () => {
    const calls = serve(false);
    const user = userEvent.setup();
    renderWithClient(<UptimePage isAdmin />);
    await user.click(await screen.findByRole("button", { name: "Add monitor" }));
    expect(screen.getByRole("option", { name: /Ping \(unavailable/ })).toBeDisabled();
    await user.selectOptions(screen.getByLabelText("Type"), "tcp");
    expect(screen.queryByLabelText("Method")).toBeNull();
    await user.type(screen.getByLabelText("Name"), "db");
    await user.type(screen.getByLabelText("Host and port"), "db.example.com:5432");
    await user.click(screen.getByRole("button", { name: "Create monitor" }));
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "db", kind: "tcp", target: "db.example.com:5432",
      interval_s: 60, timeout_s: 10, confirm_retries: 1, channels: [] });
  });

  it("shows why the server refused a monitor", async () => {
    mockApi({
      "GET /monitors": { status: 200, body: { monitors: [], ping_available: true } },
      "GET /channels": { status: 200, body: { channels: [] } },
      "POST /monitors": { status: 400, body: { error: "invalid: interval must be whole seconds between 1m0s and 24h" } },
    });
    const user = userEvent.setup();
    renderWithClient(<UptimePage isAdmin />);
    await user.click(await screen.findByRole("button", { name: "Add monitor" }));
    await user.type(screen.getByLabelText("Name"), "x");
    await user.click(screen.getByRole("button", { name: "Create monitor" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("between 1m0s and 24h");
  });
});
