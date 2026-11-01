import { expect, type Page } from "@playwright/test";

export const demoEmail = "demo@sentrix.local";

export function demoPassword(): string {
  const pw = process.env.SENTRIX_E2E_PASSWORD;
  if (!pw) throw new Error("SENTRIX_E2E_PASSWORD unset: run through playwright.config.ts");
  return pw;
}

/** Signs in through the login form and waits for the fleet. */
export async function signIn(page: Page, password = demoPassword()) {
  await page.goto("/");
  await page.getByLabel("Email").fill(demoEmail);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

export async function signedIn(page: Page) {
  await signIn(page);
  await expect(page.getByRole("button", { name: "Sign out" })).toBeVisible();
}
