import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Profile } from "@/api/api-contract";
import { parseDisplay, serializeDisplay } from "@/lib/display";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

// The account page against a mocked API client.

const api = {
	account: {
		updateProfile: vi.fn(),
		mfaEnrol: vi.fn(),
		mfaActivate: vi.fn(),
		withdrawConsent: vi.fn(),
		requestErasure: vi.fn(),
	},
	auth: { logout: vi.fn() },
};
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

const { AccountPage } = await import("@/components/account/AccountPage");

const profile = (over: Partial<Profile> = {}): Profile => ({
	publicId: "u1",
	displayName: "Wanjiku M.",
	preferredLang: "sw",
	memberSince: "2026-03-01T08:00:00Z",
	ward: { wardId: 551, name: "Kiamwangi", constituency: "Gatundu South", county: "Kiambu" },
	consent: { version: "2026-01", active: true, grantedAt: "2026-03-01T08:00:00Z" },
	mfaEnabled: false,
	...over,
});

beforeEach(() => {
	for (const fn of Object.values(api.account)) fn.mockReset();
	document.documentElement.removeAttribute("data-text");
	document.documentElement.removeAttribute("data-theme");
	// biome-ignore lint/suspicious/noDocumentCookie: reset the test cookie
	document.cookie = "civic_display=; Max-Age=0; Path=/";
});

