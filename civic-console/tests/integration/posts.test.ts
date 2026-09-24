import { afterEach, describe, expect, it } from "vitest";

import { bff, fakeGo, fakeJwt, type Reply } from "./harness";

// W1.4 — feed, channels, posts and likes through the BFF to a fake Go API.

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
	sub: "018f-user",
	typ: "access",
	exp: now + 900,
	scope: { ward: 551, constituency: 111, county: 22 },
});
const signedIn = { cookie: `civic_at=${access}; lang=en` };

const req = (method: string, path: string, body?: unknown, headers: Record<string, string> = signedIn) =>
	new Request(`http://web.test${path}`, {
		method,
		headers: {
			origin: "http://web.test",
			...(body ? { "content-type": "application/json" } : {}),
			...headers,
		},
		...(body ? { body: JSON.stringify(body) } : {}),
	});

const goPost = (over: Record<string, unknown> = {}) => ({
	post_id: "p1",
	channel_id: "c1",
	channel: "general",
	level: 2,
	ward_id: 551,
	content: "Water point broken",
	score: 143.7,
	state: "active",
	author: { public_id: "a1", display_name: "Brian" },
	counts: { likes: 42, replies: 8 },
	liked: false,
	sponsored: false,
	created_at: "2026-09-24T08:15:00Z",
	...over,
});

const problem = (status: number, code: string, detail = ""): Reply => ({
	status,
	body: { status, code, detail },
});

describe("GET /api/feed", () => {
	it("forwards the session as a bearer token with level, limit and cursor", async () => {
		const { go, handler } = start(() => ({
			status: 200,
			body: { items: [goPost()], next_cursor: "abc.def", has_more: true },
		}));
		const res = await handler(req("GET", "/api/feed?level=constituency&limit=20&cursor=abc.xyz"));
		expect(res.status).toBe(200);
		const body = await res.json();
		expect(body).toMatchObject({
			next_cursor: "abc.def",
			has_more: true,
			items: [{ post_id: "p1", level: 2 }],
		});

		const call = go.seen[0];
		expect(call?.method).toBe("GET");
		const url = new URL(call?.url ?? "");
		expect(url.pathname).toBe("/v1/feed");
		expect(Object.fromEntries(url.searchParams)).toEqual({
			level: "constituency",
			limit: "20",
			cursor: "abc.xyz",
		});
		expect(call?.headers.authorization).toBe(`Bearer ${access}`);
		expect(call?.headers["accept-language"]).toBe("en");
	});

	it("refuses without a session and never calls the platform", async () => {
		const { go, handler } = start(() => ({ status: 200, body: {} }));
		const res = await handler(req("GET", "/api/feed", undefined, {}));
		expect(res.status).toBe(401);
		expect(go.seen).toHaveLength(0);
	});

	it("rejects an unknown level before calling the platform", async () => {
		const { go, handler } = start(() => ({ status: 200, body: {} }));
		expect((await handler(req("GET", "/api/feed?level=planet"))).status).toBe(400);
		expect(go.seen).toHaveLength(0);
	});

	it("passes a sponsored post's bilingual label through", async () => {
		const { handler } = start(() => ({
			status: 200,
			body: {
				items: [
					goPost({
						sponsored: true,
						sponsored_label: { en: "Sponsored civic message", sw: "Ujumbe uliofadhiliwa" },
					}),
				],
				has_more: false,
			},
		}));
		const body = await (await handler(req("GET", "/api/feed"))).json();
		expect(body.items[0].sponsored_label).toEqual({
			en: "Sponsored civic message",
			sw: "Ujumbe uliofadhiliwa",
		});
	});
});

describe("channels and posts", () => {
	it("lists the caller's ward channels", async () => {
		const { go, handler } = start(() => ({
			status: 200,
			body: {
				ward_id: 551,
				items: [
					{
						channel_id: "c1",
						ward_id: 551,
						name: "general",
						category: "general",
						read_only: false,
						can_post: true,
						state: "active",
						created_at: "x",
					},
				],
			},
		}));
		const body = await (await handler(req("GET", "/api/channels"))).json();
		expect(body.items[0]).toMatchObject({ channel_id: "c1", name: "general", read_only: false });
		expect(new URL(go.seen[0]?.url ?? "").pathname).toBe("/v1/channels");
	});

	it("creates a post in the chosen channel", async () => {
		const { go, handler } = start(() => ({ status: 201, body: goPost({ level: 1 }) }));
		const res = await handler(req("POST", "/api/channels/c1/posts", { content: "Habari" }));
		expect(res.status).toBe(201);
		expect(go.seen[0]).toMatchObject({ method: "POST", body: { content: "Habari" } });
		expect(new URL(go.seen[0]?.url ?? "").pathname).toBe("/v1/channels/c1/posts");
	});

	it("returns the platform's validation error for the composer", async () => {
		const { handler } = start(() => ({
			status: 422,
			body: {
				status: 422,
				code: "content_too_long",
				detail: "Too long.",
				errors: [{ field: "content", code: "too_long" }],
			},
		}));
		const res = await handler(req("POST", "/api/channels/c1/posts", { content: "x" }));
		expect(res.status).toBe(422);
		expect(await res.json()).toMatchObject({
			_tag: "ValidationFailed",
			code: "content_too_long",
			detail: "Too long.",
		});
	});

	it("refuses cross-site posts (CSRF)", async () => {
		const { go, handler } = start(() => ({ status: 201, body: goPost() }));
		// The BFF handler itself trusts its caller; the route wrapper checks Origin.
		const { withCsrfProtection } = await import("@/runtimes/csrf");
		const guarded = withCsrfProtection(({ request }) => handler(request));
		const res = await guarded({
			request: req(
				"POST",
				"/api/channels/c1/posts",
				{ content: "x" },
				{ ...signedIn, origin: "http://evil.test" },
			),
		});
		expect(res.status).toBe(403);
		expect(go.seen).toHaveLength(0);
	});
});

