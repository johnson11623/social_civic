import { useId } from "react";

import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { translate } from "@/lib/i18n/messages";

/**
 * Advertising space. Platform rules: always labelled Sponsored in both
 * languages, targeted by area only (no profiling), never ranked with posts.
 * Until sponsor campaigns exist (EPIC 5 / W3.2) it carries a house message.
 * Fixed height, so a campaign arriving later can't shift the page. Shown in
 * the right-hand column on wide screens, and between posts below that.
 */
export function SponsoredCard({
	className,
	inFeed = false,
}: {
	className?: string | undefined;
	/** Between posts: an article (a feed contains only articles), like a promoted post. */
	inFeed?: boolean;
}) {
	const { t } = useT();
	const id = useId();
	const Tag = inFeed ? "article" : "section";
	return (
		<Tag
			aria-labelledby={id}
			data-sponsored-slot
			className={cn("flex min-h-56 flex-col gap-2 rounded-md bg-sponsored p-4", className)}
		>
			<p id={id} className="text-micro uppercase tracking-wide text-muted">
				<span lang="en">{translate("en", "sponsored.heading")}</span>
				{" · "}
				<span lang="sw">{translate("sw", "sponsored.heading")}</span>
			</p>
			<h2 className="text-h2 text-ink">{t("rail.houseTitle")}</h2>
			<p className="text-small text-ink">{t("rail.houseBody")}</p>
		</Tag>
	);
}
