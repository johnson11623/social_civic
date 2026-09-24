import { afterEach, describe, expect, it } from "vitest";

import { bff, fakeGo, fakeJwt } from "./harness";

// W2.2 — moderation through the BFF to a fake Go API.

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

const access = fakeJwt({ sub: "u1", typ: "access", exp: Math.floor(Date.now() / 1000) + 900 });
const req = (method: string, path: string, body?: unknown) =>
	new Request(`http://web.test${path}`, {
		method,
		headers: {
			cookie: `civic_at=${access}`,
			origin: "http://web.test",
			...(body ? { "content-type": "application/json" } : {}),
		},
		...(body ? { body: JSON.stringify(body) } : {}),
	});
const path = (u: string) => {
	const url = new URL(u);
	return `${url.pathname}${url.search}`;
};

describe("moderation BFF", () => {
	it("lists the caller's roles", async () => {
		const { go, handler } = start(() => ({
			status: 200,
			body: {
				items: [{ assignment_id: "a1", role: "ward_mod", level: 1, unit_code: 551, appointed_at: "x" }],
			},
		}));
		expect(await (await handler(req("GET", "/api/me/roles"))).json()).toEqual({
			items: [{ assignment_id: "a1", role: "ward_mod", level: 1, unit_code: 551 }],
		});
		expect(path(go.seen[0]?.url ?? "")).toBe("/v1/users/me/roles");
	});

	it("files a report with snake_case fields and passes a duplicate through as Conflict", async () => {
		let n = 0;
		const { go, handler } = start(() =>
			n++ === 0
				? { status: 201, body: { report_id: "r1", state: "open", queue: "ward_mod" } }
				: { status: 409, body: { status: 409, code: "already_reported", detail: "Already reported." } },
		);
		const ok = await handler(
			req("POST", "/api/reports", { post_id: "p1", reason_code: "privacy", details: "My number" }),
		);
		expect(ok.status).toBe(201);
		expect(go.seen[0]?.body).toEqual({ post_id: "p1", reason_code: "privacy", details: "My number" });
		const dup = await handler(req("POST", "/api/reports", { post_id: "p1", reason_code: "privacy" }));
		expect(await dup.json()).toMatchObject({
			_tag: "Conflict",
			code: "already_reported",
			detail: "Already reported.",
		});
	});

	it("rejects reasons outside the policy before calling the platform", async () => {
		const { go, handler } = start(() => ({ status: 201, body: {} }));
		expect((await handler(req("POST", "/api/reports", { post_id: "p1", reason_code: "spam" }))).status).toBe(
			400,
		);
		expect(go.seen).toHaveLength(0);
	});

	it("reads the queue, records an action, reads history, files an appeal", async () => {
		const { go, handler } = start((url, seen) => {
			if (url.pathname === "/v1/moderation/queue")
				return {
					status: 200,
					body: {
						items: [
							{
								post_id: "p1",
								level: 1,
								ward_id: 551,
								content: "x",
								state: "active",
								author: { public_id: "a", display_name: "A" },
								report_count: 2,
								reasons: ["privacy"],
								first_reported_at: "2026-09-24T08:00:00Z",
								posted_at: "2026-09-24T07:00:00Z",
							},
						],
					},
				};
			if (url.pathname === "/v1/moderation/actions")
				return {
					status: 201,
					body: { action_id: "m1", post_state: "tombstoned", appeal_window_hours: 336, appeal_due_at: "d" },
				};
			if (url.pathname.endsWith("/moderation"))
				return {
					status: 200,
					body: {
						post_id: "p1",
						items: [
							{
								action_id: "m1",
								action: "hide",
								reason_code: "privacy",
								level: 1,
								post_state: "tombstoned",
								overturned: false,
								created_at: "c",
								notes: "n",
							},
						],
					},
				};
			void seen;
			return { status: 201, body: { appeal_id: "ap1", state: "open", due_at: "2026-10-08T00:00:00Z" } };
		});
		const queue = await (
			await handler(req("GET", "/api/moderation/queue?level=ward&filter=open&sort=reports"))
		).json();
		expect(queue.items[0]).toMatchObject({ post_id: "p1", level: 1, report_count: 2 });
		await handler(
			req("POST", "/api/moderation/actions", { post_id: "p1", action: "hide", reason_code: "privacy" }),
		);
		const history = await (await handler(req("GET", "/api/posts/p1/moderation"))).json();
		expect(history.items[0]).toMatchObject({ action: "hide", notes: "n" });
		const appeal = await handler(
			req("POST", "/api/appeals", { moderation_id: "m1", statement: "Please review" }),
		);
		expect(appeal.status).toBe(201);

		expect(go.seen.map((s) => `${s.method} ${path(s.url)}`)).toEqual([
			"GET /v1/moderation/queue?level=ward&filter=open&sort=reports",
			"POST /v1/moderation/actions",
			"GET /v1/posts/p1/moderation",
			"POST /v1/appeals",
		]);
		expect(go.seen[1]?.body).toEqual({ post_id: "p1", action: "hide", reason_code: "privacy", notes: "" });
		expect(go.seen[3]?.body).toEqual({ moderation_id: "m1", statement: "Please review" });
	});
});
