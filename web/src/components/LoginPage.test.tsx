import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { mockApi, renderWithClient } from "../test/harness";
import { LoginPage } from "./LoginPage";

async function signIn(email = "ann@example.com", password = "correct horse") {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Email"), email);
  await user.type(screen.getByLabelText("Password"), password);
  await user.click(screen.getByRole("button", { name: "Sign in" }));
}

describe("LoginPage", () => {
  it("posts the typed credentials", async () => {
    const calls = mockApi({ "POST /auth/login": { status: 200, body: { email: "ann@example.com", role: "admin" } } });
    renderWithClient(<LoginPage />);
    await signIn();
    expect(calls).toEqual([{ method: "POST", path: "/auth/login", body: { email: "ann@example.com", password: "correct horse" } }]);
  });

  it("does not say whether the email or the password was wrong", async () => {
    mockApi({ "POST /auth/login": { status: 401, body: { error: "unauthenticated: invalid email or password" } } });
    renderWithClient(<LoginPage />);
    await signIn();
    expect(await screen.findByRole("alert")).toHaveTextContent("Invalid email or password.");
  });

  it("explains throttling instead of calling it a wrong password", async () => {
    mockApi({ "POST /auth/login": { status: 429, body: { error: "too many failed logins" } } });
    renderWithClient(<LoginPage />);
    await signIn();
    expect(await screen.findByRole("alert")).toHaveTextContent("Too many attempts. Try again in a few minutes.");
  });

  it("disables the button while signing in", async () => {
    let answer: (r: { status: number }) => void = () => {};
    mockApi({ "POST /auth/login": () => new Promise((r) => { answer = r; }) });
    renderWithClient(<LoginPage />);
    await signIn();
    expect(screen.getByRole("button", { name: "Signing in…" })).toBeDisabled();
    answer({ status: 401 });
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeEnabled();
  });
});
