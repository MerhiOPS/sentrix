import { describe, expect, it } from "vitest";
import { pageTitle } from "./brand";

describe("pageTitle", () => {
  it("prefixes the page name", () => {
    expect(pageTitle("Fleet")).toBe("Fleet · Sentrix");
  });
  it("falls back to the product name", () => {
    expect(pageTitle("")).toBe("Sentrix");
  });
});
