import { expect, test } from "@playwright/test";
import { signedIn } from "./session";

test("the demo's monitors are listed and come up", async ({ page }) => {
  test.setTimeout(90_000);
  await signedIn(page);
  await page.getByRole("link", { name: "Uptime" }).click();
  const storefront = page.locator("tr", { hasText: "Storefront" });
  await expect(storefront).toBeVisible();
  for (const name of ["Payments API", "Status page", "Database port"]) {
    await expect(page.locator("tr", { hasText: name })).toBeVisible();
  }
  // The first checks run at once; the list refreshes every 15 s.
  await expect(storefront.getByText("Up", { exact: true })).toBeVisible({ timeout: 60_000 });
  await expect(storefront.locator(".uptime-bars li")).toHaveCount(30);
});

test("an admin adds a monitor, sees it checked, and deletes it", async ({ page }) => {
  test.setTimeout(90_000);
  await signedIn(page);
  await page.getByRole("link", { name: "Uptime" }).click();
  await page.getByRole("button", { name: "Add monitor" }).click();
  await page.getByLabel("Name").fill("Dashboard health");
  // The demo admits loopback targets: point the monitor at this server.
  await page.getByLabel("URL").fill(new URL("/healthz", page.url()).toString());
  await page.getByRole("button", { name: "Create monitor" }).click();
  await page.getByRole("link", { name: "Dashboard health" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard health" })).toBeVisible();
  await expect(page.locator(".detail-head .badge")).toHaveText("Up", { timeout: 60_000 });
  page.once("dialog", (d) => void d.accept());
  await page.getByRole("button", { name: "Delete" }).click();
  await expect(page).toHaveURL(/#\/uptime$/);
  await expect(page.getByRole("link", { name: "Dashboard health" })).toHaveCount(0);
});
