import { Fragment, useEffect, useRef, useState } from "react";

import type { Post } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Spinner } from "@/components/ui/Spinner";
import { useT } from "@/lib/i18n/I18nProvider";
import { PostCard } from "./PostCard";
import { SponsoredCard } from "./SponsoredCard";

type Props = {
	posts: readonly Post[];
	/** Replacing the whole list (first load, tab change). */
	loading: boolean;
	error?: string | undefined;
	onRetry: () => void;
	hasMore: boolean;
	loadingMore: boolean;
	moreError?: string | undefined;
	onLoadMore: () => void;
	emptyMessage: string;
	pendingIds: ReadonlySet<string>;
	onLike: (post: Post) => void;
	onWhy: (post: Post) => void;
	onReport?: ((post: Post) => void) | undefined;
};

/** Pages fetched automatically before asking, so the footer stays reachable. */
const AUTO_PAGES = 5;
/** Fetch the next page this far before the reader reaches the end. */
const AHEAD = "1200px 0px";

/** The sponsored slot sits after this many posts (or after the last, if fewer). */
const SPONSORED_AFTER = 2;

/** Data savers choose when to spend data. */
function autoLoadAllowed(): boolean {
	if (typeof IntersectionObserver === "undefined") return false;
	const net = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection;
	return !net?.saveData;
}

/**
 * T-W1.4.1.2 — the post list. The next page loads about a screen and a half
 * before the reader gets there (as on LinkedIn), for up to AUTO_PAGES pages;
 * then, on error, or with data saver on, "Load more" asks first (data costs
 * and a findable footer, Web App Design §5.1).
 * T-W1.4.1.7 — skeletons shaped like cards while a page replaces the list.
 */
export function Feed(props: Props) {
	const { t } = useT();
	const { posts, loading, error } = props;
	const sentinel = useRef<HTMLDivElement>(null);
	const loadMore = useRef(props.onLoadMore);
	loadMore.current = props.onLoadMore;
	const [auto] = useState(autoLoadAllowed);
	const [autoLoads, setAutoLoads] = useState(0);

	// A new list (tab change, retry) starts counting again.
	useEffect(() => {
		if (loading) setAutoLoads(0);
	}, [loading]);

	const canAuto = auto && props.hasMore && !props.loadingMore && !props.moreError && autoLoads < AUTO_PAGES;
	// biome-ignore lint/correctness/useExhaustiveDependencies: re-arm after each page (posts.length)
	useEffect(() => {
		const el = sentinel.current;
		if (!canAuto || !el) return;
		const io = new IntersectionObserver(
			([e]) => {
				if (!e?.isIntersecting) return;
				io.disconnect();
				setAutoLoads((n) => n + 1);
				loadMore.current();
			},
			{ rootMargin: AHEAD },
		);
		io.observe(el);
		return () => io.disconnect();
	}, [canAuto, posts.length]);

	if (loading) {
		return (
			<div aria-busy="true" className="flex flex-col gap-4">
				<span className="sr-only" role="status">
					{t("feed.loading")}
				</span>
				{[0, 1, 2].map((i) => (
					<div key={i} className="flex flex-col gap-3 rounded-md border border-border p-4">
						<div className="flex items-center gap-3">
							<Skeleton className="h-10 w-10 rounded-full" />
							<div className="flex flex-1 flex-col gap-2">
								<Skeleton className="h-4 w-1/3" />
								<Skeleton className="h-3 w-1/4" />
							</div>
						</div>
						<Skeleton className="h-4 w-full" />
						<Skeleton className="h-4 w-5/6" />
						<Skeleton className="h-11 w-1/3" />
					</div>
				))}
			</div>
		);
	}

	if (error) {
		return (
			<div role="alert" className="flex flex-col items-start gap-3 rounded-md border border-border p-4">
				<p className="text-body">{t("feed.error")}</p>
				<p className="text-small text-muted">{error}</p>
				<Button variant="secondary" onClick={props.onRetry}>
					{t("common.retry")}
				</Button>
			</div>
		);
	}

	if (posts.length === 0) {
		return (
			<p className="rounded-md border border-dashed border-border p-6 text-body text-muted">
				{props.emptyMessage}
			</p>
		);
	}

	return (
		<div className="flex flex-col gap-4">
			<div role="feed" aria-busy={props.loadingMore} className="flex flex-col gap-4">
				{posts.map((post, i) => (
					<Fragment key={post.postId}>
						<PostCard
							post={post}
							priority={i === 0}
							pending={props.pendingIds.has(post.postId)}
							linkThread
							onLike={props.onLike}
							onWhy={props.onWhy}
							onReport={props.onReport}
						/>
						{/* Below xl the right-hand column (and its sponsored slot) isn't shown. */}
						{i === Math.min(SPONSORED_AFTER, posts.length) - 1 && (
							<SponsoredCard inFeed className="min-h-0 xl:hidden" />
						)}
					</Fragment>
				))}
			</div>
			<div ref={sentinel} aria-hidden="true" />
			{props.moreError && (
				<p role="alert" className="text-small text-danger">
					{props.moreError}
				</p>
			)}
			{props.hasMore ? (
				props.loadingMore || !canAuto ? (
					<Button
						variant="secondary"
						className="self-center"
						disabled={props.loadingMore}
						onClick={props.onLoadMore}
					>
						{props.loadingMore ? <Spinner label={t("feed.loading")} /> : t("feed.loadMore")}
					</Button>
				) : null
			) : (
				<p className="text-center text-small text-muted">{t("feed.end")}</p>
			)}
		</div>
	);
}
