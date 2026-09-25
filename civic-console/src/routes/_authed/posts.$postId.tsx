import { createFileRoute, Link, redirect } from "@tanstack/react-router";

import { PostDetail } from "@/components/civic/PostDetail";
import { WardShell } from "@/components/layout/WardShell";
import { describeError, settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { fetchChannels, fetchRoles, fetchThread } from "@/lib/loaders";
import { moderatedLevels } from "@/lib/moderation";
import { callApiEither } from "@/runtimes/get-runtime";

/** T-W1.4.3.1 — a post and its replies. A reply's link opens its thread. */
export const Route = createFileRoute("/_authed/posts/$postId")({
	loader: async ({ params }) => {
		const [post, thread, channels, roles] = await Promise.all([
			callApiEither((api) => api.posts.post({ path: { postId: params.postId } })).then(settle),
			fetchThread(params.postId),
			fetchChannels(),
			fetchRoles(),
		]);
		if (post.ok && post.value.rootId) {
			throw redirect({ to: "/posts/$postId", params: { postId: post.value.rootId } });
		}
		const isModerator = roles.ok && moderatedLevels(roles.value.items).length > 0;
		return { post, thread, channels, isModerator };
	},
	component: PostPage,
});

function PostPage() {
	const { post, thread, channels, isModerator } = Route.useLoaderData();
	const { session } = Route.useRouteContext();
	const { t } = useT();
	return (
		<main id="main" className="pb-28 md:pb-12">
			<WardShell
				channels={channels}
				activeChannelId={post.ok ? post.value.channelId : undefined}
				isModerator={isModerator}
			>
				<div className="mx-auto flex max-w-2xl flex-col gap-4 py-6">
					<Link
						to="/"
						className="inline-flex min-h-11 w-fit items-center gap-2 rounded-sm text-small text-accent hover:text-accent-hover focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
					>
						<span aria-hidden="true">←</span>
						{t("post.back")}
					</Link>
					<h1 className="sr-only">{t("post.title")}</h1>
					{post.ok ? (
						<PostDetail
							key={post.value.postId}
							post={post.value}
							thread={thread}
							viewerId={session?.subject}
						/>
					) : (
						<p role="alert" className="rounded-md border border-border p-4 text-body">
							{post.ok || post.error._tag === "UpstreamError"
								? t("post.unavailable")
								: describeError(post.error, t)}
						</p>
					)}
				</div>
			</WardShell>
		</main>
	);
}
