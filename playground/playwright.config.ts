import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  use: {
    baseURL: "http://localhost:8787",
  },
  webServer: {
    command: "python3 -m http.server 8787",
    port: 8787,
    reuseExistingServer: !process.env.CI,
  },
  projects: [
    { name: "chromium", use: { browserName: "chromium" } },
    // WebKit uses JavaScriptCore, the engine in Safari and every iOS
    // browser — it has WASM/TextDecoder behaviors V8 does not exercise.
    { name: "webkit", use: { browserName: "webkit" } },
  ],
});
