import { expect, test } from "@playwright/test";
import { demoEmail, signIn, signedIn } from "./session";

test("a wrong password is refused without saying which field was wrong", async ({ page }) => {
  await signIn(page, "not-the-password");
  await expect(page.getByRole("alert")).toHaveText("Invalid email or password.");
  await expect(page.getByRole("button", { name: "Sign out" })).toHaveCount(0);
});

test("the fleet shows every demo host reporting live data", async ({ page }) => {
  await signedIn(page);
  const cards = page.locator("article.host");
  await expect(cards).toHaveCount(4);
  await expect(page.locator("article.host").getByText("Reporting")).toHaveCount(4);
  await expect(page.getByText(`${demoEmail} · admin`)).toBeVisible();
});

test("a host page charts its metrics and switches tabs", async ({ page }) => {
  await signedIn(page);
  const first = page.locator("article.host h2 a").first();
  const name = (await first.textContent()) ?? "";
  await first.click();
  await expect(page).toHaveURL(/#\/hosts\//);
  await expect(page.getByRole("heading", { name })).toBeVisible();
  await expect(page.locator(".uplot").first()).toBeVisible();
  await page.getByRole("tab", { name: "Disk" }).click();
  await expect(page).toHaveURL(/tab=disk|\/disk/);
  await expect(page.locator(".uplot").first()).toBeVisible();
});

test("logs arrive with secrets redacted before storage", async ({ page }) => {
  await signedIn(page);
  await page.getByRole("link", { name: "Logs" }).click();
  await expect(page.locator("table.logs tbody tr").first()).toBeVisible();
  const body = page.locator("table.logs");
  await expect(body).toContainText(/\[EMAIL\]|\[REDACTED\]|\[IP\]/);
  await expect(body).not.toContainText("alice@example.com");
  await expect(body).not.toContainText("sk_demo_51HxQ2eFakeTokenValue");
});

test("an admin creates and removes an alert rule", async ({ page }) => {
  await signedIn(page);
  await page.getByRole("link", { name: "Alerts" }).click();
  const name = `e2e rule ${Date.now()}`;
  await page.getByPlaceholder("rule name").fill(name);
  await page.getByRole("button", { name: "Add rule" }).click();
  const row = page.locator(".rule-list li", { hasText: name });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "remove" }).click();
  await expect(row).toHaveCount(0);
});

test("signing out ends the session, not just the page", async ({ page }) => {
  await signedIn(page);
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
  const me = await page.request.get("/api/v1/me");
  expect(me.status()).toBe(401);
});
