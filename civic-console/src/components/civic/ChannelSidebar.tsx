import { Link } from "@tanstack/react-router";

import type { ChannelList, Level } from "@/api/api-contract";
import { PlusIcon } from "@/components/ui/icons";
import { LEVELS, LevelIndicator } from "@/components/ui/LevelIndicator";
import { cn } from "@/lib/cn";
import { formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { ChannelItem } from "./ChannelItem";

type Props = {
	channels: ChannelList;
	activeChannelId?: string | undefined;
	/** The feed level shown on the home page, if that's where we are. */
	activeLevel?: Level | undefined;
	onCreate: () => void;
	/** Called when a link is followed (closes the mobile drawer). */
	onNavigate?: (() => void) | undefined;
};

/**
 * W2.1.1 — the ward as a "server": ward header with the green accent line
 * (T-W2.1.1.1), its channels (T-W2.1.1.2), and the four levels with the
 * 4-dot motif (T-W2.1.1.3).
 */
export function ChannelSidebar({ channels, activeChannelId, activeLevel, onCreate, onNavigate }: Props) {
	const { t, lang } = useT();
	const ward = channels.ward;
	return (
		<div className="flex flex-col gap-5 p-4">
			<header className="border-l-4 border-kenya-green pl-3">
				<h2 className="text-h2 text-ink">{ward?.name ?? t("level.ward")}</h2>
				<p className="text-small text-muted">
					{ward && `${ward.constituency} · ${ward.county}`}
					{ward && channels.memberCount !== undefined && " · "}
					{channels.memberCount !== undefined &&
						t("sidebar.members", { count: formatNumber(channels.memberCount, lang) })}
				</p>
			</header>

			<nav aria-label={t("sidebar.channels")} className="flex flex-col gap-1">
				<h3 className="px-3 pb-1 text-micro uppercase tracking-wide text-muted">{t("sidebar.channels")}</h3>
				<ul className="flex flex-col gap-0.5">
					{channels.items.map((c) => (
						<li key={c.channelId}>
							<ChannelItem
								channelId={c.channelId}
								name={c.name}
								category={c.category}
								readOnly={c.readOnly}
								active={c.channelId === activeChannelId}
								onNavigate={onNavigate}
							/>
						</li>
					))}
				</ul>
				<button
					type="button"
					onClick={onCreate}
					className="flex min-h-11 items-center gap-3 rounded-sm px-3 text-small text-muted hover:bg-surface-2 hover:text-ink focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent lg:min-h-9"
				>
					<PlusIcon className="h-4 w-4" />
					{t("channel.create")}
				</button>
			</nav>

			<nav aria-label={t("sidebar.levels")} className="flex flex-col gap-1 border-t border-border pt-4">
				<h3 className="px-3 pb-1 text-micro uppercase tracking-wide text-muted">{t("sidebar.levels")}</h3>
				<ul className="flex flex-col gap-0.5">
					{LEVELS.map((level) => (
						<li key={level}>
							<Link
								to="/"
								search={level === "ward" ? {} : { level }}
								aria-current={activeLevel === level ? "page" : undefined}
								onClick={onNavigate}
								className={cn(
									"flex min-h-11 items-center gap-3 rounded-sm px-3 text-body text-ink lg:min-h-9",
									"hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
									activeLevel === level && "bg-surface-2 font-medium",
								)}
							>
								<span aria-hidden="true" className="inline-flex">
									<LevelIndicator current={level} />
								</span>
								{t(`level.${level}`)}
							</Link>
						</li>
					))}
				</ul>
			</nav>
		</div>
	);
}
