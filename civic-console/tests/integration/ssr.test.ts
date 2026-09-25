import { ConfigProvider, Effect, Layer, ManagedRuntime } from "effect";
import { makeSsrApiClientLayer } from "effect-tanstack-start/server";
import { afterEach, describe, expect, it, vi } from "vitest";

// During SSR loaders call the handlers directly (no HTTP). These calls must
// behave like the browser's: decoded values, typed failures, and no reliance
// on a socket the synthetic request doesn't have.

const forwarded = vi.hoisted(() => ({ headers: {} as Record<string, string> }));
vi.mock("@tanstack/react-start/server", () => ({ getRequestHeaders: () => forwarded.headers }));

const { ApiContract } = await import("@/api/api-contract");
const { ApiImplLive } = await import("@/api/api-impl.server");
const { ApiClient } = await import("@/services/api-client-tag");
const { Backend } = await import("@/services/backend.server");
const { fakeGo, fakeJwt } = await import("./harness");

let runtime: ManagedRuntime.ManagedRuntime<unknown, unknown> | undefined;
afterEach(async () => {
	await runtime?.dispose();
	runtime = undefined;
});

function ssr(respond: Parameters<typeof fakeGo>[0]) {
	const go = fakeGo(respond);
	const backend = Backend.DefaultWithoutDependencies.pipe(
		Layer.provide(go.layer),
		Layer.provide(Layer.setConfigProvider(ConfigProvider.fromJson({ BACKEND_URL: "http://go.test" }))),
	);
	const rt = ManagedRuntime.make(
		makeSsrApiClientLayer(ApiContract, ApiImplLive, ApiClient).pipe(Layer.provideMerge(backend)),
	);
	runtime = rt as ManagedRuntime.ManagedRuntime<unknown, unknown>;
	// biome-ignore lint/suspicious/noExplicitAny: the SSR client's type is the contract's client
	const call = <A>(fn: (api: any) => Effect.Effect<A, unknown>) =>
		rt.runPromise(Effect.either(Effect.flatMap(ApiClient, fn)));
	return { go, call };
}

const access = fakeJwt({ sub: "u", typ: "access", exp: Math.floor(Date.now() / 1000) + 900 });

describe("SSR API calls", () => {
	it("return the decoded Type, with numeric URL params as the loader passes them", async () => {
		forwarded.headers = { cookie: `civic_at=${access}`, "x-forwarded-for": "197.232.1.1" };
		const { go, call } = ssr(() => ({
			status: 200,
			body: {
				items: [
					{
						post_id: "p1",
						channel_id: "c1",
						level: 3,
						ward_id: 551,
						content: "Hi",
						score: 1,
						state: "active",
						counts: { likes: 0, replies: 0 },
						sponsored: false,
						created_at: "2026-09-24T08:15:00Z",
					},
				],
				next_cursor: "n",
				has_more: true,
			},
		}));
		const res = await call((api) => api.posts.feed({ urlParams: { level: "county", limit: 20 } }));
		expect(res).toMatchObject({
			_tag: "Right",
			right: {
				hasMore: true,
				nextCursor: "n",
				items: [{ postId: "p1", level: "county", createdAt: "2026-09-24T08:15:00Z" }],
			},
		});
		// No socket during SSR: the browser's forwarded IP is used.
		expect(go.seen[0]?.headers["x-forwarded-for"]).toBe("197.232.1.1");
		expect(new URL(go.seen[0]?.url ?? "").searchParams.get("limit")).toBe("20");
	});

	it("fail with the contract's typed error", async () => {
		forwarded.headers = {};
		const { go, call } = ssr(() => ({ status: 200, body: {} }));
		const res = await call((api) => api.posts.channels());
		expect(res).toMatchObject({ _tag: "Left", left: { _tag: "Unauthorized", code: "no_session" } });
		expect(go.seen).toHaveLength(0);
	});

	it("pass platform errors through typed", async () => {
		forwarded.headers = { cookie: `civic_at=${access}` };
		const { call } = ssr(() => ({ status: 503, body: {} }));
		const res = await call((api) => api.posts.feed({ urlParams: {} }));
		expect(res).toMatchObject({ _tag: "Left", left: { _tag: "BackendUnavailable" } });
	});
});
