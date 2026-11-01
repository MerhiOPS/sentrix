import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { host, quietHealth } from "../test/fixtures";
import { HostCard } from "./HostCard";

// uPlot needs a canvas; the card's own logic is what is under test.
vi.mock("./LiveChart", () => ({ LiveChart: () => null }));

const now = Date.parse("2026-03-01T12:00:00Z");

describe("HostCard", () => {
  it("never claims a silent host is down", () => {
    render(<HostCard host={host({ status: "no_data" })} buffer={undefined} version={0} now={now} />);
    expect(screen.getByText("No data from host")).toBeInTheDocument();
    expect(screen.getByText(/never reported/)).toBeInTheDocument();
    expect(screen.queryByText(/down/i)).toBeNull();
  });

  it("shows a disabled host as disabled whatever its last sample", () => {
    const latest = { ts_ms: now - 1000, cpu: 12, mem: 40, disk: 50, mem_used: 1 << 30 } as NonNullable<ReturnType<typeof host>["latest"]>;
    render(<HostCard host={host({ status: "disabled", latest })} buffer={undefined} version={0} now={now} />);
    expect(screen.getByText("Disabled")).toBeInTheDocument();
  });

  it("surfaces agent problems and stays quiet when there are none", () => {
    const { rerender } = render(<HostCard host={host()} buffer={undefined} version={0} now={now} />);
    expect(screen.queryByText(/dropped|refused|replaying|clock/)).toBeNull();
    rerender(<HostCard host={host({ health: { ...quietHealth, dropped_total: 2, clock_skew_ms: 45_000 } })}
      buffer={undefined} version={0} now={now} />);
    expect(screen.getByText("2 batches dropped by the agent")).toHaveClass("error");
    expect(screen.getByText("agent clock off by 45 s")).toBeInTheDocument();
  });
});
