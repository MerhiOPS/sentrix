import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { mockApi, renderWithClient } from "../test/harness";
import { HostLifecycleButton } from "./HostLifecycleButton";

const id = "6f1c1d8e-0000-4000-8000-000000000001";

describe("HostLifecycleButton", () => {
  it("does nothing when the admin cancels the confirmation", async () => {
    const calls = mockApi({});
    vi.stubGlobal("confirm", () => false);
    renderWithClient(<HostLifecycleButton hostId={id} disabled={false} />);
    await userEvent.click(screen.getByRole("button", { name: "Disable host" }));
    expect(calls).toEqual([]);
  });

  it("disables after confirmation", async () => {
    const calls = mockApi({ [`POST /hosts/${id}/disable`]: { status: 204 } });
    vi.stubGlobal("confirm", () => true);
    renderWithClient(<HostLifecycleButton hostId={id} disabled={false} />);
    await userEvent.click(screen.getByRole("button", { name: "Disable host" }));
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([`POST /hosts/${id}/disable`]);
  });

  it("re-enables without asking", async () => {
    const calls = mockApi({ [`POST /hosts/${id}/enable`]: { status: 204 } });
    const confirm = vi.fn(() => false);
    vi.stubGlobal("confirm", confirm);
    renderWithClient(<HostLifecycleButton hostId={id} disabled />);
    await userEvent.click(screen.getByRole("button", { name: "Enable host" }));
    expect(confirm).not.toHaveBeenCalled();
    expect(calls).toHaveLength(1);
  });
});
