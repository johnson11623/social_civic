import type { MessageKey, Vars } from "@/lib/i18n/messages";

type T = (key: MessageKey, vars?: Vars) => string;

/** Tagged error as it arrives from the typed API client. */
export type ApiFailure = {
	readonly _tag: string;
	readonly code?: string;
	readonly detail?: string;
	readonly retryAfter?: number;
	/** ValidationFailed: which fields failed and why. */
	readonly errors?: ReadonlyArray<{ readonly field: string; readonly code: string }>;
};

/**
 * A localized, user-facing message for any API failure. The platform's own
 * `detail` is already localized (Accept-Language), so it is preferred;
 * transport and unexpected failures get our own wording.
 */
export function describeError(error: unknown, t: T): string {
	const e = (error ?? {}) as Partial<ApiFailure>;
	switch (e._tag) {
		case "RateLimited":
			return t("error.rateLimited", { minutes: Math.max(1, Math.ceil((e.retryAfter ?? 60) / 60)) });
		case "BackendUnavailable":
			return t("error.unavailable");
		case "InvalidInput":
		case "Unauthorized":
		case "Conflict":
		case "ValidationFailed":
			return e.detail || t("error.generic");
		case "RequestError":
			return t("error.offline");
		default:
			return t("error.generic");
	}
}

/** Field-level codes from a ValidationFailed error, keyed by field. */
export function fieldErrors(error: unknown): Record<string, string> {
	const e = error as { _tag?: string; errors?: ReadonlyArray<{ field: string; code: string }> };
	if (e?._tag !== "ValidationFailed" || !e.errors) return {};
	return Object.fromEntries(e.errors.map((f) => [f.field, f.code]));
}

/**
 * A settled API call as plain data, so loader results survive SSR
 * serialization (Either is a class instance) and components need no Effect.
 */
export type Settled<A> =
	| { readonly ok: true; readonly value: A }
	| { readonly ok: false; readonly error: ApiFailure };

export function settle<A>(result: { _tag: "Right"; right: A } | { _tag: "Left"; left: unknown }): Settled<A> {
	if (result._tag === "Right") return { ok: true, value: result.right };
	const e = (result.left ?? {}) as Partial<ApiFailure>;
	return {
		ok: false,
		error: {
			_tag: e._tag ?? "Unknown",
			...(e.detail !== undefined ? { detail: e.detail } : {}),
			...(e.retryAfter !== undefined ? { retryAfter: e.retryAfter } : {}),
			...(e.code !== undefined ? { code: e.code } : {}),
			...(e.errors !== undefined ? { errors: e.errors.map(({ field, code }) => ({ field, code })) } : {}),
		},
	};
}
