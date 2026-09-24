import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HomeView } from "@/components/home/HomeView";
import { renderWithProviders } from "../render";

describe("landing (T-W1.3.1.1)", () => {
	it("invites visitors to join or log in, in their language", async () => {
		await renderWithProviders(<HomeView session={{ authenticated: false }} />, { lang: "sw" });
		expect(
			await screen.findByRole("heading", { level: 1, name: "Wadi yako. Sauti yako." }),
		).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "Jiunge" })).toHaveAttribute("href", "/join");
		expect(screen.getByRole("link", { name: "Ingia" })).toHaveAttribute("href", "/login");
	});

	it("shows the signed-in home with log out", async () => {
		await renderWithProviders(
			<HomeView
				session={{ authenticated: true, subject: "u" }}
				home={{
					level: "ward",
					feed: { ok: true, value: { items: [], hasMore: false } },
					channels: { ok: true, value: { wardId: 551, items: [] } },
				}}
			/>,
			{ lang: "en" },
		);
		expect(await screen.findByRole("heading", { level: 1, name: "Home" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Log out" })).toBeInTheDocument();
		expect(screen.getByRole("tab", { name: "Ward", selected: true })).toBeInTheDocument();
		expect(screen.queryByRole("link", { name: "Join" })).toBeNull();
	});
});
