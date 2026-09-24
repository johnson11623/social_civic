import { createFileRoute, Link, redirect } from "@tanstack/react-router";

import { fetchThread, PostDetail } from "@/components/civic/PostDetail";
import { describeError, settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { callApiEither } from "@/runtimes/get-runtime";

/** T-W1.4.3.1 — a post and its replies. A reply's link opens its thread. */
export const Route = createFileRoute("/_authed/posts/$postId")({
	loader: async ({ params }) => {
		const post = settle(await callApiEither((api) => api.posts.post({ path: { postId: params.postId } })));
		if (post.ok && post.value.rootId) {
			throw redirect({ to: "/posts/$postId", params: { postId: post.value.rootId } });
		}
		const thread = post.ok ? await fetchThread(params.postId) : undefined;
		return { post, thread };
	},
	component: PostPage,
});

function PostPage() {
	const { post, thread } = Route.useLoaderData();
	const { t } = useT();
	return (
		<main id="main" className="mx-auto flex max-w-2xl flex-col gap-4 px-4 py-6">
			<Link
				to="/"
				className="inline-flex min-h-11 w-fit items-center gap-2 rounded-sm text-small text-accent hover:text-accent-hover focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
			>
				<span aria-hidden="true">←</span>
				{t("post.back")}
			</Link>
			<h1 className="sr-only">{t("post.title")}</h1>
			{post.ok && thread ? (
				<PostDetail key={post.value.postId} post={post.value} thread={thread} />
			) : (
				<p role="alert" className="rounded-md border border-border p-4 text-body">
					{post.ok || post.error._tag === "UpstreamError"
						? t("post.unavailable")
						: describeError(post.error, t)}
				</p>
			)}
		</main>
	);
}
