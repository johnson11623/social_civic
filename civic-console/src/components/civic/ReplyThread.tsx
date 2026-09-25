import { memo } from "react";

import type { Post } from "@/api/api-contract";
import { Avatar } from "@/components/ui/Avatar";
import { HeartIcon } from "@/components/ui/icons";
import { cn } from "@/lib/cn";
import { formatDate, formatNumber, formatRelative } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { ReplyComposer } from "./ReplyComposer";

/** Deeper replies stay at this indent so threads remain readable on phones. */
export const MAX_INDENT = 3;

type Props = {
	rootId: string;
	replies: readonly Post[];
	pendingIds: ReadonlySet<string>;
	replyingTo: string | null;
	onReplyTo: (postId: string | null) => void;
	onReply: (parent: Post, content: string) => Promise<string | null>;
	onLike: (post: Post) => void;
};

/**
 * T-W1.4.3.2 — replies nested under what they answer, oldest first. Each
 * level is a list, so screen readers announce depth and size.
 */
export function ReplyThread(props: Props) {
	const children = new Map<string, Post[]>();
	for (const r of props.replies) {
		const parent = r.parentId ?? props.rootId;
		children.set(parent, [...(children.get(parent) ?? []), r]);
	}
	return <ReplyList parentId={props.rootId} depth={0} tree={children} {...props} />;
}

function ReplyList({
	parentId,
	depth,
	tree,
	...props
}: Props & { parentId: string; depth: number; tree: Map<string, Post[]> }) {
	const items = tree.get(parentId);
	if (!items?.length) return null;
	return (
		<ul
			className={cn(
				"flex flex-col gap-3",
				depth > 0 && depth <= MAX_INDENT && "border-l-2 border-border pl-3 md:pl-4",
			)}
		>
			{items.map((reply) => (
				<li key={reply.postId} className="flex flex-col gap-3">
					<ReplyItem
						reply={reply}
						pending={props.pendingIds.has(reply.postId)}
						replying={props.replyingTo === reply.postId}
						onReplyTo={props.onReplyTo}
						onReply={props.onReply}
						onLike={props.onLike}
					/>
					<ReplyList parentId={reply.postId} depth={depth + 1} tree={tree} {...props} />
				</li>
			))}
		</ul>
	);
}

const ReplyItem = memo(function ReplyItem({
	reply,
	pending,
	replying,
	onReplyTo,
	onReply,
	onLike,
}: {
	reply: Post;
	pending: boolean;
	replying: boolean;
	onReplyTo: Props["onReplyTo"];
	onReply: Props["onReply"];
	onLike: Props["onLike"];
}) {
	const { t, lang } = useT();
	const name = reply.author?.displayName ?? "—";
	if (reply.state === "tombstoned" || reply.content === null) {
		return <article className="rounded-md bg-surface p-3 text-small text-muted">{t("post.removed")}</article>;
	}
	return (
		<article aria-busy={pending || undefined} className={cn("flex flex-col gap-2", pending && "opacity-70")}>
			<header className="flex items-center gap-2">
				<Avatar name={name} src={reply.author?.avatarUrl} size="sm" />
				<span className="truncate font-medium text-small text-ink">{name}</span>
				<span className="text-micro text-muted">
					{pending ? (
						t("post.pending")
					) : (
						<time dateTime={reply.createdAt} title={formatDate(reply.createdAt, lang)}>
							{formatRelative(reply.createdAt, lang)}
						</time>
					)}
				</span>
			</header>
			<p className="whitespace-pre-wrap break-words text-body text-ink">{reply.content}</p>
			{!pending && (
				<div className="-ml-3 flex items-center gap-1">
					<button
						type="button"
						aria-pressed={reply.liked ?? false}
						onClick={() => onLike(reply)}
						className={cn(
							"inline-flex min-h-11 items-center gap-2 rounded-sm px-3 text-small",
							"hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
							reply.liked ? "text-kenya-red" : "text-muted",
						)}
					>
						<HeartIcon filled={reply.liked ?? false} className="h-4 w-4" />
						<span className="sr-only">{t("post.like")}</span>{" "}
						<span>{formatNumber(reply.counts.likes, lang)}</span>
					</button>
					<button
						type="button"
						aria-expanded={replying}
						onClick={() => onReplyTo(replying ? null : reply.postId)}
						className="inline-flex min-h-11 items-center rounded-sm px-3 text-small text-muted hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
					>
						{t("reply.action")}
					</button>
				</div>
			)}
			{replying && (
				<ReplyComposer
					label={t("reply.to", { name })}
					autoFocus
					onSubmit={(content) => onReply(reply, content)}
					onCancel={() => onReplyTo(null)}
				/>
			)}
		</article>
	);
});
