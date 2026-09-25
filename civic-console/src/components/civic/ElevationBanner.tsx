import type { Level } from "@/api/api-contract";
import { TrendingUpIcon } from "@/components/ui/icons";
import { FEATURES } from "@/lib/features";
import { useT } from "@/lib/i18n/I18nProvider";

/**
 * T-W1.4.1.5 — shown on posts that rose above their ward: where the post
 * started, where it is now, and the way into "Why am I seeing this?".
 */
export function ElevationBanner({ level, onWhy }: { level: Level; onWhy: () => void }) {
	const { t } = useT();
	return (
		<div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-elevated px-3 py-2 text-small text-ink">
			<TrendingUpIcon className="h-4 w-4 shrink-0 text-kenya-green" />
			<p className="flex-1">{t("post.elevated", { from: t("level.ward"), to: t(`level.${level}`) })}</p>
			{FEATURES.whyAmISeeing && (
				<button
					type="button"
					onClick={onWhy}
					className="min-h-11 rounded-sm text-accent underline underline-offset-2 hover:text-accent-hover focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent md:min-h-0"
				>
					{t("post.why")}
				</button>
			)}
		</div>
	);
}
