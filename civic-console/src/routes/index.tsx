import { createFileRoute } from "@tanstack/react-router";

import type { Level } from "@/api/api-contract";
import { type HomeLoaderData, HomeView } from "@/components/home/HomeView";
import { isLevel } from "@/lib/levels";
import { fetchChannels, fetchFeed } from "@/lib/loaders";
import { callApiPromise } from "@/runtimes/get-runtime";

type HomeSearch = { level?: Level };

export const Route = createFileRoute("/")({
	validateSearch: (search: Record<string, unknown>): HomeSearch =>
		isLevel(search.level) && search.level !== "ward" ? { level: search.level } : {},
	// Tab changes fetch in the component and only rewrite ?level=, so the
	// loader runs on entry and on invalidation (language, session), not per tab.
	staleTime: Number.POSITIVE_INFINITY,
	// Same loader on the server (direct handler call, reads the session
	// cookie) and in the browser (fetch to /api/auth/session).
	loader: async ({ location }): Promise<HomeLoaderData> => {
		const session = await callApiPromise((api) => api.auth.session());
		if (!session.authenticated) return { session };
		const level = (location.search as HomeSearch).level ?? "ward";
		const [feed, channels] = await Promise.all([fetchFeed(level), fetchChannels()]);
		return { session, home: { level, feed, channels } };
	},
	component: Home,
});

function Home() {
	const data = Route.useLoaderData();
	const navigate = Route.useNavigate();
	const { level } = Route.useSearch();
	return (
		<HomeView
			{...data}
			urlLevel={level ?? "ward"}
			onLevelChange={(level) =>
				void navigate({ search: level === "ward" ? {} : { level }, replace: true, resetScroll: false })
			}
		/>
	);
}
