import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { NotFound } from "@/components/layout/NotFound";
import { renderWithProviders } from "../render";

describe("NotFound", () => {
	it("is localized and links home", async () => {
		await renderWithProviders(<NotFound />, { lang: "sw", path: "/missing" });
		expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent("Ukurasa haukupatikana");
		expect(screen.getByRole("link", { name: "Rudi mwanzo" })).toHaveAttribute("href", "/");
	});
});