describe("account page", () => {
	it("lays out the sections with a jump nav and no accessibility violations", async () => {
		const { container } = await renderWithProviders(<AccountPage profile={profile()} />);
		const nav = screen.getByRole("navigation", { name: "Account sections" });
		expect(
			within(nav)
				.getAllByRole("link")
				.map((l) => l.getAttribute("href")),
		).toEqual(["#profile", "#language", "#display", "#security", "#privacy"]);
		expect(screen.getByText("Kiamwangi · Gatundu South · Kiambu")).toBeInTheDocument();
		expect(screen.getByText("Member since 1 March 2026")).toBeInTheDocument();
		await expectNoA11yViolations(container);
	});

	it("saves the display name", async () => {
		const user = userEvent.setup();
		api.account.updateProfile.mockReturnValue(Effect.succeed(profile({ displayName: "Wanjiku Mwangi" })));
		await renderWithProviders(<AccountPage profile={profile()} />);
		const save = screen.getByRole("button", { name: "Save" });
		expect(save).toBeDisabled();
		const name = screen.getByRole("textbox", { name: "Display name" });
		await user.clear(name);
		await user.type(name, "  Wanjiku Mwangi ");
		await user.click(save);
		expect(api.account.updateProfile).toHaveBeenCalledWith({ payload: { displayName: "Wanjiku Mwangi" } });
		await waitFor(() => expect(name).toHaveValue("Wanjiku Mwangi"));

		await user.clear(name);
		await user.click(screen.getByRole("button", { name: "Save" }));
		expect(name).toHaveAccessibleDescription(/Enter a display name/);
	});

	it("saves the SMS language", async () => {
		const user = userEvent.setup();
		api.account.updateProfile.mockReturnValue(Effect.succeed(profile({ preferredLang: "en" })));
		await renderWithProviders(<AccountPage profile={profile()} />);
		await user.selectOptions(screen.getByRole("combobox", { name: "Language for SMS messages" }), "en");
		expect(api.account.updateProfile).toHaveBeenCalledWith({ payload: { preferredLang: "en" } });
	});

	it("applies display preferences at once and remembers them", async () => {
		const user = userEvent.setup();
		await renderWithProviders(<AccountPage profile={profile()} />);
		const large = screen.getByRole("switch", { name: "Large text" });
		expect(large).toHaveAttribute("aria-checked", "false");
		await user.click(large);
		expect(large).toHaveAttribute("aria-checked", "true");
		expect(document.documentElement).toHaveAttribute("data-text", "large");
		await user.click(screen.getByRole("radio", { name: "Dark" }));
		expect(document.documentElement).toHaveAttribute("data-theme", "dark");
		expect(document.cookie).toContain("civic_display=theme%3Adark%2Ctext");
	});

	it("sets up two-step verification", async () => {
		const user = userEvent.setup();
		api.account.mfaEnrol.mockReturnValue(
			Effect.succeed({
				secret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
				otpauthUri: "otpauth://totp/Civic?secret=JBSW",
			}),
		);
		api.account.mfaActivate.mockReturnValue(Effect.succeed({ authenticated: true, mfa: true }));
		await renderWithProviders(<AccountPage profile={profile()} />);
		await user.click(screen.getByRole("button", { name: "Set up" }));
		expect(await screen.findByText("JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP")).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "Open in authenticator app" })).toHaveAttribute(
			"href",
			"otpauth://totp/Civic?secret=JBSW",
		);

		const code = screen.getByRole("textbox", { name: "Code" });
		await user.type(code, "12a3");
		await user.click(screen.getByRole("button", { name: "Turn on" }));
		expect(code).toHaveAccessibleDescription(/Enter the 6 digits/);
		expect(api.account.mfaActivate).not.toHaveBeenCalled();

		await user.type(code, "456");
		expect(code).toHaveValue("123456");
		await user.click(screen.getByRole("button", { name: "Turn on" }));
		expect(api.account.mfaActivate).toHaveBeenCalledWith({ payload: { code: "123456" } });
		expect(await screen.findByText("On")).toBeInTheDocument();
	});

	it("withdraws consent after confirming", async () => {
		const user = userEvent.setup();
		api.account.withdrawConsent.mockReturnValue(
			Effect.succeed({ withdrawn: true, version: "2026-01", effectiveAt: "2026-09-25T10:00:00Z" }),
		);
		await renderWithProviders(<AccountPage profile={profile()} />);
		expect(screen.getByText("Given on 1 March 2026 (version 2026-01).")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Withdraw consent" }));
		const dialog = screen.getByRole("dialog", { name: "Withdraw consent?" });
		expect(api.account.withdrawConsent).not.toHaveBeenCalled();
		await user.click(within(dialog).getByRole("button", { name: "Withdraw" }));
		expect(await screen.findByText(/Withdrawn on 25 September 2026/)).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Withdraw consent" })).toBeNull();
	});

	it("requests erasure and says what's kept", async () => {
		const user = userEvent.setup();
		api.account.requestErasure.mockReturnValue(
			Effect.succeed({
				requestId: "e1",
				state: "pending",
				completionBy: "2026-10-25T10:00:00Z",
				retained: ["consents: lawful-basis evidence (DPA 2019)"],
			}),
		);
		await renderWithProviders(<AccountPage profile={profile()} />);
		await user.click(screen.getByRole("button", { name: "Delete my account" }));
		const dialog = screen.getByRole("dialog", { name: "Delete your account?" });
		await user.type(within(dialog).getByRole("textbox"), "Moving abroad");
		await user.click(within(dialog).getByRole("button", { name: "Delete my account" }));
		expect(api.account.requestErasure).toHaveBeenCalledWith({ payload: { reason: "Moving abroad" } });
		expect(
			await screen.findByText("Your account is being deleted. It will be complete by 25 October 2026."),
		).toBeInTheDocument();
		expect(screen.getByText("consents: lawful-basis evidence (DPA 2019)")).toBeInTheDocument();
	});

	it("shows an erasure already in progress instead of the button", async () => {
		await renderWithProviders(
			<AccountPage
				profile={profile({
					erasure: {
						requestId: "e1",
						state: "pending",
						requestedAt: "2026-09-20T00:00:00Z",
						completionBy: "2026-10-20T00:00:00Z",
					},
				})}
			/>,
		);
		expect(
			screen.getByText(/requested on 20 September 2026; it will be complete by 20 October 2026/),
		).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Delete my account" })).toBeNull();
	});
});

describe("display preferences cookie", () => {
	it("round-trips and ignores junk", () => {
		const prefs = { theme: "dark", largeText: true, highContrast: false, reducedMotion: true } as const;
		expect(parseDisplay(serializeDisplay(prefs))).toEqual(prefs);
		expect(parseDisplay("theme:neon,<script>,text")).toEqual({
			theme: "system",
			largeText: true,
			highContrast: false,
			reducedMotion: false,
		});
		expect(serializeDisplay(parseDisplay(undefined))).toBe("");
	});
});
