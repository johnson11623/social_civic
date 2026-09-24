import { useEffect, useState } from "react";

/** The desktop breakpoint at which the right-hand column shows (Tailwind xl). */
export const WIDE_QUERY = "(min-width: 1280px)";

/**
 * True once the screen is wide enough *and* the browser is idle: extras
 * like the right-hand column load only then, so they never compete with the
 * first paint, and phones never download them. False during SSR.
 */
export function useDeferredWide(query = WIDE_QUERY): boolean {
	const [ready, setReady] = useState(false);
	useEffect(() => {
		if (typeof window.matchMedia !== "function") return;
		const media = window.matchMedia(query);
		let idle: number | undefined;
		const schedule = () => {
			if (!media.matches || idle !== undefined) return;
			const done = () => setReady(true);
			idle =
				typeof window.requestIdleCallback === "function"
					? window.requestIdleCallback(done, { timeout: 2000 })
					: window.setTimeout(done, 200);
		};
		schedule();
		media.addEventListener("change", schedule);
		return () => {
			media.removeEventListener("change", schedule);
			if (idle !== undefined) {
				if (typeof window.cancelIdleCallback === "function") window.cancelIdleCallback(idle);
				else window.clearTimeout(idle);
			}
		};
	}, [query]);
	return ready;
}
