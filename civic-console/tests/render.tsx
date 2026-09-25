import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";

import { I18nProvider } from "@/lib/i18n/I18nProvider";
import type { Lang } from "@/lib/i18n/lang";

/** Render `ui` inside a memory router and the i18n provider. */
export async function renderWithProviders(ui: ReactNode, { lang = "en" as Lang, path = "/" } = {}) {
	const rootRoute = createRootRoute({
		component: () => <I18nProvider initialLang={lang}>{ui}</I18nProvider>,
	});
	const router = createRouter({
		routeTree: rootRoute,
		history: createMemoryHistory({ initialEntries: [path] }),
	});
	const result = render(<RouterProvider router={router} />);
	await router.load();
	return { ...result, router };
}
