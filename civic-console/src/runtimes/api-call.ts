/**
 * The concrete API caller. Imported lazily by get-runtime.ts so the Effect
 * client (≈ 80 KB gzipped) is not part of the initial page load: SSR delivers
 * the first page's data, and this module loads on the first browser-side call.
 */
import { createIsomorphicFn } from "@tanstack/react-start";
import { makeCallApiPromise } from "effect-tanstack-start/client";

import { ApiClient } from "@/services/api-client-tag";
import { clientRuntime } from "./client-runtime";
import { serverRuntime } from "./server-runtime.server";

// T-W1.1.2.5 — server runtime on the server, client runtime in the browser.
// The static .server.ts import is safe: import protection strips it from the
// client bundle.
export const getRuntime = createIsomorphicFn()
	.server(() => serverRuntime)
	.client(() => clientRuntime);

export const call = makeCallApiPromise(ApiClient, getRuntime);
