import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SiteHeader } from "@/components/layout/SiteHeader";
import { renderWithProviders } from "../render";

describe("site header", () => {
	it("shows the app name and, when signed in, the avatar linking to the account", async () => {
		await renderWithProviders(<SiteHeader user={{ displayName: "Wanjiku Mwangi" }} />, { lang: "en" });
		expect(screen.getByRole("link", { name: "Civic" })).toHaveAttribute("href", "/");
		const account = screen.getByRole("link", { name: "Your account" });
		expect(account).toHaveAttribute("href", "/account");
		expect(account).toHaveTextContent("WM");
		// The language switch is only on the account page.
		expect(screen.queryByRole("button", { name: "English" })).toBeNull();
	});

	it("offers log in when signed out", async () => {
		await renderWithProviders(<SiteHeader user={null} />, { lang: "sw" });
		expect(screen.getByRole("link", { name: "Kiraia" })).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "Ingia" })).toHaveAttribute("href", "/login");
		expect(screen.queryByRole("link", { name: "Akaunti yako" })).toBeNull();
	});
});
