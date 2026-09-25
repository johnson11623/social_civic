import { expect, test } from "@playwright/test";

// T-W1.3.1.8 — registration against the running Go API (make run-api).
// Completing the SMS step needs a test SMS inbox in the platform API (the
// dev sender only logs codes), so this checks the flow up to the code screen,
// which proves the account was created and a code was requested.
test.skip(!process.env.E2E_BACKEND, "set E2E_BACKEND=1 with the Go API running (make run-api)");

const randomId = () => String(10_000_000 + Math.floor(Math.random() * 89_999_999));

test("registers a new resident up to phone verification", async ({ page }) => {
	await page.goto("/join");
	await page.getByRole("button", { name: /Continue|Endelea/ }).click();
	await expect(page.getByRole("alert")).toBeVisible(); // consent required

	await page.getByRole("checkbox").check();
	await page.getByRole("button", { name: /Continue|Endelea/ }).click();

	await page.getByLabel(/National ID number|Nambari ya kitambulisho/).fill(randomId());
	await page.getByLabel(/Mobile number|Nambari ya simu/).fill("0712 345 678");
	await page.getByRole("button", { name: /Continue|Endelea/ }).click();

	await page.getByLabel(/Search for your ward|Tafuta wadi yako/).fill("kiamwngi");
	await page.getByRole("button", { name: "Kiamwangi, Gatundu South, Kiambu" }).click();
	await page.getByRole("button", { name: /Continue|Endelea/ }).click();

	await page.getByLabel(/Display name|Jina la kuonyeshwa/).fill("E2E Resident");
	await page.getByRole("button", { name: /Create account|Fungua akaunti/ }).click();

	await expect(
		page.getByRole("heading", { name: /Enter the code we sent|Weka msimbo tuliokutumia/ }),
	).toBeVisible();
	await expect(page.getByText(/\+254 7•• ••• 678/)).toBeVisible();

	// A wrong code is refused inline and no session starts.
	await page.getByLabel(/6-digit code|Msimbo wa tarakimu 6/).fill("000000");
	await page.getByRole("button", { name: /Verify and continue|Thibitisha/ }).click();
	await expect(page.getByRole("alert")).toBeVisible();
	await expect(page).toHaveURL(/\/join$/);
});
