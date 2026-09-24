import { useCallback, useRef } from "react";

import type { Post } from "@/api/api-contract";
import { useToast } from "@/components/ui/Toast";
import { settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { callApiEither } from "@/runtimes/get-runtime";

type Update = (postId: string, change: (p: Post) => Post) => void;

/**
 * T-W1.4.1.6 — optimistic like/unlike: the heart flips at once, the
 * server's count replaces the guess, and a failure puts the post back as it
 * was. One request per post at a time. `update` applies a change to the
 * caller's copy of the post (feed list, thread, …).
 */
export function useLikeToggle(update: Update): (post: Post) => Promise<void> {
	const { t } = useT();
	const toast = useToast();
	const inflight = useRef(new Set<string>());

	return useCallback(
		async (post: Post) => {
			if (inflight.current.has(post.postId)) return;
			inflight.current.add(post.postId);
			const was = { liked: post.liked ?? false, likes: post.counts.likes };
			const liked = !was.liked;
			update(post.postId, (p) => ({
				...p,
				liked,
				counts: { ...p.counts, likes: Math.max(0, p.counts.likes + (liked ? 1 : -1)) },
			}));
			const res = settle(
				await callApiEither((api) =>
					liked
						? api.posts.like({ path: { postId: post.postId } })
						: api.posts.unlike({ path: { postId: post.postId } }),
				),
			);
			inflight.current.delete(post.postId);
			if (res.ok) {
				update(post.postId, (p) => ({
					...p,
					liked: res.value.liked,
					counts: { ...p.counts, likes: res.value.likes },
				}));
			} else if (liked && res.error.code === "already_liked") {
				// Liked from another device: the server agrees with the new state.
			} else {
				update(post.postId, (p) => ({ ...p, liked: was.liked, counts: { ...p.counts, likes: was.likes } }));
				toast(t("error.likeFailed"), "error");
			}
		},
		[t, toast, update],
	);
}
