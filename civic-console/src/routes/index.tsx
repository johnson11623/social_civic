import { createFileRoute } from "@tanstack/react-router";

import type { Health } from "@/api/api-contract";
import { callApiPromise } from "@/runtimes/get-runtime";

export const Route = createFileRoute("/")({
	// T-W1.1.2.5 — the same loader runs on the server (direct handler call)
	// and in the browser (fetch to /api/health).
	loader: () => callApiPromise((api) => api.system.health()),
	component: Home,
});

// Placeholder until the landing page (T-W1.3.1.1). Styled only with design
// tokens, so it doubles as a smoke test for the Tailwind theme.
function Home() {
	return <HomeView health={Route.useLoaderData()} />;
}

export function HomeView({ health }: { health: typeof Health.Type }) {
	const up = health.backend === "ok";
	return (
		<main id="main" className="mx-auto max-w-7xl px-4 py-8">
			<div className="border-l-4 border-kenya-green pl-3">
				<h1 className="text-display text-ink">Civic Platform</h1>
				<p className="mt-2 text-body text-muted">Ward · Constituency · County · National</p>
			</div>
			<p className="mt-6 inline-flex items-center gap-2 text-small text-muted" data-testid="platform-status">
				<span
					aria-hidden="true"
					className={up ? "h-2 w-2 rounded-full bg-kenya-green" : "h-2 w-2 rounded-full bg-warning"}
				/>
				Platform API: {up ? "ok" : "unavailable"}
			</p>
		</main>
	);
}
