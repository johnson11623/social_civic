import { Link } from "@tanstack/react-router";

import { useT } from "@/lib/i18n/I18nProvider";

/** Router-wide 404. */
export function NotFound() {
	const { t } = useT();
	return (
		<main id="main" className="mx-auto flex max-w-2xl flex-col gap-4 px-4 py-16">
			<p className="text-micro uppercase tracking-wide text-muted">{t("notFound.eyebrow")}</p>
			<h1 className="text-h1 text-ink">{t("notFound.title")}</h1>
			<p className="text-body text-muted">{t("notFound.body")}</p>
			<Link
				to="/"
				className="inline-flex min-h-11 w-fit items-center rounded-sm bg-accent px-4 text-body font-medium text-on-accent hover:bg-accent-hover"
			>
				{t("notFound.home")}
			</Link>
		</main>
	);
}
