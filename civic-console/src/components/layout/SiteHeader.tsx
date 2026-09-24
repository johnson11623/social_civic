import { Link } from "@tanstack/react-router";

import { Avatar } from "@/components/ui/Avatar";
import { button } from "@/components/ui/Button";
import { useT } from "@/lib/i18n/I18nProvider";

/** The signed-in user, for the header; null when signed out. */
export type HeaderUser = { displayName: string } | null;

/**
 * Top bar on every screen: skip link (T-X.7), brand, and the account: the
 * user's avatar (to the account page) when signed in, "Log in" otherwise.
 */
export function SiteHeader({ user }: { user: HeaderUser }) {
	const { t } = useT();
	return (
		<header className="border-b border-border bg-paper">
			<a
				href="#main"
				className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-10 focus:rounded-sm focus:bg-accent focus:px-4 focus:py-2 focus:text-on-accent"
			>
				{t("common.skip")}
			</a>
			<div className="mx-auto flex max-w-7xl items-center justify-between gap-4 px-4 py-2">
				<Link to="/" className="flex min-h-11 items-center gap-2 text-h2 text-ink">
					<span aria-hidden="true" className="inline-flex gap-1">
						<span className="h-2 w-2 rounded-full bg-kenya-green" />
						<span className="h-2 w-2 rounded-full bg-kenya-green" />
						<span className="h-2 w-2 rounded-full bg-border" />
					</span>
					{t("app.name")}
				</Link>
				{user ? (
					<Link
						to="/account"
						aria-label={t("account.link")}
						activeProps={{ "aria-current": "page" }}
						className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
					>
						<Avatar name={user.displayName || "?"} size="sm" />
					</Link>
				) : (
					<Link to="/login" className={button({ variant: "secondary", size: "sm" })}>
						{t("landing.login")}
					</Link>
				)}
			</div>
		</header>
	);
}
