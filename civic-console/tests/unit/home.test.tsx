import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HomeView } from "@/routes/index";
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
		await renderWithProviders(<HomeView session={{ authenticated: true, subject: "u" }} />, { lang: "en" });
		expect(await screen.findByRole("heading", { level: 1, name: "Welcome back" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Log out" })).toBeInTheDocument();
		expect(screen.queryByRole("link", { name: "Join" })).toBeNull();
	});
});
