import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({ component: Home });

// Placeholder until the landing page (T-W1.3.1.1). Styled only with design
// tokens, so it doubles as a smoke test for the Tailwind theme.
function Home() {
	return (
		<main id="main" className="mx-auto max-w-7xl px-4 py-8">
			<div className="border-l-4 border-kenya-green pl-3">
				<h1 className="text-display text-ink">Civic Platform</h1>
				<p className="mt-2 text-body text-muted">Ward · Constituency · County · National</p>
			</div>
		</main>
	);
}
