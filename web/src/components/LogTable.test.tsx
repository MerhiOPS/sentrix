import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LogTable } from "./LogTable";

describe("LogTable", () => {
  it("says so when nothing matched", () => {
    render(<LogTable rows={[]} />);
    expect(screen.getByText("No matching lines.")).toBeInTheDocument();
  });

  it("renders a log message as text, never as markup", () => {
    const message = `<img src=x onerror="alert(1)"><script>alert(2)</script>\n  at main.go:12`;
    const { container } = render(
      <LogTable rows={[{ key: 1, ts: "2026-03-01T12:00:00Z", hostName: "web-1", level: "error", source: "app", message }]} />,
    );
    expect(container.querySelector("img, script")).toBeNull();
    expect(container.querySelector("pre")?.textContent).toBe(message);
  });
});
