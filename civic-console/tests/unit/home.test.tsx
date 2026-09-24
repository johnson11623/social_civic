import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HomeView } from "@/routes/index";

// T-W1.1.1.8 — Vitest + Testing Library render a route view.
describe("home route", () => {
	it("renders the placeholder heading with token classes", () => {
		render(<HomeView health={{ status: "ok", backend: "ok" }} />);
		const heading = screen.getByRole("heading", { level: 1, name: "Civic Platform" });
		expect(heading).toHaveClass("text-display", "text-ink");
		expect(screen.getByTestId("platform-status")).toHaveTextContent("Platform API: ok");
	});

	it("shows when the platform API is unavailable", () => {
		render(<HomeView health={{ status: "ok", backend: "unavailable" }} />);
		expect(screen.getByTestId("platform-status")).toHaveTextContent("Platform API: unavailable");
	});
});
