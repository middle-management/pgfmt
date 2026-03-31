import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#status")).toHaveText("Ready", { timeout: 30_000 });
});

test("WASM loads and shows ready status", async ({ page }) => {
  await expect(page.locator("#status")).toHaveClass(/ready/);
  await expect(page.locator("#formatBtn")).toBeEnabled();
});

test("formats a simple SELECT", async ({ page }) => {
  await page.locator("#input").fill("select id, name from users where active = true;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("SELECT", { timeout: 15_000 });
  await expect(page.locator("#output")).toContainText("FROM");
  await expect(page.locator("#output")).toContainText("WHERE");
});

test("formats CREATE TABLE with GENERATED ALWAYS AS IDENTITY", async ({ page }) => {
  await page.locator("#input").fill(
    "create table foo (id bigint generated always as identity primary key, name text not null);"
  );
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("GENERATED ALWAYS AS IDENTITY", {
    timeout: 15_000,
  });
});

test("formats multiple statements", async ({ page }) => {
  await page.locator("#input").fill("select 1; select 2; select 3;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("SELECT\n\t1;", { timeout: 15_000 });
  await expect(page.locator("#output")).toContainText("SELECT\n\t2;");
  await expect(page.locator("#output")).toContainText("SELECT\n\t3;");
  await expect(page.locator("#formatBtn")).toBeEnabled();
});

test("load example populates input without auto-formatting", async ({ page }) => {
  await page.locator("#exampleBtn").click();
  await expect(page.locator("#input")).not.toBeEmpty();
  await expect(page.locator("#output")).toBeEmpty();
  await expect(page.locator("#formatBtn")).toBeEnabled();
});

test("clear button resets input and output", async ({ page }) => {
  await page.locator("#input").fill("select 1;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).not.toBeEmpty({ timeout: 15_000 });

  await page.locator("#clearBtn").click();
  await expect(page.locator("#input")).toBeEmpty();
  await expect(page.locator("#output")).toBeEmpty();
});

test("Ctrl+Enter triggers format", async ({ page }) => {
  await page.locator("#input").fill("select 1;");
  await page.locator("#input").press("Control+Enter");
  await expect(page.locator("#output")).toContainText("SELECT", { timeout: 15_000 });
});

test("shows error for invalid SQL", async ({ page }) => {
  await page.locator("#input").fill("NOT VALID SQL AT ALL %%%");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output .error-text")).toBeVisible({ timeout: 15_000 });
});

// The full 3-statement example previously hung in WASM. Fixed by splitting
// multi-statement input before parsing (splitStatements in format.go).
test("formats the full playground example", async ({ page }) => {
  await page.locator("#exampleBtn").click();
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("GENERATED ALWAYS AS IDENTITY", {
    timeout: 30_000,
  });
  await expect(page.locator("#output")).toContainText("monthly_revenue");
  await expect(page.locator("#formatBtn")).toBeEnabled();
});
