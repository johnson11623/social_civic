import { useCallback, useState } from "react";

import type { Channel, ChannelPostsPage, Post } from "@/api/api-contract";
import { Badge } from "@/components/ui/Badge";
import { useToast } from "@/components/ui/Toast";
import { describeError, type Settled, settle } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { fetchChannelPosts } from "@/lib/loaders";
import { useLikeToggle } from "@/lib/use-like";
import { callApiEither } from "@/runtimes/get-runtime";
import { categoryDot } from "./ChannelItem";
import { Composer } from "./Composer";
import { Feed } from "./Feed";
import { LazyWhyModal as WhyModal } from "./LazyWhyModal";

type Props = { channel: Channel; posts: Settled<ChannelPostsPage> };

/**
 * W2.1.3 — one channel: header (#name, category, description, read-only),
 * a composer that posts here (hidden where the caller can't post,
 * T-W2.1.3.4), and only this channel's posts, newest first (T-W2.1.3.2).
 */
export function ChannelView({ channel, posts: initial }: Props) {
	const { t, lang } = useT();
	const toast = useToast();
	const [posts, setPosts] = useState<readonly Post[]>(initial.ok ? initial.value.items : []);
	const [cursor, setCursor] = useState(initial.ok ? initial.value.nextCursor : undefined);
	const [hasMore, setHasMore] = useState(initial.ok && initial.value.hasMore);
	const [error, setError] = useState(initial.ok ? undefined : describeError(initial.error, t));
	const [loadingMore, setLoadingMore] = useState(false);
	const [moreError, setMoreError] = useState<string>();
	const [pendingIds, setPendingIds] = useState<ReadonlySet<string>>(new Set());
	const [why, setWhy] = useState<Post | null>(null);

	const update = useCallback((postId: string, change: (p: Post) => Post) => {
		setPosts((all) => all.map((p) => (p.postId === postId ? change(p) : p)));
	}, []);
	const onLike = useLikeToggle(update);
	const onWhy = useCallback((p: Post) => setWhy(p), []);

	const reload = async () => {
		setError(undefined);
		const res = await fetchChannelPosts(channel.channelId);
		if (!res.ok) return setError(describeError(res.error, t));
		setPosts(res.value.items);
		setCursor(res.value.nextCursor);
		setHasMore(res.value.hasMore);
	};

	const loadMore = async () => {
		if (!cursor) return;
		setLoadingMore(true);
		setMoreError(undefined);
		const res = await fetchChannelPosts(channel.channelId, cursor);
		setLoadingMore(false);
		if (!res.ok) return setMoreError(describeError(res.error, t));
		setPosts((all) => {
			const seen = new Set(all.map((p) => p.postId));
			return [...all, ...res.value.items.filter((p) => !seen.has(p.postId))];
		});
		setCursor(res.value.nextCursor);
		setHasMore(res.value.hasMore);
	};

	// T-W2.1.3.3 — the post appears at the top of this channel at once.
	const submitPost = async (target: Channel, content: string): Promise<string | null> => {
		const tempId = `pending-${Date.now()}`;
		const temp: Post = {
			postId: tempId,
			channelId: target.channelId,
			channel: target.name,
			level: "ward",
			wardId: target.wardId,
			content,
			score: 0,
			state: "active",
			counts: { likes: 0, replies: 0 },
			liked: false,
			sponsored: false,
			createdAt: new Date().toISOString(),
		};
		setPosts((all) => [temp, ...all]);
		setPendingIds((ids) => new Set(ids).add(tempId));
		const res = settle(
			await callApiEither((api) =>
				api.posts.createPost({ path: { channelId: target.channelId }, payload: { content } }),
			),
		);
		setPendingIds((ids) => {
			const next = new Set(ids);
			next.delete(tempId);
			return next;
		});
		setPosts((all) =>
			res.ok ? all.map((p) => (p.postId === tempId ? res.value : p)) : all.filter((p) => p.postId !== tempId),
		);
		if (!res.ok) return `${t("error.postFailed")} ${describeError(res.error, t)}`;
		toast(t("composer.posted", { channel: target.name }));
		return null;
	};

	return (
		<div className="flex flex-col gap-4 py-6">
			<header className="flex flex-col gap-2">
				<div className="flex flex-wrap items-center gap-2">
					<span aria-hidden="true" className={cn("h-3 w-3 rounded-full", categoryDot[channel.category])} />
					<h1 className="text-h1">#{channel.name}</h1>
					<Badge>{t(`category.${channel.category}`)}</Badge>
					{channel.readOnly && <Badge variant="constituency">{t("channel.readOnly")}</Badge>}
				</div>
				{channel.description && <p className="text-body text-muted">{channel.description}</p>}
				{channel.memberCount !== undefined && (
					<p className="text-small text-muted">
						{t("sidebar.members", { count: formatNumber(channel.memberCount, lang) })}
					</p>
				)}
			</header>
			{channel.canPost ? (
				<Composer channels={[channel]} onSubmit={submitPost} />
			) : (
				<p className="rounded-md bg-surface p-3 text-small text-muted">{t("channel.readOnlyNote")}</p>
			)}
			<Feed
				posts={posts}
				loading={false}
				error={error}
				onRetry={() => void reload()}
				hasMore={hasMore}
				loadingMore={loadingMore}
				moreError={moreError}
				onLoadMore={() => void loadMore()}
				emptyMessage={t("channel.empty", { name: channel.name })}
				pendingIds={pendingIds}
				onLike={onLike}
				onWhy={onWhy}
			/>
			<WhyModal post={why} onClose={() => setWhy(null)} />
		</div>
	);
}
