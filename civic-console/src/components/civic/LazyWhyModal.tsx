import { lazy, Suspense } from "react";

import type { Post } from "@/api/api-contract";

const WhyModal = lazy(() => import("./WhyModal").then((m) => ({ default: m.WhyModal })));

/** WhyModal, loaded the first time someone asks "Why am I seeing this?". */
export function LazyWhyModal({ post, onClose }: { post: Post | null; onClose: () => void }) {
	if (post === null) return null;
	return (
		<Suspense>
			<WhyModal post={post} onClose={onClose} />
		</Suspense>
	);
}
