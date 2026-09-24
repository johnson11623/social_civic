import { useCallback, useState } from "react";

import type { Post, ThreadPage } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { Spinner } from "@/components/ui/Spinner";
import { useToast } from "@/components/ui/Toast";
import { describeError, type Settled, settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { fetchThread } from "@/lib/loaders";
import { useLikeToggle } from "@/lib/use-like";
import { callApiEither } from "@/runtimes/get-runtime";
import { LazyWhyModal as WhyModal } from "./LazyWhyModal";
import { PostCard } from "./PostCard";
import { ReplyComposer } from "./ReplyComposer";
import { ReplyThread } from "./ReplyThread";

type Props = { post: Post; thread: Settled<ThreadPage> };

/**
 * W1.4.3 — a post with its whole conversation: nested replies, an inline
 * reply box under the post and under each reply, optimistic like and reply.
 */
export function PostDetail({ post: initialPost, thread }: Props) {
	const { t } = useT();
	const toast = useToast();
	const [post, setPost] = useState(initialPost);
	const [replies, setReplies] = useState<readonly Post[]>(thread.ok ? thread.value.items : []);
	const [cursor, setCursor] = useState(thread.ok ? thread.value.nextCursor : undefined);
	const [hasMore, setHasMore] = useState(thread.ok && thread.value.hasMore);
	const [threadError, setThreadError] = useState(thread.ok ? undefined : describeError(thread.error, t));
	const [loadingMore, setLoadingMore] = useState(false);
	const [pendingIds, setPendingIds] = useState<ReadonlySet<string>>(new Set());
	const [replyingTo, setReplyingTo] = useState<string | null>(null);
	const [why, setWhy] = useState<Post | null>(null);

	const update = useCallback(
		(postId: string, change: (p: Post) => Post) => {
			if (postId === initialPost.postId) setPost(change);
			else setReplies((all) => all.map((r) => (r.postId === postId ? change(r) : r)));
		},
		[initialPost.postId],
	);
	const onLike = useLikeToggle(update);
	const onWhy = useCallback((p: Post) => setWhy(p), []);

	const loadMore = async () => {
		if (!cursor) return;
		setLoadingMore(true);
		const res = await fetchThread(post.postId, cursor);
		setLoadingMore(false);
		if (!res.ok) return setThreadError(describeError(res.error, t));
		setThreadError(undefined);
		setReplies((all) => {
			const seen = new Set(all.map((r) => r.postId));
			return [...all, ...res.value.items.filter((r) => !seen.has(r.postId))];
		});
		setCursor(res.value.nextCursor);
		setHasMore(res.value.hasMore);
	};

	// T-W1.4.3.3 — the reply shows under its parent at once, marked as
	// posting; the server's copy replaces it, or it's taken back on failure.
	const onReply = useCallback(
		async (parent: Post, content: string): Promise<string | null> => {
			const tempId = `pending-${Date.now()}`;
			const temp: Post = {
				postId: tempId,
				channelId: parent.channelId,
				level: parent.level,
				wardId: parent.wardId,
				content,
				score: 0,
				state: "active",
				counts: { likes: 0, replies: 0 },
				liked: false,
				sponsored: false,
				rootId: initialPost.postId,
				parentId: parent.postId,
				createdAt: new Date().toISOString(),
			};
			setReplies((all) => [...all, temp]);
			setPendingIds((ids) => new Set(ids).add(tempId));
			const res = settle(
				await callApiEither((api) =>
					api.posts.reply({ path: { postId: parent.postId }, payload: { content } }),
				),
			);
			setPendingIds((ids) => {
				const next = new Set(ids);
				next.delete(tempId);
				return next;
			});
			if (!res.ok) {
				setReplies((all) => all.filter((r) => r.postId !== tempId));
				return `${t("error.replyFailed")} ${describeError(res.error, t)}`;
			}
			const bump = (p: Post) => ({ ...p, counts: { ...p.counts, replies: p.counts.replies + 1 } });
			setReplies((all) =>
				all.map((r) => (r.postId === tempId ? res.value : r.postId === parent.postId ? bump(r) : r)),
			);
			setPost((p) => bump(p));
			toast(t("reply.posted"));
			return null;
		},
		[initialPost.postId, t, toast],
	);

	return (
		<div className="flex flex-col gap-6">
			<PostCard post={post} onLike={onLike} onWhy={onWhy} />
			{post.state === "active" && (
				<ReplyComposer label={t("reply.label")} onSubmit={(content) => onReply(post, content)} />
			)}
			<section aria-labelledby="replies-heading" className="flex flex-col gap-4">
				<h2 id="replies-heading" className="text-h2">
					{t("post.replies")}
				</h2>
				{threadError && (
					<p role="alert" className="text-small text-danger">
						{threadError}
					</p>
				)}
				{replies.length === 0 && !threadError ? (
					<p className="text-body text-muted">{t("thread.empty")}</p>
				) : (
					<ReplyThread
						rootId={post.postId}
						replies={replies}
						pendingIds={pendingIds}
						replyingTo={replyingTo}
						onReplyTo={setReplyingTo}
						onReply={onReply}
						onLike={onLike}
					/>
				)}
				{hasMore && (
					<Button
						variant="secondary"
						className="self-center"
						disabled={loadingMore}
						onClick={() => void loadMore()}
					>
						{loadingMore ? <Spinner label={t("feed.loading")} /> : t("thread.more")}
					</Button>
				)}
			</section>
			<WhyModal post={why} onClose={() => setWhy(null)} />
		</div>
	);
}
