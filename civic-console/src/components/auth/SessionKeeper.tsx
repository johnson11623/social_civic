import { useRouter } from "@tanstack/react-router";
import { useEffect } from "react";

import { hasSessionHint } from "@/lib/session-hint";

/** Refresh this long before the access token expires. */
export const REFRESH_LEAD_SECONDS = 60;

type SessionJson = { authenticated: boolean; expiresAt?: number; refreshed?: boolean };

/**
 * T-W1.3.2.3 — keeps the session alive without the user noticing:
 * refreshes a minute before the access token expires, and re-checks when the
 * tab becomes visible again (timers don't run while a phone sleeps). When a
 * refresh happens because the token had already expired, or the session is
 * lost, loaders re-run so the page reflects it.
 *
 * Plain fetch on purpose: it runs on every page, so it must not pull the
 * Effect client into the bundle. Anonymous visitors (no hint cookie) make no
 * requests at all.
 */
export function SessionKeeper() {
	const router = useRouter();

	useEffect(() => {
		let timer: ReturnType<typeof setTimeout> | undefined;
		let stopped = false;

		const schedule = (expiresAt: number | undefined) => {
			clearTimeout(timer);
			if (!expiresAt) return;
			const delay = Math.max(0, (expiresAt - REFRESH_LEAD_SECONDS) * 1000 - Date.now());
			timer = setTimeout(refresh, delay);
		};

		const settle = async (res: Response) => {
			if (stopped) return;
			if (res.status === 401) {
				await router.invalidate(); // session ended elsewhere or was revoked
				return;
			}
			if (!res.ok) return; // transient: the next visibility check retries
			const session = (await res.json()) as SessionJson;
			if (session.refreshed || !session.authenticated) await router.invalidate();
			if (session.authenticated) schedule(session.expiresAt);
		};

		async function refresh() {
			if (!hasSessionHint(document.cookie)) return;
			await fetch("/api/auth/refresh", { method: "POST", credentials: "same-origin" })
				.then(settle)
				.catch(() => undefined);
		}

		async function check() {
			if (!hasSessionHint(document.cookie)) return;
			// /api/auth/session refreshes by itself when the access token has expired.
			await fetch("/api/auth/session", { credentials: "same-origin" })
				.then(settle)
				.catch(() => undefined);
		}

		const onVisible = () => document.visibilityState === "visible" && void check();
		void check();
		document.addEventListener("visibilitychange", onVisible);
		return () => {
			stopped = true;
			clearTimeout(timer);
			document.removeEventListener("visibilitychange", onVisible);
		};
	}, [router]);

	return null;
}
