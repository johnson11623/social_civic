import type { Level, Post, ReasonCode } from "@/api/api-contract";
import { Badge } from "@/components/ui/Badge";
import { formatDate } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { appealOpen, REASONS } from "@/lib/moderation";

type Props = {
	post: Post;
	/** The viewer wrote the post: offer the appeal while the window is open. */
	canAppeal?: boolean;
	onAppeal?: (() => void) | undefined;
};

const isReason = (r: string): r is ReasonCode => (REASONS as readonly string[]).includes(r);

/**
 * T-W1.4.3.5 — why a post was removed or frozen, by which level's
 * moderator, and (for its author) the appeal while the 14-day window lasts.
 */
export function ModerationNotice({ post, canAppeal = false, onAppeal }: Props) {
	const { t, lang } = useT();
	const m = post.moderation;
	const removed = post.state === "tombstoned";
	const reason = m && isReason(m.reasonCode) ? t(`reason.${m.reasonCode}`) : "—";
	const level: Level = post.level;
	return (
		<div role="note" className="flex flex-col gap-2 rounded-md border border-danger/20 bg-danger/5 p-3">
			<div className="flex flex-wrap items-center gap-2">
				<Badge variant="danger">{removed ? t("moderation.removedBadge") : t("moderation.frozenBadge")}</Badge>
				<p className="flex-1 text-small text-ink">
					{removed
						? t("moderation.removed", { level: t(`level.${level}`), reason })
						: t("moderation.frozen", { reason })}
				</p>
			</div>
			{canAppeal && m?.appealDueAt && (
				<div className="flex flex-wrap items-center gap-3">
					{appealOpen(m.appealDueAt) ? (
						<>
							<p className="text-small text-muted">
								{t("appeal.until", { date: formatDate(m.appealDueAt, lang) })}
							</p>
							<button
								type="button"
								onClick={onAppeal}
								className="min-h-11 rounded-sm text-small font-medium text-accent underline underline-offset-2 hover:text-accent-hover focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
							>
								{t("appeal.action")}
							</button>
						</>
					) : (
						<p className="text-small text-muted">
							{t("appeal.closed", { date: formatDate(m.appealDueAt, lang) })}
						</p>
					)}
				</div>
			)}
		</div>
	);
}
