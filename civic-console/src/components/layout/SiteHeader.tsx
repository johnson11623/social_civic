import { Link } from "@tanstack/react-router";

import { LanguageToggle } from "@/components/civic/LanguageToggle";
import { useT } from "@/lib/i18n/I18nProvider";

/** Top bar on every screen: skip link (T-X.7), brand, language toggle. */
export function SiteHeader() {
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
				<LanguageToggle />
			</div>
		</header>
	);
}
