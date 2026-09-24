import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Route } from "@/routes/index";

// T-W1.1.1.8 — Vitest + Testing Library render a route component.
describe("home route", () => {
	it("renders the placeholder heading with token classes", () => {
		const Home = Route.options.component;
		if (!Home) throw new Error("home route has no component");
		render(<Home />);
		const heading = screen.getByRole("heading", { level: 1, name: "Civic Platform" });
		expect(heading).toHaveClass("text-display", "text-ink");
	});
});
