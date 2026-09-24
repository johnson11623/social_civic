/**
 * The concrete API caller. Imported lazily by get-runtime.ts so the Effect
 * client (≈ 80 KB gzipped) is not part of the initial page load: SSR delivers
 * the first page's data, and this module loads on the first browser-side call.
 */
import { createIsomorphicFn } from "@tanstack/react-start";
import { Effect, type Either } from "effect";
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

type Api = Parameters<Parameters<typeof call>[0]>[0];

/**
 * Like `call`, but typed failures come back as `Left` instead of throwing, so
 * components can branch on `_tag` without importing Effect themselves.
 */
export const callEither = <A, E>(
	fn: (api: Api) => Effect.Effect<A, E, never>,
): Promise<Either.Either<A, E>> => call((api) => Effect.either(fn(api)));
