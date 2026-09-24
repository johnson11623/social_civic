import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";

import type { Level, Session } from "@/api/api-contract";
import { fetchFeed, type HomeData, HomeFeed } from "@/components/civic/HomeFeed";
import { Button, button } from "@/components/ui/Button";
import { LEVELS } from "@/components/ui/LevelIndicator";
import { settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import type { MessageKey } from "@/lib/i18n/messages";
import { useLogout } from "@/lib/use-logout";
import { callApiEither, callApiPromise } from "@/runtimes/get-runtime";

type HomeSearch = { level?: Level };

const isLevel = (v: unknown): v is Level =>
	typeof v === "string" && (LEVELS as readonly string[]).includes(v);

type HomeLoaderData = { session: Session; home?: HomeData };

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
		const [feed, channels] = await Promise.all([
			fetchFeed(level),
			callApiEither((api) => api.posts.channels()).then(settle),
		]);
		return { session, home: { level, feed, channels } };
	},
	component: Home,
});

function Home() {
	const data = Route.useLoaderData();
	const navigate = Route.useNavigate();
	return (
		<HomeView
			{...data}
			onLevelChange={(level) =>
				void navigate({ search: level === "ward" ? {} : { level }, replace: true, resetScroll: false })
			}
		/>
	);
}

const POINTS: MessageKey[] = ["landing.point.ward", "landing.point.elevation", "landing.point.safety"];

/** T-W1.3.1.1 — landing with Join/Log in; signed-in users see their feed (W1.4.1). */
export function HomeView({
	session,
	home,
	onLevelChange,
}: HomeLoaderData & { onLevelChange?: (level: Level) => void }) {
	return (
		<main
			id="main"
			className={
				session.authenticated ? "mx-auto max-w-2xl px-4 py-6 pb-28 md:pb-12" : "mx-auto max-w-7xl px-4 py-12"
			}
		>
			{session.authenticated && home ? <SignedIn home={home} onLevelChange={onLevelChange} /> : <Landing />}
		</main>
	);
}

function Landing() {
	const { t } = useT();
	return (
		<section className="flex max-w-2xl flex-col gap-6">
			<h1 className="border-l-4 border-kenya-green pl-3 text-display text-ink">{t("landing.title")}</h1>
			<p className="text-body text-muted">{t("landing.lead")}</p>
			<ul className="flex flex-col gap-2">
				{POINTS.map((key) => (
					<li key={key} className="flex items-center gap-3 text-body">
						<span aria-hidden="true" className="h-2 w-2 shrink-0 rounded-full bg-kenya-green" />
						{t(key)}
					</li>
				))}
			</ul>
			<div className="flex flex-wrap gap-3">
				<Link to="/join" className={button({ size: "lg" })}>
					{t("landing.join")}
				</Link>
				<Link to="/login" className={button({ variant: "secondary", size: "lg" })}>
					{t("landing.login")}
				</Link>
			</div>
		</section>
	);
}

function SignedIn({
	home,
	onLevelChange,
}: {
	home: HomeData;
	onLevelChange?: ((level: Level) => void) | undefined;
}) {
	const { t } = useT();
	const logout = useLogout();
	const [busy, setBusy] = useState(false);
	return (
		<div className="flex flex-col gap-4">
			<div className="flex flex-wrap items-center justify-between gap-2">
				<h1 className="text-h1">{t("feed.title")}</h1>
				<div className="flex gap-1">
					<Link to="/account" className={button({ variant: "ghost", size: "sm" })}>
						{t("account.link")}
					</Link>
					<Button
						variant="ghost"
						size="sm"
						disabled={busy}
						onClick={async () => {
							setBusy(true);
							await logout();
							setBusy(false);
						}}
					>
						{t("landing.logout")}
					</Button>
				</div>
			</div>
			<HomeFeed {...home} {...(onLevelChange ? { onLevelChange } : {})} />
		</div>
	);
}
