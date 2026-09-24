import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HomeView } from "@/routes/index";
import { renderWithProviders } from "../render";

describe("home route", () => {
	it("renders localized heading with token classes", async () => {
		await renderWithProviders(<HomeView health={{ status: "ok", backend: "ok" }} />, { lang: "en" });
		const heading = await screen.findByRole("heading", { level: 1, name: "Civic Platform" });
		expect(heading).toHaveClass("text-display", "text-ink");
		expect(screen.getByTestId("platform-status")).toHaveTextContent("Platform API: ok");
	});

	it("renders in Kiswahili", async () => {
		await renderWithProviders(<HomeView health={{ status: "ok", backend: "unavailable" }} />, { lang: "sw" });
		expect(await screen.findByRole("heading", { level: 1, name: "Jukwaa la Kiraia" })).toBeInTheDocument();
		expect(screen.getByTestId("platform-status")).toHaveTextContent("API ya jukwaa: haipatikani");
	});
});
