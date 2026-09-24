import { createFileRoute } from "@tanstack/react-router";

import type { Health } from "@/api/api-contract";
import { LanguageToggle } from "@/components/civic/LanguageToggle";
import { useT } from "@/lib/i18n/I18nProvider";
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
	const { t } = useT();
	const up = health.backend === "ok";
	return (
		<main id="main" className="mx-auto max-w-7xl px-4 py-8">
			<div className="flex justify-end">
				<LanguageToggle />
			</div>
			<div className="mt-6 border-l-4 border-kenya-green pl-3">
				<h1 className="text-display text-ink">{t("app.name")}</h1>
				<p className="mt-2 text-body text-muted">{t("app.tagline")}</p>
			</div>
			<p className="mt-6 inline-flex items-center gap-2 text-small text-muted" data-testid="platform-status">
				<span
					aria-hidden="true"
					className={up ? "h-2 w-2 rounded-full bg-kenya-green" : "h-2 w-2 rounded-full bg-warning"}
				/>
				{t("platform.status", { status: t(up ? "platform.ok" : "platform.unavailable") })}
			</p>
		</main>
	);
}
