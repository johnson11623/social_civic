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
	Group,
	SearchResponse,
	type Session,
	UpstreamError,
} from "@/api/api-contract";
import { isLang, LANG_COOKIE } from "@/lib/i18n/lang";
import { Backend } from "@/services/backend.server";
import {
	clearSessionCookies,
	clientIp,
	currentSession,
	GoTokens,
	sessionFromAccessToken,
	setSessionCookies,
} from "@/services/session.server";

/**
 * Language to request from the Go API: the user's explicit choice (`lang`
 * cookie) wins over the browser's Accept-Language (T-W1.1.3.4). Works for
 * browser requests and SSR-forwarded headers alike.
 */
const acceptLanguage = Effect.map(HttpServerRequest.HttpServerRequest, (req) => {
	const chosen = req.cookies[LANG_COOKIE];
	return isLang(chosen) ? chosen : req.headers["accept-language"];
});

/** Options every proxied call carries: language and the browser's IP. */
const callerContext = Effect.all({ acceptLanguage, forwardedFor: clientIp });

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
				return yield* backend.get("/v1/boundary/tree", BoundaryTree, yield* callerContext);
			}).pipe(Effect.catchTags(narrowTo("RateLimited", "BackendUnavailable", "UpstreamError"))),
		)
		.handle("search", ({ urlParams }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get("/v1/boundary/search", SearchResponse, {
					urlParams,
					...(yield* callerContext),
				});
			}).pipe(
				Effect.catchTags(narrowTo("ValidationFailed", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		),
);

const GoRegistered = Schema.Struct({ display_name: Schema.String, groups: Schema.Array(Group) });
const GoOtp = Schema.Struct({ otp_requested: Schema.Boolean, expires_in: Schema.Int });

const AuthLive = HttpApiBuilder.group(ApiContract, "auth", (handlers) =>
	handlers
		// Registration does not start a session: the phone is verified first
		// with an SMS code (requestOtp + login), which then signs the user in.
		.handle("register", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				const res = yield* backend.post("/v1/auth/register", GoRegistered, {
					...(yield* callerContext),
					body: {
						national_id: payload.nationalId,
						display_name: payload.displayName,
						preferred_lang: payload.preferredLang,
						phone: payload.phone,
						ward_id: payload.wardId,
						consent_version: payload.consentVersion,
						consent_granted: payload.consentGranted,
					},
				});
				return { displayName: res.display_name, groups: res.groups };
			}).pipe(
				Effect.catchTags(
					narrowTo(
						"InvalidInput",
						"Conflict",
						"ValidationFailed",
						"RateLimited",
						"BackendUnavailable",
						"UpstreamError",
					),
				),
			),
		)
		.handle("requestOtp", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				const res = yield* backend.post("/v1/auth/otp", GoOtp, {
					...(yield* callerContext),
					body: { national_id: payload.nationalId },
				});
				return { expiresIn: res.expires_in };
			}).pipe(
				Effect.catchTags(narrowTo("InvalidInput", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		)
		.handle("login", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				const tokens = yield* backend.post("/v1/auth/login", GoTokens, {
					...(yield* callerContext),
					body: { national_id: payload.nationalId, otp: payload.otp },
				});
				yield* setSessionCookies(tokens);
				return sessionFromAccessToken(tokens.access_token, Math.floor(Date.now() / 1000));
			}).pipe(
				Effect.catchTags(
					narrowTo("InvalidInput", "Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("session", () => currentSession)
		.handle("logout", () =>
			Effect.gen(function* () {
				yield* clearSessionCookies;
				return { authenticated: false } satisfies Session;
			}),
		),
);

/**
 * Keep the errors an endpoint declares; report any other platform error as
 * UpstreamError so the contract's error types stay exact.
 */
type Tag =
	| "InvalidInput"
	| "Unauthorized"
	| "Conflict"
	| "ValidationFailed"
	| "RateLimited"
	| "BackendUnavailable"
	| "UpstreamError";
function narrowTo<const K extends Tag>(...keep: K[]) {
	const all: Tag[] = [
		"InvalidInput",
		"Unauthorized",
		"Conflict",
		"ValidationFailed",
		"RateLimited",
		"BackendUnavailable",
		"UpstreamError",
	];
	return Object.fromEntries(
		all
			.filter((t) => !(keep as Tag[]).includes(t))
			.map((t) => [
				t,
				(e: { code?: string; detail?: string }) =>
					Effect.fail(
						new UpstreamError({ status: 502, code: e.code ?? "upstream_error", detail: e.detail ?? "" }),
					),
			]),
	) as {
		[T in Exclude<Tag, K>]: (e: { code?: string; detail?: string }) => Effect.Effect<never, UpstreamError>;
	};
}

export const ApiImplLive = HttpApiBuilder.api(ApiContract).pipe(
	Layer.provide([SystemLive, BoundaryLive, AuthLive]),
);
