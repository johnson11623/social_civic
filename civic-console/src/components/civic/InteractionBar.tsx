import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { HeartIcon, MessageIcon } from "@/components/ui/icons";
import { cn } from "@/lib/cn";
import { formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";

type Props = {
	likes: number;
	replies: number;
	liked: boolean;
	onLike: () => void;
	disabled?: boolean;
	/** Make the reply count a link to the post's thread. */
	threadOf?: string | undefined;
	/** Right-aligned extra action. */
	trailing?: ReactNode;
};

/**
 * Molecule: InteractionBar — like (a toggle, aria-pressed) and the reply
 * count. Icon + number on phones; the label is always in the accessible name.
 */
export function InteractionBar({
	likes,
	replies,
	liked,
	onLike,
	disabled = false,
	threadOf,
	trailing,
}: Props) {
	const { t, lang } = useT();
	return (
		<div className="flex items-center gap-2 border-t border-border pt-2">
			<button
				type="button"
				aria-pressed={liked}
				disabled={disabled}
				onClick={onLike}
				className={cn(
					"inline-flex min-h-11 min-w-11 items-center gap-2 rounded-sm px-3 text-small font-medium",
					"hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
					"disabled:pointer-events-none disabled:opacity-50",
					liked ? "text-kenya-red" : "text-muted",
				)}
			>
				<HeartIcon filled={liked} className="h-5 w-5" />
				<span className="sr-only md:not-sr-only">{t("post.like")}</span>{" "}
				<span>{formatNumber(likes, lang)}</span>
			</button>
			{threadOf ? (
				<Link
					to="/posts/$postId"
					params={{ postId: threadOf }}
					className="inline-flex min-h-11 items-center gap-2 rounded-sm px-3 text-small text-muted hover:bg-surface-2 hover:text-ink focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
				>
					<MessageIcon className="h-5 w-5" />
					<span className="sr-only md:not-sr-only">{t("post.replies")}</span>{" "}
					<span>{formatNumber(replies, lang)}</span>
				</Link>
			) : (
				<span className="inline-flex min-h-11 items-center gap-2 px-3 text-small text-muted">
					<MessageIcon className="h-5 w-5" />
					<span className="sr-only md:not-sr-only">{t("post.replies")}</span>{" "}
					<span>{formatNumber(replies, lang)}</span>
				</span>
			)}
			{trailing && <div className="ml-auto">{trailing}</div>}
		</div>
	);
}
