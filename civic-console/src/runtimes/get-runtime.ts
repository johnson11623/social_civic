/**
 * Public entry for calling the typed API from loaders and components. Same
 * code on server and client. The implementation is loaded on first use so
 * Effect stays out of the initial JS bundle (budget: ≤ 150 KB gzipped).
 * Components must not import `effect` directly for the same reason: use
 * callApiEither to branch on typed errors.
 */
import type { call as Call, callEither as CallEither } from "./api-call";

export const callApiPromise: typeof Call = async (fn, options) => {
	const { call } = await import("./api-call");
	return call(fn, options);
};

export const callApiEither: typeof CallEither = async (fn) => {
	const { callEither } = await import("./api-call");
	return callEither(fn);
};
