/**
 * Public entry for calling the typed API from loaders and components. Same
 * code on server and client. The implementation is loaded on first use so
 * Effect stays out of the initial JS bundle (budget: ≤ 150 KB gzipped).
 */
import type { call as Call } from "./api-call";

export const callApiPromise: typeof Call = async (fn, options) => {
	const { call } = await import("./api-call");
	return call(fn, options);
};
