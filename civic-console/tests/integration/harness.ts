import {
	HttpApiBuilder,
	HttpClient,
	type HttpClientRequest,
	HttpClientResponse,
	HttpServer,
	UrlParams,
} from "@effect/platform";
import { ConfigProvider, Effect, Either, Layer } from "effect";

import { ApiImplLive } from "@/api/api-impl.server";
import { Backend } from "@/services/backend.server";

export type Seen = { method: string; url: string; headers: Record<string, string>; body: unknown };
export type Reply = { status: number; body: unknown; headers?: Record<string, string> };

/** The full URL as the real fetch client would send it (url + urlParams). */
export const fullUrl = (request: HttpClientRequest.HttpClientRequest) =>
	Either.getOrThrow(UrlParams.makeUrl(request.url, request.urlParams, request.hash)).toString();

function bodyOf(request: HttpClientRequest.HttpClientRequest): unknown {
	const b = request.body;
	if (b._tag !== "Uint8Array") return undefined;
	return JSON.parse(new TextDecoder().decode(b.body));
}

/** A fake Go platform API recording every call. */
export function fakeGo(respond: (url: URL, seen: Seen) => Reply) {
	const seen: Seen[] = [];
	const layer = Layer.succeed(
		HttpClient.HttpClient,
		HttpClient.make((request) => {
			const url = new URL(fullUrl(request));
			const call: Seen = {
				method: request.method,
				url: url.toString(),
				headers: { ...request.headers },
				body: bodyOf(request),
			};
			seen.push(call);
			const r = respond(url, call);
			return Effect.succeed(
				HttpClientResponse.fromWeb(
					request,
					new Response(JSON.stringify(r.body), {
						status: r.status,
						headers: { "content-type": "application/json", ...r.headers },
					}),
				),
			);
		}),
	);
	return { seen, layer };
}

/** The BFF's /api/* web handler wired to a fake Go API. Call dispose() after. */
export function bff(go: ReturnType<typeof fakeGo>, config: Record<string, string> = {}) {
	const backend = Backend.DefaultWithoutDependencies.pipe(
		Layer.provide(go.layer),
		Layer.provide(
			Layer.setConfigProvider(ConfigProvider.fromJson({ BACKEND_URL: "http://go.test", ...config })),
		),
	);
	return HttpApiBuilder.toWebHandler(
		Layer.mergeAll(ApiImplLive.pipe(Layer.provide(backend)), HttpServer.layerContext),
	);
}

/** A JWT-shaped token with the given claims (unsigned; the BFF only decodes). */
export function fakeJwt(claims: Record<string, unknown>) {
	const enc = (o: unknown) => Buffer.from(JSON.stringify(o)).toString("base64url");
	return `${enc({ alg: "HS256", typ: "JWT" })}.${enc(claims)}.sig`;
}
