import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { host, rule, slack } from "../test/fixtures";
import { mockApi, renderWithClient } from "../test/harness";
import { RuleForm } from "./RuleForm";

const hosts = [host(), host({ id: "6f1c1d8e-0000-4000-8000-000000000002", name: "db-1" })];

function serve() {
  return mockApi({
    "POST /alert-rules": (body) => ({ status: 201, body: { ...rule(), ...(body as object), id: 9 } }),
    "GET /alert-rules": { status: 200, body: { rules: [] } },
  });
}

describe("RuleForm", () => {
  it("cannot be submitted without a name", async () => {
    serve();
    renderWithClient(<RuleForm hosts={hosts} channels={[slack]} />);
    expect(screen.getByRole("button", { name: "Add rule" })).toBeDisabled();
    await userEvent.type(screen.getByPlaceholderText("rule name"), "   ");
    expect(screen.getByRole("button", { name: "Add rule" })).toBeDisabled();
  });

  it("sends minutes as seconds, with the chosen hosts and channels", async () => {
    const calls = serve();
    const user = userEvent.setup();
    renderWithClient(<RuleForm hosts={hosts} channels={[slack]} />);
    await user.type(screen.getByPlaceholderText("rule name"), "Disk full");
    await user.clear(screen.getByLabelText("for (minutes)"));
    await user.type(screen.getByLabelText("for (minutes)"), "10");
    await user.selectOptions(screen.getByLabelText("hosts (none selected: all)"), "db-1");
    await user.selectOptions(screen.getByLabelText("channels"), "ops");
    await user.click(screen.getByRole("button", { name: "Add rule" }));
    const post = calls.find((c) => c.method === "POST");
    expect(post?.body).toMatchObject({
      name: "Disk full", kind: "threshold", duration_s: 600, hosts: [hosts[1]!.id], channels: [3], on_no_data: "hold",
    });
    expect(await screen.findByPlaceholderText("rule name")).toHaveValue("");
  });

  it("hides the threshold fields for a no-data rule", async () => {
    serve();
    renderWithClient(<RuleForm hosts={hosts} channels={[]} />);
    expect(screen.getByLabelText("threshold")).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByDisplayValue("metric threshold"), "no_data");
    expect(screen.queryByLabelText("threshold")).toBeNull();
    expect(screen.queryByLabelText("if no data")).toBeNull();
  });

  it("shows the server's reason when a rule is refused", async () => {
    mockApi({ "POST /alert-rules": { status: 400, body: { error: "duration must be whole minutes between 1m and 24h" } } });
    renderWithClient(<RuleForm hosts={hosts} channels={[]} />);
    await userEvent.type(screen.getByPlaceholderText("rule name"), "x");
    await userEvent.click(screen.getByRole("button", { name: "Add rule" }));
    expect(await screen.findByText(/whole minutes/)).toBeInTheDocument();
    expect(screen.getByPlaceholderText("rule name")).toHaveValue("x");
  });
});
