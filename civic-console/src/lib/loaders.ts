/**
 * Data fetchers shared by route loaders and components. Kept apart from the
 * components: a route's loader stays in the main bundle, so importing a
 * fetcher from a component module would pull that whole UI into the first
 * download and defeat route code-splitting (3G budget).
 */
import type { Level, QueueParams } from "@/api/api-contract";
import { settle } from "@/lib/api-errors";
import { callApiEither } from "@/runtimes/get-runtime";

export const FEED_PAGE_SIZE = 20;
export const THREAD_PAGE_SIZE = 50;
export const CHANNEL_PAGE_SIZE = 20;

export const fetchFeed = (level: Level, cursor?: string) =>
	callApiEither((api) =>
		api.posts.feed({ urlParams: { level, limit: FEED_PAGE_SIZE, ...(cursor ? { cursor } : {}) } }),
	).then(settle);

export const fetchThread = (postId: string, cursor?: string) =>
	callApiEither((api) =>
		api.posts.replies({
			path: { postId },
			urlParams: { limit: THREAD_PAGE_SIZE, ...(cursor ? { cursor } : {}) },
		}),
	).then(settle);

export const fetchChannelPosts = (channelId: string, cursor?: string) =>
	callApiEither((api) =>
		api.posts.channelPosts({
			path: { channelId },
			urlParams: { limit: CHANNEL_PAGE_SIZE, ...(cursor ? { cursor } : {}) },
		}),
	).then(settle);

export const fetchChannels = () => callApiEither((api) => api.posts.channels()).then(settle);

export const fetchRoles = () => callApiEither((api) => api.moderation.roles()).then(settle);

export const fetchQueue = (urlParams: QueueParams) =>
	callApiEither((api) => api.moderation.queue({ urlParams })).then(settle);
