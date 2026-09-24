import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";

import type { Session } from "@/api/api-contract";
import { Button, button } from "@/components/ui/Button";
import { useT } from "@/lib/i18n/I18nProvider";
import type { MessageKey } from "@/lib/i18n/messages";
import { useLogout } from "@/lib/use-logout";
import { callApiPromise } from "@/runtimes/get-runtime";

export const Route = createFileRoute("/")({
	// Same loader on the server (direct handler call, reads the session
	// cookie) and in the browser (fetch to /api/auth/session).
	loader: () => callApiPromise((api) => api.auth.session()),
	component: Home,
});

function Home() {
	return <HomeView session={Route.useLoaderData()} />;
}

const POINTS: MessageKey[] = ["landing.point.ward", "landing.point.elevation", "landing.point.safety"];

/** T-W1.3.1.1 — landing with Join/Log in; signed-in users see their home. */
export function HomeView({ session }: { session: Session }) {
	return (
		<main id="main" className="mx-auto max-w-7xl px-4 py-12">
			{session.authenticated ? <SignedIn /> : <Landing />}
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

function SignedIn() {
	const { t } = useT();
	const logout = useLogout();
	const [busy, setBusy] = useState(false);
	return (
		<section className="flex max-w-2xl flex-col gap-4">
			<h1 className="text-h1">{t("landing.welcome")}</h1>
			<p className="text-body">{t("landing.signedIn")}</p>
			<p className="text-body text-muted">{t("landing.feedSoon")}</p>
			<div className="flex flex-wrap gap-3">
				<Link to="/account" className={button({ variant: "secondary" })}>
					{t("account.link")}
				</Link>
				<Button
					variant="ghost"
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
		</section>
	);
}
