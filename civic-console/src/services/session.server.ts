/**
 * BFF session: the platform's JWTs live in httpOnly cookies so browser
 * JavaScript can never read them (T-W1.3.2.2, F-10). Server-only.
 *
 * The BFF decodes the access token's claims only to describe the session to
 * the UI; it does not verify the signature. That is safe because the cookie
 * is httpOnly and set only by this server, and the Go API verifies the token
 * on every call that acts on it.
 */
import { HttpApp, HttpServerRequest, HttpServerResponse } from "@effect/platform";
import { Config, Effect, type Either, Option, Runtime, Schema } from "effect";

import type { Session } from "@/api/api-contract";
import { SESSION_HINT_COOKIE } from "@/lib/session-hint";
import { Backend, type BackendError, type RequestOptions } from "./backend.server";

export const ACCESS_COOKIE = "civic_at";
export const REFRESH_COOKIE = "civic_rt";
const REFRESH_PATH = "/api/auth";
const REFRESH_TTL = "30 days";

/** Token pair as returned by the Go API's login and refresh endpoints. */
export const GoTokens = Schema.Struct({
	access_token: Schema.String,
	refresh_token: Schema.String,
	expires_in: Schema.Int,
});
export type GoTokens = typeof GoTokens.Type;

const Claims = Schema.Struct({
	sub: Schema.String,
	exp: Schema.Number,
	typ: Schema.Literal("access"),
	scope: Schema.Struct({ ward: Schema.Int, constituency: Schema.Int, county: Schema.Int }),
});

const anonymous: Session = { authenticated: false };

/** Session described by an access token, or anonymous if absent/expired/malformed. */
export function sessionFromAccessToken(token: string | undefined, nowSeconds: number): Session {
	if (!token) return anonymous;
	const payload = token.split(".")[1];
	if (!payload) return anonymous;
	try {
		const json = JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
		const claims = Option.getOrUndefined(Schema.decodeUnknownOption(Claims)(json));
		if (!claims || claims.exp <= nowSeconds) return anonymous;
		return { authenticated: true, subject: claims.sub, scope: claims.scope, expiresAt: claims.exp };
	} catch {
		return anonymous;
	}
}

const secureCookies = Config.boolean("COOKIE_SECURE").pipe(
	Config.withDefault(process.env.NODE_ENV === "production"),
);

/** Set the session cookies on this handler's response. */
export const setSessionCookies = (tokens: GoTokens) =>
	Effect.gen(function* () {
		const secure = yield* Effect.orDie(secureCookies);
		yield* HttpApp.appendPreResponseHandler((_req, res) =>
			Effect.succeed(
				res.pipe(
					HttpServerResponse.unsafeSetCookie(ACCESS_COOKIE, tokens.access_token, {
						httpOnly: true,
						secure,
						sameSite: "lax",
						path: "/",
						maxAge: `${tokens.expires_in} seconds`,
					}),
					// Only sent to /api/auth/*, where refresh happens.
					HttpServerResponse.unsafeSetCookie(REFRESH_COOKIE, tokens.refresh_token, {
						httpOnly: true,
						secure,
						sameSite: "strict",
						path: REFRESH_PATH,
						maxAge: REFRESH_TTL,
					}),
					// Not a secret: tells the browser a refreshable session exists, so it
					// only calls the refresh endpoint when there is something to refresh.
					HttpServerResponse.unsafeSetCookie(SESSION_HINT_COOKIE, "1", {
						secure,
						sameSite: "lax",
						path: "/",
						maxAge: REFRESH_TTL,
					}),
				),
			),
		);
	});

/** Remove the session cookies on this handler's response. */
export const clearSessionCookies = HttpApp.appendPreResponseHandler((_req, res) =>
	Effect.succeed(
		res.pipe(
			HttpServerResponse.expireCookie(ACCESS_COOKIE, { path: "/" }),
			HttpServerResponse.expireCookie(REFRESH_COOKIE, { path: REFRESH_PATH }),
			HttpServerResponse.expireCookie(SESSION_HINT_COOKIE, { path: "/" }),
		),
	),
);

/** The current request's session. */
export const currentSession = Effect.map(HttpServerRequest.HttpServerRequest, (req) =>
	sessionFromAccessToken(req.cookies[ACCESS_COOKIE], Math.floor(Date.now() / 1000)),
);

/**
 * The browser's IP for the Go API's per-IP rate limits. The Go API must be
 * configured to trust X-Forwarded-For from this BFF (not yet: see README).
 *
 * During SSR the handlers run on a synthetic request with no socket, so
 * remoteAddress is absent (not Option.none); the forwarded browser headers
 * are all there is.
 */
export const clientIp = Effect.map(HttpServerRequest.HttpServerRequest, (req) => {
	const remote = (req as { remoteAddress?: Option.Option<string> }).remoteAddress;
	return (
		(remote ? Option.getOrUndefined(remote) : undefined) ??
		req.headers["x-forwarded-for"]?.split(",")[0]?.trim()
	);
});

// ---- Refresh (T-W1.3.2.3) ------------------------------------------------------

/**
 * The Go API rotates refresh tokens and treats a second use of a rotated
 * token as theft (every session revoked). Two tabs refreshing at once would
 * trip that, so refreshes are single-flight per token, and the result is
 * reused for requests that arrive with the same old token shortly after.
 *
 * In-memory: correct for one BFF instance. Several instances need a shared
 * store (Redis) keyed by a hash of the token.
 */
const REUSE_WINDOW_MS = 30_000;
const inflight = new Map<string, { at: number; result: Promise<Either.Either<GoTokens, BackendError>> }>();

export const refreshTokens = (refreshToken: string, context: RequestOptions = {}) =>
	Effect.gen(function* () {
		const now = Date.now();
		for (const [key, entry] of inflight) if (now - entry.at > REUSE_WINDOW_MS) inflight.delete(key);

		let entry = inflight.get(refreshToken);
		if (!entry) {
			const backend = yield* Backend;
			const runtime = yield* Effect.runtime<never>();
			const call = backend.post("/v1/auth/refresh", GoTokens, {
				...context,
				body: { refresh_token: refreshToken },
			});
			entry = { at: now, result: Runtime.runPromise(runtime)(Effect.either(call)) };
			inflight.set(refreshToken, entry);
		}
		const pending = entry.result;
		return yield* Effect.flatten(Effect.promise(() => pending));
	});

/** Test hook: forget in-flight refreshes. */
export const resetRefreshCache = () => inflight.clear();
