import { afterEach, describe, expect, it } from "vitest";

import { sameOrigin, withCsrfProtection } from "@/runtimes/csrf";
import { sessionFromAccessToken } from "@/services/session.server";
import { bff, fakeGo, fakeJwt, type Reply } from "./harness";

let dispose: (() => Promise<void>) | undefined;
afterEach(async () => {
	await dispose?.();
	dispose = undefined;
});

function start(respond: Parameters<typeof fakeGo>[0], config?: Record<string, string>) {
	const go = fakeGo(respond);
	const web = bff(go, config);
	dispose = web.dispose;
	return { go, handler: web.handler };
}

const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
	new Request(`http://web.test${path}`, {
		method: "POST",
		headers: { "content-type": "application/json", origin: "http://web.test", ...headers },
		body: JSON.stringify(body),
	});

const problem = (status: number, code: string, detail = "", headers?: Record<string, string>): Reply => ({
	status,
	body: { type: "…", title: "…", status, code, detail, lang: "sw" },
	...(headers ? { headers } : {}),
});

const now = Math.floor(Date.now() / 1000);
const access = fakeJwt({
	sub: "018f-user",
	typ: "access",
	exp: now + 900,
	scope: { ward: 551, constituency: 111, county: 22 },
});
const tokens = {
	access_token: access,
	refresh_token: "refresh.jwt.x",
	token_type: "Bearer",
	expires_in: 900,
};

const registration = {
	nationalId: "12345678",
	displayName: "Wanjiku M.",
	preferredLang: "sw",
	phone: "0712 345 678",
	wardId: 551,
	consentVersion: "2026-01",
	consentGranted: true,
};

describe("POST /api/auth/register", () => {
	it("sends snake_case to Go with language and client IP, returns groups, and starts no session", async () => {
		const { go, handler } = start(() => ({
			status: 201,
			body: {
				user_id: "u",
				display_name: "Wanjiku M.",
				groups: [{ level: 1, id: 551, name: "Kiamwangi" }],
				access_token: "a",
				refresh_token: "r",
				expires_in: 900,
			},
		}));
		const res = await handler(
			post("/api/auth/register", registration, { cookie: "lang=sw", "x-forwarded-for": "196.201.214.10" }),
		);
		expect(res.status).toBe(201);
		expect(await res.json()).toEqual({
			displayName: "Wanjiku M.",
			groups: [{ level: 1, id: 551, name: "Kiamwangi" }],
		});
		expect(res.headers.get("set-cookie")).toBeNull(); // phone not yet verified

		const call = go.seen[0];
		expect(call?.method).toBe("POST");
		expect(call?.url).toBe("http://go.test/v1/auth/register");
		expect(call?.body).toEqual({
			national_id: "12345678",
			display_name: "Wanjiku M.",
			preferred_lang: "sw",
			phone: "0712 345 678",
			ward_id: 551,
			consent_version: "2026-01",
			consent_granted: true,
		});
		expect(call?.headers["accept-language"]).toBe("sw");
		expect(call?.headers["x-forwarded-for"]).toBe("196.201.214.10");
	});

	it("never returns the platform's tokens to the browser", async () => {
		const { handler } = start(() => ({
			status: 201,
			body: { display_name: "W", groups: [], access_token: "SECRET-A", refresh_token: "SECRET-R" },
		}));
		const text = await (await handler(post("/api/auth/register", registration))).text();
		expect(text).not.toContain("SECRET");
	});

	it("rejects consent not granted before calling Go", async () => {
		const { go, handler } = start(() => ({ status: 201, body: {} }));
		const res = await handler(post("/api/auth/register", { ...registration, consentGranted: false }));
		expect(res.status).toBe(400);
		expect(go.seen).toHaveLength(0);
	});

	it.each([
		[
			problem(409, "id_already_registered", "Nambari hii tayari imesajiliwa."),
			409,
			{ _tag: "Conflict", code: "id_already_registered" },
		],
		[problem(400, "invalid_id"), 400, { _tag: "InvalidInput", code: "invalid_id" }],
		[
			{
				status: 422,
				body: {
					status: 422,
					code: "validation_failed",
					detail: "…",
					errors: [{ field: "phone", code: "invalid_kenyan_mobile" }],
				},
			},
			422,
			{ _tag: "ValidationFailed", errors: [{ field: "phone", code: "invalid_kenyan_mobile" }] },
		],
		[
			problem(429, "rate_limited", "", { "retry-after": "3540" }),
			429,
			{ _tag: "RateLimited", retryAfter: 3540 },
		],
		[problem(503, "kms_unavailable"), 503, { _tag: "BackendUnavailable" }],
	] satisfies Array<[Reply, number, Record<string, unknown>]>)(
		"maps Go %# to a typed error",
		async (reply, status, body) => {
			const { handler } = start(() => reply);
			const res = await handler(post("/api/auth/register", registration));
			expect(res.status).toBe(status);
			expect(await res.json()).toMatchObject(body);
		},
	);

	it("reports an undeclared platform error as UpstreamError", async () => {
		const { handler } = start(() => problem(401, "strange"));
		const res = await handler(post("/api/auth/register", registration));
		expect(res.status).toBe(502);
		expect(await res.json()).toMatchObject({ _tag: "UpstreamError", code: "strange" });
	});
});

