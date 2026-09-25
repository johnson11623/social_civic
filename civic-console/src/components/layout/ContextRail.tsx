import type { ChannelList } from "@/api/api-contract";
import { SponsoredCard } from "@/components/civic/SponsoredCard";
import { LEVELS, LevelIndicator } from "@/components/ui/LevelIndicator";
import { formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";

/**
 * The right-hand column on wide screens: a sponsored slot and context about
 * the reader's area. Loaded on demand (see WardShell), static, no images
 * or third-party scripts — nothing here can slow the feed.
 */
export default function ContextRail({ channels }: { channels: ChannelList | undefined }) {
	return (
		<div className="flex flex-col gap-4 py-6">
			<SponsoredCard />
			{channels?.ward && <WardGlance channels={channels} />}
			<HowPostsRise />
		</div>
	);
}

function WardGlance({ channels }: { channels: ChannelList }) {
	const { t, lang } = useT();
	const ward = channels.ward;
	if (!ward) return null;
	return (
		<section aria-labelledby="rail-ward" className="flex flex-col gap-2 rounded-md border border-border p-4">
			<h2 id="rail-ward" className="text-small font-medium text-muted">
				{t("rail.ward")}
			</h2>
			<p className="border-l-4 border-kenya-green pl-3 text-h2 text-ink">{ward.name}</p>
			<p className="text-small text-muted">
				{ward.constituency} · {ward.county}
			</p>
			<ul className="flex gap-4 text-small text-ink">
				{channels.memberCount !== undefined && (
					<li>{t("sidebar.members", { count: formatNumber(channels.memberCount, lang) })}</li>
				)}
				<li>{t("rail.channels", { count: formatNumber(channels.items.length, lang) })}</li>
			</ul>
		</section>
	);
}

function HowPostsRise() {
	const { t } = useT();
	return (
		<section
			aria-labelledby="rail-levels"
			className="flex flex-col gap-3 rounded-md border border-border p-4"
		>
			<h2 id="rail-levels" className="text-small font-medium text-muted">
				{t("rail.levelsTitle")}
			</h2>
			<ol className="flex flex-col gap-2">
				{LEVELS.map((level) => (
					<li key={level} className="flex items-center gap-3 text-small text-ink">
						<LevelIndicator current={level} />
						{t(`level.${level}`)}
					</li>
				))}
			</ol>
			<p className="text-small text-muted">{t("rail.levelsBody")}</p>
		</section>
	);
}
