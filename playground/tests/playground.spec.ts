import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#status")).toContainText("Ready", { timeout: 30_000 });
});

test("WASM loads and shows ready status", async ({ page }) => {
  await expect(page.locator("#status")).toHaveClass(/ready/);
  await expect(page.locator("#formatBtn")).toBeEnabled();
});

test("formats a simple SELECT", async ({ page }) => {
  await page.locator("#input").fill("select id, name from users where active = true;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("SELECT");
  await expect(page.locator("#output")).toContainText("FROM");
  await expect(page.locator("#output")).toContainText("WHERE");
});

test("formats CREATE TABLE with GENERATED ALWAYS AS IDENTITY", async ({ page }) => {
  await page.locator("#input").fill(
    "create table foo (id bigint generated always as identity primary key, name text not null);"
  );
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("GENERATED ALWAYS AS IDENTITY");
});

test("formats multiple statements", async ({ page }) => {
  await page.locator("#input").fill("select 1; select 2; select 3;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("SELECT\n\t1;");
  await expect(page.locator("#output")).toContainText("SELECT\n\t2;");
  await expect(page.locator("#output")).toContainText("SELECT\n\t3;");
});

test("load example populates input without auto-formatting", async ({ page }) => {
  await page.locator("#exampleBtn").click();
  await expect(page.locator("#input")).not.toBeEmpty();
  await expect(page.locator("#output")).toBeEmpty();
});

test("clear button resets input and output", async ({ page }) => {
  await page.locator("#input").fill("select 1;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).not.toBeEmpty();
  await page.locator("#clearBtn").click();
  await expect(page.locator("#input")).toBeEmpty();
  await expect(page.locator("#output")).toBeEmpty();
});

test("Ctrl+Enter triggers format", async ({ page }) => {
  await page.locator("#input").fill("select 1;");
  await page.locator("#input").press("Control+Enter");
  await expect(page.locator("#output")).toContainText("SELECT");
});

test("shows error for invalid SQL", async ({ page }) => {
  await page.locator("#input").fill("NOT VALID SQL AT ALL %%%");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output .error-text")).toBeVisible();
});

test("formats the full playground example", async ({ page }) => {
  await page.locator("#exampleBtn").click();
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("GENERATED ALWAYS AS IDENTITY");
  await expect(page.locator("#output")).toContainText("monthly_revenue");
  // No statement may fall back to raw text (fallbacks show a banner).
  await expect(page.locator("#output .warning-banner")).not.toBeVisible();
});

test("passes psql meta-commands through (pg_dump output)", async ({ page }) => {
  await page.locator("#input").fill(
    "--\n-- PostgreSQL database dump\n--\n\n\\restrict K3y6vPqT\n\nselect id from users;\n\n\\unrestrict K3y6vPqT\n"
  );
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("\\restrict K3y6vPqT");
  await expect(page.locator("#output")).toContainText("SELECT");
  await expect(page.locator("#output")).toContainText("\\unrestrict K3y6vPqT");
  await expect(page.locator("#output .error-text")).not.toBeVisible();
});
