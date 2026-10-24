export const productName = "Sentrix";

/** Title shown in the browser tab for a page. */
export function pageTitle(page: string): string {
  return page ? `${page} · ${productName}` : productName;
}
