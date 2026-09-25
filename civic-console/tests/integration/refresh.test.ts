import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { resetRefreshCache } from "@/services/session.server";
import { bff, fakeGo, fakeJwt, type Reply } from "./harness";

// T-W1.3.2.3 — refresh through the BFF.

let dispose: (() => Promise<void>) | undefined;
beforeEach(() => resetRefreshCache());
afterEach(async () => {
	await dispose?.();
	dispose = undefined;
});

function start(respond: Parameters<typeof fakeGo>[0]) {
	const go = fakeGo(respond);
	const web = bff(go);
	dispose = web.dispose;
	return { go, handler: web.handler };
}

const now = Math.floor(Date.now() / 1000);
const access = (exp: number) =>
	fakeJwt({ sub: "018f-user", typ: "access", exp, scope: { ward: 551, constituency: 111, county: 22 } });
const rotated: Reply = {
	status: 200,
	body: { access_token: access(now + 900), refresh_token: "rt-2", token_type: "Bearer", expires_in: 900 },
};
const cookie = (c: Record<string, string>) =>
	Object.entries(c)
		.map(([k, v]) => `${k}=${v}`)
		.join("; ");
const refreshCalls = (go: ReturnType<typeof fakeGo>) =>
	go.seen.filter((s) => s.url.endsWith("/v1/auth/refresh"));

describe("GET /api/auth/session", () => {
	it("refreshes automatically when the access token has expired", async () => {
		const { go, handler } = start(() => rotated);
		const res = await handler(
			new Request("http://web.test/api/auth/session", {
				headers: { cookie: cookie({ civic_at: access(now - 10), civic_rt: "rt-1", civic_s: "1" }) },
			}),
		);
		expect(await res.json()).toMatchObject({ authenticated: true, subject: "018f-user", refreshed: true });
		expect(refreshCalls(go)[0]?.body).toEqual({ refresh_token: "rt-1" });
		const set = res.headers.getSetCookie();
		expect(set.find((c) => c.startsWith("civic_at="))).toContain(access(now + 900));
		expect(set.find((c) => c.startsWith("civic_rt="))).toContain("rt-2");
		expect(set.find((c) => c.startsWith("civic_s=1"))).toBeDefined();
	});

	it("does not refresh a valid session", async () => {
		const { go, handler } = start(() => rotated);
		const res = await handler(
			new Request("http://web.test/api/auth/session", {
				headers: { cookie: cookie({ civic_at: access(now + 600), civic_rt: "rt-1" }) },
			}),
		);
		expect((await res.json()).refreshed).toBeUndefined();
		expect(refreshCalls(go)).toHaveLength(0);
	});

	it("single-flights concurrent refreshes of the same token (no false reuse detection)", async () => {
		const { go, handler } = start(() => rotated);
		const req = () =>
			handler(
				new Request("http://web.test/api/auth/session", {
					headers: { cookie: cookie({ civic_rt: "rt-1", civic_s: "1" }) },
				}),
			);
		const results = await Promise.all([req(), req(), req(), req(), req()]);
		for (const r of results) expect((await r.json()).authenticated).toBe(true);
		expect(refreshCalls(go)).toHaveLength(1);
	});

	it("ends the session when the platform rejects the refresh token", async () => {
		const { handler } = start(() => ({
			status: 401,
			body: { status: 401, code: "token_reuse_detected", detail: "" },
		}));
		const res = await handler(
			new Request("http://web.test/api/auth/session", {
				headers: { cookie: cookie({ civic_rt: "stolen", civic_s: "1" }) },
			}),
		);
		expect(await res.json()).toEqual({ authenticated: false });
		const set = res.headers.getSetCookie();
		for (const name of ["civic_at", "civic_rt", "civic_s"]) {
			expect(set.find((c) => c.startsWith(`${name}=`))).toMatch(/Max-Age=0|Expires=Thu, 01 Jan 1970/);
		}
	});

	it("keeps the cookies when the platform is briefly unavailable", async () => {
		const { handler } = start(() => ({ status: 503, body: {} }));
		const res = await handler(
			new Request("http://web.test/api/auth/session", { headers: { cookie: cookie({ civic_rt: "rt-1" }) } }),
		);
		expect(await res.json()).toEqual({ authenticated: false });
		expect(res.headers.getSetCookie()).toEqual([]);
	});
});

describe("POST /api/auth/refresh", () => {
	const post = (c: Record<string, string>) =>
		new Request("http://web.test/api/auth/refresh", {
			method: "POST",
			headers: { origin: "http://web.test", cookie: cookie(c) },
		});

	it("rotates tokens proactively", async () => {
		const { handler } = start(() => rotated);
		const res = await handler(post({ civic_at: access(now + 30), civic_rt: "rt-1" }));
		expect(res.status).toBe(200);
		expect(await res.json()).toMatchObject({ authenticated: true, refreshed: true });
		expect(res.headers.getSetCookie().find((c) => c.startsWith("civic_rt="))).toContain("rt-2");
	});

	it("is 401 without a refresh cookie", async () => {
		const { go, handler } = start(() => rotated);
		const res = await handler(post({}));
		expect(res.status).toBe(401);
		expect(await res.json()).toMatchObject({ _tag: "Unauthorized", code: "no_session" });
		expect(go.seen).toHaveLength(0);
	});

	it("clears cookies when the refresh token is rejected", async () => {
		const { handler } = start(() => ({
			status: 401,
			body: { status: 401, code: "token_revoked", detail: "" },
		}));
		const res = await handler(post({ civic_rt: "old" }));
		expect(res.status).toBe(401);
		expect(res.headers.getSetCookie().find((c) => c.startsWith("civic_rt="))).toMatch(
			/Max-Age=0|Expires=Thu, 01 Jan 1970/,
		);
	});
});
