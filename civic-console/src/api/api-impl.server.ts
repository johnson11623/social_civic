/**
 * BFF handlers for the ApiContract: each calls the Go platform API through
 * the Backend service. Server-only.
 *
 * Stateful services (Backend) are NOT provided here; they come from the
 * server runtime so the SSR client and the HTTP handler share one instance.
 */
import { HttpApiBuilder, HttpServerRequest } from "@effect/platform";
import { Effect, Layer, Schema } from "effect";

import {
	ApiContract,
	BoundaryTree,
	SearchResponse,
	UpstreamError,
	type ValidationFailed,
} from "@/api/api-contract";
import { isLang, LANG_COOKIE } from "@/lib/i18n/lang";
import { Backend } from "@/services/backend.server";

/**
 * Language to request from the Go API: the user's explicit choice (`lang`
 * cookie) wins over the browser's Accept-Language (T-W1.1.3.4). Works for
 * browser requests and SSR-forwarded headers alike.
 */
const acceptLanguage = Effect.map(HttpServerRequest.HttpServerRequest, (req) => {
	const chosen = req.cookies[LANG_COOKIE];
	return isLang(chosen) ? chosen : req.headers["accept-language"];
});

/** For endpoints that take no input, a 422 from the platform is an upstream fault. */
const asUpstream = (e: ValidationFailed) =>
	Effect.fail(new UpstreamError({ status: 422, code: e.code, detail: e.detail }));

const SystemLive = HttpApiBuilder.group(ApiContract, "system", (handlers) =>
	handlers.handle("health", () =>
		Effect.gen(function* () {
			const backend = yield* Backend;
			const up = yield* backend.get("/v1/health", Schema.Struct({ status: Schema.Literal("ok") })).pipe(
				Effect.as(true),
				Effect.orElseSucceed(() => false),
			);
			return { status: "ok", backend: up ? "ok" : "unavailable" } as const;
		}),
	),
);

const BoundaryLive = HttpApiBuilder.group(ApiContract, "boundary", (handlers) =>
	handlers
		.handle("tree", () =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend
					.get("/v1/boundary/tree", BoundaryTree, { acceptLanguage: yield* acceptLanguage })
					.pipe(Effect.catchTag("ValidationFailed", asUpstream));
			}),
		)
		.handle("search", ({ urlParams }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get("/v1/boundary/search", SearchResponse, {
					urlParams,
					acceptLanguage: yield* acceptLanguage,
				});
			}),
		),
);

export const ApiImplLive = HttpApiBuilder.api(ApiContract).pipe(Layer.provide([SystemLive, BoundaryLive]));
