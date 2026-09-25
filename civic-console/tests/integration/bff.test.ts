import { HttpApiClient, HttpClient, HttpClientResponse } from "@effect/platform";
import { Effect, Layer } from "effect";
import { afterEach, describe, expect, it } from "vitest";

import { ApiContract } from "@/api/api-contract";
import { fakeGo, fullUrl, bff as makeBff } from "./harness";

// W1.1.2 — the BFF handlers end to end: browser request → /api/* handler →
// Backend client → (fake) Go API, and back through the typed client.

let dispose: (() => Promise<void>) | undefined;
afterEach(async () => {
	await dispose?.();
	dispose = undefined;
});

function bff(go: ReturnType<typeof fakeGo>) {
	const web = makeBff(go);
	dispose = web.dispose;
	return web.handler;
}

const kiamwangi = {
	level: "ward",
	code: 551,
	iebc_code: "0551",
	name: "Kiamwangi",
	label: "Kiamwangi, Gatundu South, Kiambu",
	constituency: { level: "constituency", code: 111, iebc_code: "111", name: "Gatundu South" },
	county: { level: "county", code: 22, iebc_code: "022", name: "Kiambu" },
};

describe("BFF /api/boundary/search", () => {
	it("proxies to the Go API, forwarding params and Accept-Language", async () => {
		const go = fakeGo(() => ({ status: 200, body: { query: "kiamwngi", items: [kiamwangi] } }));
		const res = await bff(go)(
			new Request("http://web.test/api/boundary/search?q=kiamwngi&level=ward&limit=5", {
				headers: { "accept-language": "sw-KE" },
			}),
		);
		expect(res.status).toBe(200);
		const body = await res.json();
		expect(body.items[0]).toMatchObject({
			code: 551,
			iebc_code: "0551",
			label: "Kiamwangi, Gatundu South, Kiambu",
		});

		const call = go.seen[0];
		expect(call?.url).toBe("http://go.test/v1/boundary/search?q=kiamwngi&level=ward&limit=5");
		expect(call?.headers["accept-language"]).toBe("sw-KE");
	});

	it("prefers the user's `lang` cookie over the browser's Accept-Language (T-W1.1.3.4)", async () => {
		const go = fakeGo(() => ({ status: 200, body: { query: "ka", items: [] } }));
		await bff(go)(
			new Request("http://web.test/api/boundary/search?q=ka", {
				headers: { "accept-language": "en-US,en;q=0.9", cookie: "theme=dark; lang=sw" },
			}),
		);
		expect(go.seen[0]?.headers["accept-language"]).toBe("sw");
	});

	it("maps Go 422 problems to ValidationFailed with localized detail", async () => {
		const go = fakeGo(() => ({
			status: 422,
			body: {
				type: "…",
				title: "Data haikubaliki",
				status: 422,
				code: "validation_failed",
				detail: "Andika angalau herufi au tarakimu 2 ili kutafuta.",
				lang: "sw",
				errors: [{ field: "q", code: "too_short" }],
			},
		}));
		const res = await bff(go)(new Request("http://web.test/api/boundary/search?q=a"));
		expect(res.status).toBe(422);
		expect(await res.json()).toMatchObject({
			_tag: "ValidationFailed",
			code: "validation_failed",
			detail: "Andika angalau herufi au tarakimu 2 ili kutafuta.",
			errors: [{ field: "q", code: "too_short" }],
		});
	});

	it("rejects invalid params before calling the Go API", async () => {
		const go = fakeGo(() => ({ status: 200, body: {} }));
		const res = await bff(go)(new Request("http://web.test/api/boundary/search?q=ka&limit=500"));
		expect(res.status).toBe(400);
		expect(go.seen).toHaveLength(0);
	});

	it("reports BackendUnavailable when the Go API returns 5xx", async () => {
		const go = fakeGo(() => ({ status: 503, body: { status: 503, code: "kms_unavailable" } }));
		const res = await bff(go)(new Request("http://web.test/api/boundary/search?q=ka"));
		expect(res.status).toBe(503);
		expect((await res.json())._tag).toBe("BackendUnavailable");
	});

	it("reports UpstreamError when the Go response breaks the contract", async () => {
		const go = fakeGo(() => ({ status: 200, body: { query: "ka", items: [{ level: "village" }] } }));
		const res = await bff(go)(new Request("http://web.test/api/boundary/search?q=ka"));
		expect(res.status).toBe(502);
		expect(await res.json()).toMatchObject({ _tag: "UpstreamError", code: "invalid_upstream_response" });
	});
});

describe("BFF /api/health", () => {
	it("reports the backend as ok or unavailable without failing", async () => {
		const up = await bff(fakeGo(() => ({ status: 200, body: { status: "ok", time: "…" } })))(
			new Request("http://web.test/api/health"),
		);
		expect(await up.json()).toEqual({ status: "ok", backend: "ok" });
		await dispose?.();

		const down = await bff(fakeGo(() => ({ status: 503, body: {} })))(
			new Request("http://web.test/api/health"),
		);
		expect(down.status).toBe(200);
		expect(await down.json()).toEqual({ status: "ok", backend: "unavailable" });
	});
});

describe("typed client", () => {
	it("decodes snake_case wire data into camelCase types", async () => {
		const go = fakeGo(() => ({ status: 200, body: { query: "kiamwangi", items: [kiamwangi] } }));
		const handler = bff(go);
		const client = HttpApiClient.make(ApiContract, { baseUrl: "http://web.test" }).pipe(
			Effect.provide(
				Layer.succeed(
					HttpClient.HttpClient,
					HttpClient.make((req) =>
						Effect.promise(() => handler(new Request(fullUrl(req)))).pipe(
							Effect.map((res) => HttpClientResponse.fromWeb(req, res)),
						),
					),
				),
			),
		);
		const result = await Effect.runPromise(
			Effect.flatMap(client, (api) => api.boundary.search({ urlParams: { q: "kiamwangi" } })),
		);
		expect(result.items[0]?.iebcCode).toBe("0551");
		expect(result.items[0]?.county?.name).toBe("Kiambu");
	});
});
