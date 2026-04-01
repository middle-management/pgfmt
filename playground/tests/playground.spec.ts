import { test, expect, type Page } from "@playwright/test";

// The first format call triggers wazero to compile the inner libpg_query
// WASM module, which can take 2+ minutes on CI runners. We use a shared
// page across all tests so the compilation only happens once.
const FIRST_FORMAT_TIMEOUT = 180_000;
const FORMAT_TIMEOUT = 15_000;

let page: Page;

test.describe.configure({ mode: "serial" });

test.beforeAll(async ({ browser }, testInfo) => {
  testInfo.setTimeout(FIRST_FORMAT_TIMEOUT + 30_000);
  page = await browser.newPage();
  await page.goto("/");
  // The pre-warm (wazero compiling libpg_query.wasm) can take 2+ minutes on CI.
  await expect(page.locator("#status")).toHaveText("Ready", {
    timeout: FIRST_FORMAT_TIMEOUT,
  });
});

test.afterAll(async () => {
  await page.close();
});

async function resetPage() {
  await page.locator("#clearBtn").click();
  await expect(page.locator("#input")).toBeEmpty();
}

test("WASM loads and shows ready status", async () => {
  await expect(page.locator("#status")).toHaveClass(/ready/);
  await expect(page.locator("#formatBtn")).toBeEnabled();
});

test("formats a simple SELECT", async () => {
  await page.locator("#input").fill(
    "select id, name from users where active = true;"
  );
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("SELECT", {
    timeout: FORMAT_TIMEOUT,
  });
  await expect(page.locator("#output")).toContainText("FROM");
  await expect(page.locator("#output")).toContainText("WHERE");
  await resetPage();
});

test("formats CREATE TABLE with GENERATED ALWAYS AS IDENTITY", async () => {
  await page.locator("#input").fill(
    "create table foo (id bigint generated always as identity primary key, name text not null);"
  );
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText(
    "GENERATED ALWAYS AS IDENTITY",
    { timeout: FORMAT_TIMEOUT }
  );
  await resetPage();
});

test("formats multiple statements", async () => {
  await page.locator("#input").fill("select 1; select 2; select 3;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText("SELECT\n\t1;", {
    timeout: FORMAT_TIMEOUT,
  });
  await expect(page.locator("#output")).toContainText("SELECT\n\t2;");
  await expect(page.locator("#output")).toContainText("SELECT\n\t3;");
  await expect(page.locator("#formatBtn")).toBeEnabled();
  await resetPage();
});

test("load example populates input without auto-formatting", async () => {
  await page.locator("#exampleBtn").click();
  await expect(page.locator("#input")).not.toBeEmpty();
  await expect(page.locator("#output")).toBeEmpty();
  await expect(page.locator("#formatBtn")).toBeEnabled();
  await resetPage();
});

test("clear button resets input and output", async () => {
  await page.locator("#input").fill("select 1;");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).not.toBeEmpty({
    timeout: FORMAT_TIMEOUT,
  });

  await page.locator("#clearBtn").click();
  await expect(page.locator("#input")).toBeEmpty();
  await expect(page.locator("#output")).toBeEmpty();
});

test("Ctrl+Enter triggers format", async () => {
  await page.locator("#input").fill("select 1;");
  await page.locator("#input").press("Control+Enter");
  await expect(page.locator("#output")).toContainText("SELECT", {
    timeout: FORMAT_TIMEOUT,
  });
  await resetPage();
});

test("shows error for invalid SQL", async () => {
  await page.locator("#input").fill("NOT VALID SQL AT ALL %%%");
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output .error-text")).toBeVisible({
    timeout: FORMAT_TIMEOUT,
  });
  await resetPage();
});

// Requires split-statements PR to avoid OOM on the 256MB WASM memory cap.
test.fixme("formats the full playground example", async () => {
  await page.locator("#exampleBtn").click();
  await page.locator("#formatBtn").click();
  await expect(page.locator("#output")).toContainText(
    "GENERATED ALWAYS AS IDENTITY",
    { timeout: FORMAT_TIMEOUT }
  );
  await expect(page.locator("#output")).toContainText("monthly_revenue");
  await expect(page.locator("#formatBtn")).toBeEnabled();
});
