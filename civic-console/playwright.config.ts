import { defineConfig, devices } from "@playwright/test";

// T-W1.1.1.9 — end-to-end tests against the dev server.
const PORT = 3000;

export default defineConfig({
	testDir: "tests/e2e",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: process.env.CI ? "github" : "list",
	use: {
		baseURL: `http://localhost:${PORT}`,
		trace: "on-first-retry",
	},
	projects: [
		{ name: "mobile", use: { ...devices["Pixel 7"] } }, // mobile-first (xs/sm)
		{ name: "desktop", use: { ...devices["Desktop Chrome"] } },
	],
	webServer: {
		command: "pnpm dev",
		url: `http://localhost:${PORT}`,
		reuseExistingServer: !process.env.CI,
		timeout: 120_000,
	},
});
