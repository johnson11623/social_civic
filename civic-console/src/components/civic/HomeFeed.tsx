import { useCallback, useId, useRef, useState } from "react";

import type { Channel, ChannelList, FeedPage, Level, Post } from "@/api/api-contract";
import { useToast } from "@/components/ui/Toast";
import { describeError, type Settled, settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { useLikeToggle } from "@/lib/use-like";
import { callApiEither } from "@/runtimes/get-runtime";
import { Composer } from "./Composer";
import { Feed } from "./Feed";
import { LevelTabs, tabId } from "./LevelTabs";
import { WhyModal } from "./WhyModal";

export type HomeData = {
	level: Level;
	feed: Settled<FeedPage>;
	channels: Settled<ChannelList>;
};

type Props = HomeData & {
	/** Keep the URL in step with the selected tab (shareable, back button). */
	onLevelChange?: (level: Level) => void;
};

export const FEED_PAGE_SIZE = 20;

export const fetchFeed = (level: Level, cursor?: string) =>
	callApiEither((api) =>
		api.posts.feed({ urlParams: { level, limit: FEED_PAGE_SIZE, ...(cursor ? { cursor } : {}) } }),
	).then(settle);

/**
 * W1.4.1 — the signed-in home: level tabs, the composer, and the feed.
 * Owns the list so likes and new posts update one card at a time.
 */
export function HomeFeed({ level: initialLevel, feed, channels, onLevelChange }: Props) {
	const { t } = useT();
	const toast = useToast();
	const panelId = useId();

	const [level, setLevel] = useState(initialLevel);
	const [posts, setPosts] = useState<readonly Post[]>(feed.ok ? feed.value.items : []);
	const [cursor, setCursor] = useState(feed.ok ? feed.value.nextCursor : undefined);
	const [hasMore, setHasMore] = useState(feed.ok && feed.value.hasMore);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState(feed.ok ? undefined : describeError(feed.error, t));
	const [loadingMore, setLoadingMore] = useState(false);
	const [moreError, setMoreError] = useState<string>();
	const [pendingIds, setPendingIds] = useState<ReadonlySet<string>>(new Set());
	const [why, setWhy] = useState<Post | null>(null);

	// Responses for a tab the user already left are dropped.
	const request = useRef(0);

	const load = useCallback(
		async (next: Level) => {
			const id = ++request.current;
			setLoading(true);
			setError(undefined);
			setMoreError(undefined);
			const res = await fetchFeed(next);
			if (id !== request.current) return;
			setLoading(false);
			if (!res.ok) {
				setError(describeError(res.error, t));
				return;
			}
			setPosts(res.value.items);
			setCursor(res.value.nextCursor);
			setHasMore(res.value.hasMore);
		},
		[t],
	);

	const selectLevel = (next: Level) => {
		if (next === level) return;
		setLevel(next);
		onLevelChange?.(next);
		void load(next);
	};

	const loadMore = async () => {
		if (!cursor) return;
		const id = request.current;
		setLoadingMore(true);
		setMoreError(undefined);
		const res = await fetchFeed(level, cursor);
		if (id !== request.current) return;
		setLoadingMore(false);
		if (!res.ok) {
			setMoreError(describeError(res.error, t));
			return;
		}
		setPosts((all) => {
			const seen = new Set(all.map((p) => p.postId));
			return [...all, ...res.value.items.filter((p) => !seen.has(p.postId))];
		});
		setCursor(res.value.nextCursor);
		setHasMore(res.value.hasMore);
	};

	const update = useCallback((postId: string, change: (p: Post) => Post) => {
		setPosts((all) => all.map((p) => (p.postId === postId ? change(p) : p)));
	}, []);

	const onLike = useLikeToggle(update);

	const onWhy = useCallback((post: Post) => setWhy(post), []);

	// T-W1.4.2.5 — the post shows at the top of the ward feed straight away,
	// marked as posting; the server's copy replaces it.
	const submitPost = async (channel: Channel, content: string): Promise<string | null> => {
		const tempId = `pending-${Date.now()}`;
		const optimistic = level === "ward";
		if (optimistic) {
			const temp: Post = {
				postId: tempId,
				channelId: channel.channelId,
				channel: channel.name,
				level: "ward",
				wardId: channel.wardId,
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
		}
		const res = settle(
			await callApiEither((api) =>
				api.posts.createPost({ path: { channelId: channel.channelId }, payload: { content } }),
			),
		);
		if (optimistic) {
			setPendingIds((ids) => {
				const next = new Set(ids);
				next.delete(tempId);
				return next;
			});
			setPosts((all) =>
				res.ok
					? all.map((p) => (p.postId === tempId ? res.value : p))
					: all.filter((p) => p.postId !== tempId),
			);
		}
		if (!res.ok) return `${t("error.postFailed")} ${describeError(res.error, t)}`;
		toast(t("composer.posted", { channel: channel.name }));
		if (!optimistic) selectLevel("ward");
		return null;
	};

	return (
		<div className="flex flex-col gap-4">
			<LevelTabs value={level} onChange={selectLevel} panelId={panelId} />
			{channels.ok ? (
				<Composer channels={channels.value.items} onSubmit={submitPost} />
			) : (
				<p role="alert" className="text-small text-danger">
					{describeError(channels.error, t)}
				</p>
			)}
			<section id={panelId} role="tabpanel" aria-labelledby={tabId(level)}>
				<Feed
					posts={posts}
					loading={loading}
					error={error}
					onRetry={() => void load(level)}
					hasMore={hasMore}
					loadingMore={loadingMore}
					moreError={moreError}
					onLoadMore={() => void loadMore()}
					emptyMessage={level === "ward" ? t("feed.empty.ward") : t("feed.empty.level")}
					pendingIds={pendingIds}
					onLike={onLike}
					onWhy={onWhy}
				/>
			</section>
			<WhyModal post={why} onClose={() => setWhy(null)} />
		</div>
	);
}
