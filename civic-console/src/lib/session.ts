import { createIsomorphicFn } from "@tanstack/react-start";

import { hasSessionHint } from "./session-hint";
import { requestHasSessionHint } from "./session-hint.server";

/** Whether a refreshable session may exist (server: request cookie; browser: document.cookie). */
export const sessionHintPresent = createIsomorphicFn()
	.server(() => requestHasSessionHint())
	.client(() => hasSessionHint(document.cookie));

/**
 * Where to go after login: only same-site paths, so `?redirect=` can't send
 * people to another site (open redirect).
 */
export function safeRedirect(target: unknown): string {
	if (
		typeof target !== "string" ||
		!target.startsWith("/") ||
		target.startsWith("//") ||
		target.startsWith("/\\\\")
	) {
		return "/";
	}
	try {
		const url = new URL(target, "http://civic.invalid");
		return url.origin === "http://civic.invalid" ? `${url.pathname}${url.search}${url.hash}` : "/";
	} catch {
		return "/";
	}
}
