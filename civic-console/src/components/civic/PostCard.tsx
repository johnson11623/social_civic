import { memo } from "react";

import type { Post } from "@/api/api-contract";
import { Avatar } from "@/components/ui/Avatar";
import { LevelIndicator } from "@/components/ui/LevelIndicator";
import { cn } from "@/lib/cn";
import { formatDate, formatRelative } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { ElevationBanner } from "./ElevationBanner";
import { InteractionBar } from "./InteractionBar";
import { ModerationNotice } from "./ModerationNotice";
import { PostMedia } from "./PostMedia";
import { SponsoredLabel } from "./SponsoredLabel";

type Props = {
	post: Post;
	onLike: (post: Post) => void;
	onWhy: (post: Post) => void;
	/** Optimistic: not yet confirmed by the server. */
	pending?: boolean;
	/** Link the reply count to the thread (feed cards; not the detail page). */
	linkThread?: boolean;
	/** Offer "Report" (EPIC 3.1.1). */
	onReport?: ((post: Post) => void) | undefined;
	/** The viewer wrote this post: removed/frozen notices offer the appeal. */
	viewerIsAuthor?: boolean;
	onAppeal?: ((post: Post) => void) | undefined;
};

/**
 * T-W1.4.1.3 — a post in the feed: author, #channel, level and age; the
 * sponsored disclosure and elevation banner when they apply; content; likes
 * and replies. Removed posts keep their place with a notice.
 *
 * T-W1.4.1.8 — memoized: the feed replaces only the post that changed, so
 * liking one card never re-renders the rest.
 */
export const PostCard = memo(function PostCard({
	post,
	onLike,
	onWhy,
	pending = false,
	linkThread = false,
	onReport,
	viewerIsAuthor = false,
	onAppeal,
}: Props) {
	const { t, lang } = useT();
	const name = post.author?.displayName ?? "—";

	const notice = (post.state === "tombstoned" || post.state === "frozen") && post.moderation && (
		<ModerationNotice
			post={post}
			canAppeal={viewerIsAuthor}
			onAppeal={onAppeal ? () => onAppeal(post) : undefined}
		/>
	);
	if (post.state === "tombstoned" || post.content === null) {
		return (
			<article className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4 text-small text-muted">
				{notice || t("post.removed")}
			</article>
		);
	}
	const frozen = post.state === "frozen";

	return (
		<article
			aria-busy={pending || undefined}
			className={cn(
				"flex flex-col gap-3 rounded-md border border-border bg-paper p-4",
				post.sponsored && "border-sponsored",
				pending && "opacity-70",
			)}
		>
			<header className="flex items-start gap-3">
				<Avatar name={name} size="md" />
				<div className="flex min-w-0 flex-1 flex-col">
					<div className="flex flex-wrap items-center gap-x-2">
						<span className="truncate font-medium text-ink">{name}</span>
						<LevelIndicator current={post.level} />
					</div>
					<p className="truncate text-small text-muted">
						{post.channel && <span>#{post.channel} · </span>}
						{pending ? (
							t("post.pending")
						) : (
							<time dateTime={post.createdAt} title={formatDate(post.createdAt, lang)}>
								{formatRelative(post.createdAt, lang)}
							</time>
						)}
					</p>
				</div>
			</header>
			{post.sponsored && post.sponsoredLabel && <SponsoredLabel label={post.sponsoredLabel} />}
			{post.level !== "ward" && <ElevationBanner level={post.level} onWhy={() => onWhy(post)} />}
			{notice}
			{post.content && <p className="whitespace-pre-wrap break-words text-body text-ink">{post.content}</p>}
			{post.media && <PostMedia media={post.media} authorName={post.author?.displayName} />}
			<InteractionBar
				likes={post.counts.likes}
				replies={post.counts.replies}
				liked={post.liked ?? false}
				onLike={() => onLike(post)}
				disabled={pending || frozen}
				threadOf={linkThread && !pending ? post.postId : undefined}
				trailing={
					pending ? undefined : (
						<div className="flex items-center">
							{post.level === "ward" && (
								<button
									type="button"
									onClick={() => onWhy(post)}
									className="min-h-11 rounded-sm px-2 text-small text-muted underline-offset-2 hover:text-accent hover:underline focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
								>
									{t("post.why")}
								</button>
							)}
							{onReport && (
								<button
									type="button"
									onClick={() => onReport(post)}
									className="min-h-11 rounded-sm px-2 text-small text-muted hover:text-danger focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
								>
									{t("post.report")}
								</button>
							)}
						</div>
					)
				}
			/>
		</article>
	);
});
