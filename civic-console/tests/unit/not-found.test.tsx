import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { NotFound } from "@/components/layout/NotFound";

describe("NotFound", () => {
	it("is bilingual and links home", async () => {
		const rootRoute = createRootRoute({ component: NotFound });
		const router = createRouter({
			routeTree: rootRoute,
			history: createMemoryHistory({ initialEntries: ["/missing"] }),
		});
		render(<RouterProvider router={router} />);

		expect(await screen.findByRole("heading", { level: 1 })).toHaveTextContent(
			"Ukurasa haukupatikana · Page not found",
		);
		expect(screen.getByRole("link")).toHaveAttribute("href", "/");
	});
});
