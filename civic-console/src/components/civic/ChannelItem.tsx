import { Link } from "@tanstack/react-router";

import type { ChannelCategory } from "@/api/api-contract";
import { cn } from "@/lib/cn";
import { formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";

/** Channel category dots (Web App Design: subtle coloured dots, not emoji). */
export const categoryDot: Record<ChannelCategory, string> = {
	general: "bg-muted",
	services: "bg-accent",
	opportunities: "bg-kenya-green",
	safety: "bg-kenya-red",
	culture: "bg-warning",
};

type Props = {
	channelId: string;
	name: string;
	category: ChannelCategory;
	readOnly: boolean;
	active: boolean;
	unreadCount?: number | undefined;
	onNavigate?: (() => void) | undefined;
};

/**
 * T-W2.1.1.2 — a channel in the sidebar: category dot, #name, read-only
 * marker, unread badge when there is one. A link: Tab reaches it, Enter
 * opens it (T-W2.1.1.5); the current channel is aria-current.
 */
export function ChannelItem({ channelId, name, category, readOnly, active, unreadCount, onNavigate }: Props) {
	const { t, lang } = useT();
	return (
		<Link
			to="/channels/$channelId"
			params={{ channelId }}
			aria-current={active ? "page" : undefined}
			onClick={onNavigate}
			className={cn(
				"flex min-h-11 items-center gap-3 rounded-sm px-3 text-body text-ink lg:min-h-9",
				"hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
				active && "bg-surface-2 font-medium",
			)}
		>
			<span aria-hidden="true" className={cn("h-2 w-2 shrink-0 rounded-full", categoryDot[category])} />
			<span className="flex-1 truncate">#{name}</span>
			{readOnly && <span className="text-micro text-muted">{t("channel.readOnly")}</span>}
			{unreadCount ? (
				<span className="rounded-full bg-accent px-2 py-0.5 text-micro text-on-accent">
					{formatNumber(unreadCount, lang)}
				</span>
			) : null}
		</Link>
	);
}
