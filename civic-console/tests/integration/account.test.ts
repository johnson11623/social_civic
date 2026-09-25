import { afterEach, describe, expect, it } from "vitest";

import { bff, fakeGo, fakeJwt } from "./harness";

// Account BFF: profile, consent, erasure and two-step verification.

let dispose: (() => Promise<void>) | undefined;
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
const access = fakeJwt({
	sub: "u1",
	typ: "access",
	exp: now + 900,
	scope: { ward: 551, constituency: 111, county: 22 },
});
const req = (method: string, path: string, body?: unknown) =>
	new Request(`http://web.test${path}`, {
		method,
		headers: {
			cookie: `civic_at=${access}; civic_rt=r1`,
			origin: "http://web.test",
			...(body ? { "content-type": "application/json" } : {}),
		},
		...(body ? { body: JSON.stringify(body) } : {}),
	});

const goProfile = {
	public_id: "u1",
	display_name: "Wanjiku M.",
	preferred_lang: "sw",
	member_since: "2026-03-01T08:00:00Z",
	ward: { ward_id: 551, name: "Kiamwangi", constituency: "Gatundu South", county: "Kiambu" },
	consent: { version: "2026-01", active: true, granted_at: "2026-03-01T08:00:00Z" },
	mfa_enabled: false,
};

describe("account BFF", () => {
	it("reads and updates the profile", async () => {
		const { go, handler } = start(() => ({ status: 200, body: goProfile }));
		expect(await (await handler(req("GET", "/api/me"))).json()).toMatchObject({
			display_name: "Wanjiku M.",
			mfa_enabled: false,
		});
		await handler(req("PATCH", "/api/me", { display_name: "Wanjiku Mwangi" }));
		expect(go.seen[1]).toMatchObject({ method: "PATCH", body: { display_name: "Wanjiku Mwangi" } });
		expect(new URL(go.seen[1]?.url ?? "").pathname).toBe("/v1/users/me");
	});

	it("withdraws consent for the current policy version", async () => {
		const { go, handler } = start(() => ({
			status: 200,
			body: { withdrawn: true, version: "2026-01", effective_at: "2026-09-25T10:00:00Z" },
		}));
		expect((await handler(req("POST", "/api/me/consent/withdraw"))).status).toBe(200);
		expect(go.seen[0]?.body).toEqual({ version: "2026-01" });
	});

	it("ends this browser's session when erasure starts", async () => {
		const { handler } = start(() => ({
			status: 202,
			body: { request_id: "e1", state: "pending", ack_by: "a", completion_by: "c", retained: ["consents"] },
		}));
		const res = await handler(req("POST", "/api/me/erasure", { reason: "Moving" }));
		expect(res.status).toBe(202);
		const cookies = res.headers.getSetCookie().join("\n");
		expect(cookies).toMatch(/civic_at=;.*Max-Age=0/);
		expect(cookies).toMatch(/civic_rt=;.*Max-Age=0/);
	});

	it("steps up with a code: only the access cookie is replaced, marked mfa", async () => {
		const stepped = fakeJwt({
			sub: "u1",
			typ: "access",
			exp: now + 900,
			mfa: true,
			scope: { ward: 551, constituency: 111, county: 22 },
		});
		const { go, handler } = start(() => ({
			status: 200,
			body: { access_token: stepped, token_type: "Bearer", expires_in: 900 },
		}));
		const res = await handler(req("POST", "/api/auth/mfa", { code: "123456" }));
		expect(await res.json()).toMatchObject({ authenticated: true, mfa: true });
		const cookies = res.headers.getSetCookie();
		expect(cookies).toHaveLength(1);
		expect(cookies[0]).toContain(`civic_at=${stepped}`);
		expect(go.seen[0]?.body).toEqual({ code: "123456" });
	});

	it("rejects a malformed code before calling the platform", async () => {
		const { go, handler } = start(() => ({ status: 200, body: {} }));
		expect((await handler(req("POST", "/api/me/mfa/totp/verify", { code: "12ab" }))).status).toBe(400);
		expect(go.seen).toHaveLength(0);
	});
});