describe("POST /api/auth/otp and /api/auth/login", () => {
	it("requests a code", async () => {
		const { go, handler } = start(() => ({ status: 202, body: { otp_requested: true, expires_in: 300 } }));
		const res = await handler(post("/api/auth/otp", { nationalId: "12345678" }));
		expect(res.status).toBe(202);
		expect(await res.json()).toEqual({ expiresIn: 300 });
		expect(go.seen[0]?.body).toEqual({ national_id: "12345678" });
	});

	it("logs in: sets httpOnly session cookies and returns the session, not the tokens", async () => {
		const { go, handler } = start(() => ({ status: 200, body: tokens }));
		const res = await handler(post("/api/auth/login", { nationalId: "12345678", otp: "123456" }));
		expect(res.status).toBe(200);
		const body = await res.json();
		expect(body).toMatchObject({
			authenticated: true,
			subject: "018f-user",
			scope: { ward: 551, constituency: 111, county: 22 },
		});
		expect(JSON.stringify(body)).not.toContain(access);

		const cookies = res.headers.getSetCookie();
		const at = cookies.find((c) => c.startsWith("civic_at="));
		const rt = cookies.find((c) => c.startsWith("civic_rt="));
		expect(at).toMatch(/HttpOnly/i);
		expect(at).toMatch(/SameSite=Lax/i);
		expect(at).toMatch(/Path=\//);
		expect(at).toMatch(/Max-Age=900/);
		expect(rt).toMatch(/HttpOnly/i);
		expect(rt).toMatch(/SameSite=Strict/i);
		expect(rt).toMatch(/Path=\/api\/auth/);
		expect(go.seen[0]?.body).toEqual({ national_id: "12345678", otp: "123456" });
	});

	it("marks cookies Secure when COOKIE_SECURE is on", async () => {
		const { handler } = start(() => ({ status: 200, body: tokens }), { COOKIE_SECURE: "true" });
		const res = await handler(post("/api/auth/login", { nationalId: "12345678", otp: "123456" }));
		for (const c of res.headers.getSetCookie()) expect(c).toMatch(/Secure/i);
	});

	it("maps a wrong code to Unauthorized and sets no cookies", async () => {
		const { handler } = start(() => problem(401, "invalid_credentials", "Nambari si sahihi."));
		const res = await handler(post("/api/auth/login", { nationalId: "12345678", otp: "000000" }));
		expect(res.status).toBe(401);
		expect(await res.json()).toMatchObject({ _tag: "Unauthorized", code: "invalid_credentials" });
		expect(res.headers.getSetCookie()).toEqual([]);
	});

	it("validates the code format before calling Go", async () => {
		const { go, handler } = start(() => ({ status: 200, body: tokens }));
		expect((await handler(post("/api/auth/login", { nationalId: "12345678", otp: "12ab" }))).status).toBe(
			400,
		);
		expect(go.seen).toHaveLength(0);
	});
});

describe("GET /api/auth/session and POST /api/auth/logout", () => {
	it("reads the session from the cookie", async () => {
		const { handler } = start(() => ({ status: 500, body: {} }));
		const signedIn = await handler(
			new Request("http://web.test/api/auth/session", { headers: { cookie: `civic_at=${access}` } }),
		);
		expect(await signedIn.json()).toMatchObject({ authenticated: true, subject: "018f-user" });
		const anonymous = await handler(new Request("http://web.test/api/auth/session"));
		expect(await anonymous.json()).toEqual({ authenticated: false });
	});

	it("logout expires both cookies", async () => {
		const { handler } = start(() => ({ status: 500, body: {} }));
		const res = await handler(post("/api/auth/logout", {}, { cookie: `civic_at=${access}` }));
		expect(await res.json()).toEqual({ authenticated: false });
		const cookies = res.headers.getSetCookie();
		expect(cookies.find((c) => c.startsWith("civic_at="))).toMatch(/Max-Age=0|Expires=Thu, 01 Jan 1970/);
		expect(cookies.find((c) => c.startsWith("civic_rt="))).toMatch(/Path=\/api\/auth/);
	});
});

describe("sessionFromAccessToken", () => {
	it("is anonymous for missing, malformed, expired or non-access tokens", () => {
		expect(sessionFromAccessToken(undefined, now)).toEqual({ authenticated: false });
		expect(sessionFromAccessToken("garbage", now)).toEqual({ authenticated: false });
		expect(sessionFromAccessToken("a.!!!.c", now)).toEqual({ authenticated: false });
		expect(sessionFromAccessToken(access, now + 901)).toEqual({ authenticated: false });
		const refresh = fakeJwt({
			sub: "x",
			typ: "refresh",
			exp: now + 100,
			scope: { ward: 1, constituency: 1, county: 1 },
		});
		expect(sessionFromAccessToken(refresh, now)).toEqual({ authenticated: false });
	});
});

describe("CSRF protection (T-X.2)", () => {
	const req = (method: string, headers: Record<string, string>) =>
		new Request("http://web.test/api/auth/login", { method, headers: { host: "web.test", ...headers } });

	it("allows same-origin and safe requests, refuses cross-site writes", async () => {
		expect(sameOrigin(req("GET", { origin: "https://evil.test" }))).toBe(true);
		expect(sameOrigin(req("POST", { origin: "http://web.test" }))).toBe(true);
		expect(sameOrigin(req("POST", { "sec-fetch-site": "same-origin" }))).toBe(true);
		expect(sameOrigin(req("POST", { origin: "https://evil.test" }))).toBe(false);
		expect(sameOrigin(req("POST", { "sec-fetch-site": "cross-site" }))).toBe(false);
		expect(sameOrigin(req("POST", {}))).toBe(false);
		expect(sameOrigin(req("POST", { origin: "null" }))).toBe(false);

		const guarded = withCsrfProtection(async () => new Response("ok"));
		const refused = await guarded({ request: req("POST", { origin: "https://evil.test" }) });
		expect(refused.status).toBe(403);
		expect((await guarded({ request: req("POST", { origin: "http://web.test" }) })).status).toBe(200);
	});
});
