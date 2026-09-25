import { Link } from "@tanstack/react-router";

import type { Level, Session } from "@/api/api-contract";
import { type HomeData, HomeFeed } from "@/components/civic/HomeFeed";
import { WardShell } from "@/components/layout/WardShell";
import { button } from "@/components/ui/Button";
import { useT } from "@/lib/i18n/I18nProvider";
import type { MessageKey } from "@/lib/i18n/messages";

export type HomeLoaderData = { session: Session; home?: HomeData };

const POINTS: MessageKey[] = ["landing.point.ward", "landing.point.elevation", "landing.point.safety"];

/** T-W1.3.1.1 — landing with Join/Log in; signed-in users see their feed (W1.4.1). */
export function HomeView({
	session,
	home,
	urlLevel,
	onLevelChange,
}: HomeLoaderData & { urlLevel?: Level | undefined; onLevelChange?: (level: Level) => void }) {
	return (
		<main id="main" className={session.authenticated ? "pb-28 md:pb-12" : "mx-auto max-w-7xl px-4 py-12"}>
			{session.authenticated && home ? (
				<SignedIn home={home} urlLevel={urlLevel} onLevelChange={onLevelChange} />
			) : (
				<Landing />
			)}
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
	urlLevel,
	onLevelChange,
}: {
	home: HomeData;
	urlLevel?: Level | undefined;
	onLevelChange?: ((level: Level) => void) | undefined;
}) {
	const { t } = useT();
	return (
		<WardShell
			channels={home.channels}
			activeLevel={urlLevel ?? home.level}
			isModerator={home.isModerator ?? false}
		>
			<div className="mx-auto flex max-w-2xl flex-col gap-4 py-6">
				<h1 className="text-h1">{t("feed.title")}</h1>
				<HomeFeed {...home} urlLevel={urlLevel} {...(onLevelChange ? { onLevelChange } : {})} />
			</div>
		</WardShell>
	);
}
