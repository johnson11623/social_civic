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
import { Backend } from "@/services/backend.server";

/** The caller's Accept-Language (browser request or SSR-forwarded headers). */
const acceptLanguage = Effect.map(
	HttpServerRequest.HttpServerRequest,
	(req) => req.headers["accept-language"],
);

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
