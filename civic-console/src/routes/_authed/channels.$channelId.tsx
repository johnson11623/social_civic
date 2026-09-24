import { createFileRoute } from "@tanstack/react-router";

import { ChannelView } from "@/components/civic/ChannelView";
import { WardShell } from "@/components/layout/WardShell";
import { describeError, settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { fetchChannelPosts, fetchChannels } from "@/lib/loaders";
import { callApiEither } from "@/runtimes/get-runtime";

/** W2.1.3 — a ward channel inside the ward layout. */
export const Route = createFileRoute("/_authed/channels/$channelId")({
	loader: async ({ params }) => {
		const [channel, posts, channels] = await Promise.all([
			callApiEither((api) => api.posts.channel({ path: { channelId: params.channelId } })).then(settle),
			fetchChannelPosts(params.channelId),
			fetchChannels(),
		]);
		return { channel, posts, channels };
	},
	component: ChannelPage,
});

function ChannelPage() {
	const { channel, posts, channels } = Route.useLoaderData();
	const { channelId } = Route.useParams();
	const { t } = useT();
	return (
		<main id="main" className="pb-28 md:pb-12">
			<WardShell channels={channels} activeChannelId={channelId}>
				<div className="mx-auto max-w-2xl">
					{channel.ok ? (
						<ChannelView key={channel.value.channelId} channel={channel.value} posts={posts} />
					) : (
						<p role="alert" className="my-6 rounded-md border border-border p-4 text-body">
							{channel.error._tag === "UpstreamError"
								? t("channel.unavailable")
								: describeError(channel.error, t)}
						</p>
					)}
				</div>
			</WardShell>
		</main>
	);
}
