import { expect, test } from "@playwright/test";

test("home page renders with server-side HTML and no console errors", async ({ page }) => {
	const errors: string[] = [];
	page.on("console", (msg) => {
		if (msg.type() === "error") errors.push(msg.text());
	});
	const response = await page.goto("/");
	expect(response?.status()).toBe(200);
	await expect(page).toHaveTitle("Kiraia");
	await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
	// No horizontal scroll at any breakpoint (Design QA checklist).
	const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
	expect(overflow).toBe(false);
	expect(errors).toEqual([]);
});
