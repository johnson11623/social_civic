import type { Post } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Spinner } from "@/components/ui/Spinner";
import { useT } from "@/lib/i18n/I18nProvider";
import { PostCard } from "./PostCard";

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
};

/**
 * T-W1.4.1.2 — the post list with an explicit "Load more" (no infinite
 * scroll: data costs and a findable footer, Web App Design §5.1).
 * T-W1.4.1.7 — skeletons shaped like cards while a page replaces the list.
 */
export function Feed(props: Props) {
	const { t } = useT();
	const { posts, loading, error } = props;

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
				{posts.map((post) => (
					<PostCard
						key={post.postId}
						post={post}
						pending={props.pendingIds.has(post.postId)}
						onLike={props.onLike}
						onWhy={props.onWhy}
					/>
				))}
			</div>
			{props.moreError && (
				<p role="alert" className="text-small text-danger">
					{props.moreError}
				</p>
			)}
			{props.hasMore ? (
				<Button
					variant="secondary"
					className="self-center"
					disabled={props.loadingMore}
					onClick={props.onLoadMore}
				>
					{props.loadingMore ? <Spinner label={t("feed.loading")} /> : t("feed.loadMore")}
				</Button>
			) : (
				<p className="text-center text-small text-muted">{t("feed.end")}</p>
			)}
		</div>
	);
}
