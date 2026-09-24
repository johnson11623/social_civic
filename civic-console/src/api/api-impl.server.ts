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
	ActionRecorded,
	ApiContract,
	AppealFiled,
	BoundaryTree,
	Channel,
	ChannelList,
	ChannelPostsPage,
	FeedPage,
	Group,
	History,
	LikeState,
	Post,
	Queue,
	ReportFiled,
	RoleList,
	SearchResponse,
	type Session,
	ThreadPage,
	Unauthorized,
	UpstreamError,
} from "@/api/api-contract";
import { isLang, LANG_COOKIE } from "@/lib/i18n/lang";
import { Backend } from "@/services/backend.server";
import {
	ACCESS_COOKIE,
	clearSessionCookies,
	clientIp,
	currentSession,
	GoTokens,
	REFRESH_COOKIE,
	refreshTokens,
	sessionFromAccessToken,
	setSessionCookies,
} from "@/services/session.server";

const nowSeconds = () => Math.floor(Date.now() / 1000);

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

/**
 * The signed-in caller's access token, from the httpOnly session cookie.
 * An expired token is refreshed by the browser's SessionKeeper; the Go API's
 * 401 comes back as Unauthorized meanwhile.
 */
const bearer = Effect.flatMap(HttpServerRequest.HttpServerRequest, (req) => {
	const token = req.cookies[ACCESS_COOKIE];
	return token ? Effect.succeed(token) : Effect.fail(new Unauthorized({ code: "no_session", detail: "" }));
});