describe("likes", () => {
	it("likes and unlikes", async () => {
		const { go, handler } = start((_url, seen) =>
			seen.method === "POST"
				? { status: 201, body: { post_id: "p1", likes: 43, liked: true } }
				: { status: 200, body: { post_id: "p1", likes: 42, liked: false } },
		);
		const liked = await handler(req("POST", "/api/posts/p1/likes"));
		expect(liked.status).toBe(201);
		expect(await liked.json()).toEqual({ post_id: "p1", likes: 43, liked: true });
		const unliked = await handler(req("DELETE", "/api/posts/p1/likes"));
		expect(await unliked.json()).toEqual({ post_id: "p1", likes: 42, liked: false });
		expect(go.seen.map((s) => `${s.method} ${new URL(s.url).pathname}`)).toEqual([
			"POST /v1/posts/p1/likes",
			"DELETE /v1/posts/p1/likes",
		]);
	});

	it("reports an existing like as Conflict with its code", async () => {
		const { handler } = start(() => problem(409, "already_liked", "Already liked."));
		const res = await handler(req("POST", "/api/posts/p1/likes"));
		expect(res.status).toBe(409);
		expect(await res.json()).toMatchObject({ _tag: "Conflict", code: "already_liked" });
	});
});

describe("post detail and replies", () => {
	it("gets a post, its thread with cursor, and posts a reply", async () => {
		const { go, handler } = start((url, seen) => {
			if (seen.method === "POST")
				return { status: 201, body: goPost({ post_id: "r1", root_id: "p1", parent_id: "p1", level: 1 }) };
			if (url.pathname.endsWith("/replies"))
				return {
					status: 200,
					body: {
						post_id: "p1",
						items: [goPost({ post_id: "r1", root_id: "p1", parent_id: "p1" })],
						has_more: false,
					},
				};
			return { status: 200, body: goPost() };
		});
		expect(await (await handler(req("GET", "/api/posts/p1"))).json()).toMatchObject({
			post_id: "p1",
			level: 2,
		});
		const thread = await (await handler(req("GET", "/api/posts/p1/replies?limit=50&cursor=c"))).json();
		expect(thread).toMatchObject({
			post_id: "p1",
			has_more: false,
			items: [{ post_id: "r1", parent_id: "p1" }],
		});
		const reply = await handler(req("POST", "/api/posts/p1/replies", { content: "Asante" }));
		expect(reply.status).toBe(201);

		expect(go.seen.map((s) => `${s.method} ${new URL(s.url).pathname}${new URL(s.url).search}`)).toEqual([
			"GET /v1/posts/p1",
			"GET /v1/posts/p1/replies?cursor=c&limit=50",
			"POST /v1/posts/p1/replies",
		]);
		expect(go.seen[2]?.body).toEqual({ content: "Asante" });
	});

	it("reports a frozen post's refusal as Conflict", async () => {
		const { handler } = start(() => problem(409, "post_not_active"));
		const res = await handler(req("POST", "/api/posts/p1/replies", { content: "x" }));
		expect(await res.json()).toMatchObject({ _tag: "Conflict", code: "post_not_active" });
	});
});

describe("channel creation and views (W2.1)", () => {
	it("creates a read-only channel with snake_case fields", async () => {
		const { go, handler } = start(() => ({
			status: 201,
			body: {
				channel_id: "c9",
				ward_id: 551,
				name: "mca-updates",
				category: "safety",
				read_only: true,
				can_post: true,
				state: "active",
				created_at: "x",
			},
		}));
		const res = await handler(
			req("POST", "/api/channels", { name: "mca-updates", category: "safety", read_only: true }),
		);
		expect(res.status).toBe(201);
		expect(go.seen[0]?.body).toEqual({
			name: "mca-updates",
			description: "",
			category: "safety",
			read_only: true,
		});
	});

	it("gets a channel and its posts with cursor", async () => {
		const { go, handler } = start((url) =>
			url.pathname.endsWith("/posts")
				? { status: 200, body: { channel_id: "c1", items: [goPost()], next_cursor: "n", has_more: true } }
				: {
						status: 200,
						body: {
							channel_id: "c1",
							ward_id: 551,
							name: "water",
							category: "services",
							read_only: false,
							can_post: true,
							member_count: 7,
							state: "active",
							created_at: "x",
						},
					},
		);
		expect(await (await handler(req("GET", "/api/channels/c1"))).json()).toMatchObject({
			member_count: 7,
			can_post: true,
		});
		expect(await (await handler(req("GET", "/api/channels/c1/posts?cursor=abc"))).json()).toMatchObject({
			has_more: true,
			next_cursor: "n",
		});
		expect(go.seen.map((s) => `${new URL(s.url).pathname}${new URL(s.url).search}`)).toEqual([
			"/v1/channels/c1",
			"/v1/channels/c1/posts?cursor=abc",
		]);
	});

	it("passes the ward header through the channel list", async () => {
		const { handler } = start(() => ({
			status: 200,
			body: {
				ward_id: 551,
				member_count: 2,
				ward: { ward_id: 551, name: "Kiamwangi", constituency: "Gatundu South", county: "Kiambu" },
				items: [],
			},
		}));
		expect(await (await handler(req("GET", "/api/channels"))).json()).toMatchObject({
			member_count: 2,
			ward: { name: "Kiamwangi", county: "Kiambu" },
		});
	});
});
