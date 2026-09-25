import { lazy, type ReactNode, Suspense, useCallback, useState } from "react";

import type { Post } from "@/api/api-contract";

const ReportModal = lazy(() =>
	import("@/components/civic/ReportModal").then((m) => ({ default: m.ReportModal })),
);

/**
 * The report dialog for a list of posts: a stable `onReport` for PostCard
 * (memo-friendly) and the dialog to render, loaded on first use.
 */
export function useReportDialog(): [(post: Post) => void, ReactNode] {
	const [post, setPost] = useState<Post | null>(null);
	const onReport = useCallback((p: Post) => setPost(p), []);
	const dialog = post ? (
		<Suspense>
			<ReportModal post={post} onClose={() => setPost(null)} />
		</Suspense>
	) : null;
	return [onReport, dialog];
}