/** Caller context plus the bearer token, for authenticated platform calls. */
const authedContext = Effect.all({ acceptLanguage, forwardedFor: clientIp, bearer });

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
				return sessionFromAccessToken(tokens.access_token, nowSeconds());
			}).pipe(
				Effect.catchTags(
					narrowTo("InvalidInput", "Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("session", () =>
			Effect.gen(function* () {
				const session = yield* currentSession;
				if (session.authenticated) return session;
				const req = yield* HttpServerRequest.HttpServerRequest;
				const refreshToken = req.cookies[REFRESH_COOKIE];
				// civic_rt is only sent to /api/auth/*, so this refreshes for browser
				// calls; SSR of pages sees an anonymous session and the browser's
				// SessionKeeper takes over.
				if (!refreshToken) return session;
				return yield* refreshTokens(refreshToken, yield* callerContext).pipe(
					Effect.flatMap((tokens) =>
						Effect.as(setSessionCookies(tokens), {
							...sessionFromAccessToken(tokens.access_token, nowSeconds()),
							refreshed: true,
						}),
					),
					Effect.catchAll((e) =>
						// Rejected (expired, revoked, reused): end the session. Transient
						// failures keep the cookies so the next attempt can succeed.
						e._tag === "Unauthorized"
							? Effect.as(clearSessionCookies, { authenticated: false } satisfies Session)
							: Effect.succeed({ authenticated: false } satisfies Session),
					),
				);
			}),
		)
		.handle("refresh", () =>
			Effect.gen(function* () {
				const req = yield* HttpServerRequest.HttpServerRequest;
				const refreshToken = req.cookies[REFRESH_COOKIE];
				if (!refreshToken) {
					return yield* new Unauthorized({ code: "no_session", detail: "" });
				}
				const tokens = yield* refreshTokens(refreshToken, yield* callerContext).pipe(
					Effect.tapErrorTag("Unauthorized", () => clearSessionCookies),
				);
				yield* setSessionCookies(tokens);
				return { ...sessionFromAccessToken(tokens.access_token, nowSeconds()), refreshed: true };
			}).pipe(
				Effect.catchTags(narrowTo("Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		)
		.handle("logout", () =>
			Effect.gen(function* () {
				yield* clearSessionCookies;
				return { authenticated: false } satisfies Session;
			}),
		),
);

const PostsLive = HttpApiBuilder.group(ApiContract, "posts", (handlers) =>
	handlers
		.handle("feed", ({ urlParams }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get("/v1/feed", FeedPage, { urlParams, ...(yield* authedContext) });
			}).pipe(
				Effect.catchTags(
					narrowTo("Unauthorized", "ValidationFailed", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("channels", () =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get("/v1/channels", ChannelList, yield* authedContext);
			}).pipe(
				Effect.catchTags(narrowTo("Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		)
		.handle("createChannel", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post("/v1/channels", Channel, {
					...(yield* authedContext),
					body: {
						name: payload.name,
						description: payload.description ?? "",
						category: payload.category,
						read_only: payload.readOnly,
					},
				});
			}).pipe(
				Effect.catchTags(
					narrowTo(
						"Unauthorized",
						"Conflict",
						"ValidationFailed",
						"RateLimited",
						"BackendUnavailable",
						"UpstreamError",
					),
				),
			),
		)
		.handle("channel", ({ path }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get(
					`/v1/channels/${encodeURIComponent(path.channelId)}`,
					Channel,
					yield* authedContext,
				);
			}).pipe(
				Effect.catchTags(narrowTo("Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		)
		.handle("channelPosts", ({ path, urlParams }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get(
					`/v1/channels/${encodeURIComponent(path.channelId)}/posts`,
					ChannelPostsPage,
					{
						urlParams,
						...(yield* authedContext),
					},
				);
			}).pipe(
				Effect.catchTags(
					narrowTo("Unauthorized", "ValidationFailed", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("createPost", ({ path, payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post(`/v1/channels/${encodeURIComponent(path.channelId)}/posts`, Post, {
					...(yield* authedContext),
					body: { content: payload.content },
				});
			}).pipe(
				Effect.catchTags(
					narrowTo("Unauthorized", "ValidationFailed", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("post", ({ path }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get(`/v1/posts/${encodeURIComponent(path.postId)}`, Post, yield* authedContext);
			}).pipe(
				Effect.catchTags(narrowTo("Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		)
		.handle("replies", ({ path, urlParams }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get(`/v1/posts/${encodeURIComponent(path.postId)}/replies`, ThreadPage, {
					urlParams,
					...(yield* authedContext),
				});
			}).pipe(
				Effect.catchTags(
					narrowTo("Unauthorized", "ValidationFailed", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("reply", ({ path, payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post(`/v1/posts/${encodeURIComponent(path.postId)}/replies`, Post, {
					...(yield* authedContext),
					body: { content: payload.content },
				});
			}).pipe(
				Effect.catchTags(
					narrowTo(
						"Unauthorized",
						"Conflict",
						"ValidationFailed",
						"RateLimited",
						"BackendUnavailable",
						"UpstreamError",
					),
				),
			),
		)
		.handle("like", ({ path }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post(
					`/v1/posts/${encodeURIComponent(path.postId)}/likes`,
					LikeState,
					yield* authedContext,
				);
			}).pipe(
				Effect.catchTags(
					narrowTo("Unauthorized", "Conflict", "RateLimited", "BackendUnavailable", "UpstreamError"),
				),
			),
		)
		.handle("unlike", ({ path }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.request(
					"DELETE",
					`/v1/posts/${encodeURIComponent(path.postId)}/likes`,
					LikeState,
					yield* authedContext,
				);
			}).pipe(
				Effect.catchTags(narrowTo("Unauthorized", "RateLimited", "BackendUnavailable", "UpstreamError")),
			),
		),
);

const ModerationLive = HttpApiBuilder.group(ApiContract, "moderation", (handlers) =>
	handlers
		.handle("roles", () =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get("/v1/users/me/roles", RoleList, yield* authedContext);
			}).pipe(Effect.catchTags(narrowTo("Unauthorized", "BackendUnavailable", "UpstreamError"))),
		)
		.handle("report", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post("/v1/reports", ReportFiled, {
					...(yield* authedContext),
					body: { post_id: payload.postId, reason_code: payload.reasonCode, details: payload.details ?? "" },
				});
			}).pipe(
				Effect.catchTags(
					narrowTo(
						"Unauthorized",
						"Conflict",
						"ValidationFailed",
						"RateLimited",
						"BackendUnavailable",
						"UpstreamError",
					),
				),
			),
		)
		.handle("queue", ({ urlParams }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get("/v1/moderation/queue", Queue, { urlParams, ...(yield* authedContext) });
			}).pipe(
				Effect.catchTags(narrowTo("Unauthorized", "ValidationFailed", "BackendUnavailable", "UpstreamError")),
			),
		)
		.handle("act", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post("/v1/moderation/actions", ActionRecorded, {
					...(yield* authedContext),
					body: {
						post_id: payload.postId,
						action: payload.action,
						reason_code: payload.reasonCode,
						notes: payload.notes ?? "",
					},
				});
			}).pipe(
				Effect.catchTags(
					narrowTo(
						"Unauthorized",
						"Conflict",
						"ValidationFailed",
						"RateLimited",
						"BackendUnavailable",
						"UpstreamError",
					),
				),
			),
		)
		.handle("history", ({ path }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.get(
					`/v1/posts/${encodeURIComponent(path.postId)}/moderation`,
					History,
					yield* authedContext,
				);
			}).pipe(Effect.catchTags(narrowTo("Unauthorized", "BackendUnavailable", "UpstreamError"))),
		)
		.handle("appeal", ({ payload }) =>
			Effect.gen(function* () {
				const backend = yield* Backend;
				return yield* backend.post("/v1/appeals", AppealFiled, {
					...(yield* authedContext),
					body: { moderation_id: payload.moderationId, statement: payload.statement },
				});
			}).pipe(
				Effect.catchTags(
					narrowTo(
						"Unauthorized",
						"Conflict",
						"ValidationFailed",
						"RateLimited",
						"BackendUnavailable",
						"UpstreamError",
					),
				),
			),
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
	Layer.provide([SystemLive, BoundaryLive, AuthLive, PostsLive, ModerationLive]),
);
