/**
 * Server-only client for the Go platform API. Never import from client code:
 * the .server.ts suffix keeps it (and BACKEND_URL) out of the browser bundle.
 */
import {
	FetchHttpClient,
	HttpClient,
	HttpClientRequest,
	type HttpClientResponse,
	type HttpMethod,
} from "@effect/platform";
import { Config, Duration, Effect, Option, Schema } from "effect";

import {
	BackendUnavailable,
	Conflict,
	FieldError,
	InvalidInput,
	RateLimited,
	Unauthorized,
	UpstreamError,
	ValidationFailed,
} from "@/api/api-contract";

/** The Go API's RFC 7807 problem body (LLD v2.0 §6). */
const Problem = Schema.Struct({
	status: Schema.Int,
	code: Schema.String,
	detail: Schema.optionalWith(Schema.String, { default: () => "" }),
	errors: Schema.optionalWith(Schema.Array(FieldError), { default: () => [] }),
});

export type BackendError =
	| InvalidInput
	| Unauthorized
	| Conflict
	| ValidationFailed
	| RateLimited
	| BackendUnavailable
	| UpstreamError;

export interface RequestOptions {
	readonly urlParams?: Readonly<Record<string, string | number | undefined>>;
	/** JSON body (snake_case, as the Go API expects). */
	readonly body?: unknown;
	/** Caller's language, forwarded so errors come back localized. */
	readonly acceptLanguage?: string | undefined;
	/** Access token for authenticated endpoints. */
	readonly bearer?: string | undefined;
	/** The browser's IP, for the Go API's per-IP rate limits. */
	readonly forwardedFor?: string | undefined;
}

const unavailable = new BackendUnavailable({ detail: "The platform service is unavailable." });

/** Map a non-2xx Go response to the contract's typed errors. */
function toError(res: HttpClientResponse.HttpClientResponse): Effect.Effect<BackendError> {
	return res.json.pipe(
		Effect.flatMap(Schema.decodeUnknown(Problem)),
		Effect.option,
		Effect.map(Option.getOrUndefined),
		Effect.map((p): BackendError => {
			const code = p?.code ?? "upstream_error";
			const detail = p?.detail ?? "";
			switch (true) {
				case res.status >= 500:
					return unavailable;
				case res.status === 400:
					return new InvalidInput({ code, detail });
				case res.status === 401:
					return new Unauthorized({ code, detail });
				case res.status === 409:
					return new Conflict({ code, detail });
				case res.status === 422:
					return new ValidationFailed({ code, detail, errors: p?.errors ?? [] });
				case res.status === 429: {
					const retryAfter = Number.parseInt(res.headers["retry-after"] ?? "", 10);
					return new RateLimited({ detail, retryAfter: Number.isFinite(retryAfter) ? retryAfter : 60 });
				}
				default:
					return new UpstreamError({ status: res.status, code, detail });
			}
		}),
	);
}

export class Backend extends Effect.Service<Backend>()("civic/Backend", {
	dependencies: [FetchHttpClient.layer],
	effect: Effect.gen(function* () {
		const baseUrl = yield* Config.string("BACKEND_URL").pipe(Config.withDefault("http://localhost:8090"));
		const timeout = yield* Config.duration("BACKEND_TIMEOUT").pipe(Config.withDefault(Duration.seconds(10)));
		const client = (yield* HttpClient.HttpClient).pipe(
			HttpClient.mapRequest(HttpClientRequest.prependUrl(baseUrl)),
		);

		/** Call the Go API and decode a 2xx body with `schema`. */
		const request = <A, I>(
			method: HttpMethod.HttpMethod,
			path: string,
			schema: Schema.Schema<A, I>,
			options: RequestOptions = {},
		): Effect.Effect<A, BackendError> => {
			const params = Object.fromEntries(
				Object.entries(options.urlParams ?? {})
					.filter((e): e is [string, string | number] => e[1] !== undefined)
					.map(([k, v]) => [k, String(v)]),
			);
			let req = HttpClientRequest.make(method)(path).pipe(
				HttpClientRequest.acceptJson,
				HttpClientRequest.setUrlParams(params),
			);
			if (options.acceptLanguage)
				req = HttpClientRequest.setHeader(req, "accept-language", options.acceptLanguage);
			if (options.bearer) req = HttpClientRequest.bearerToken(req, options.bearer);
			if (options.forwardedFor)
				req = HttpClientRequest.setHeader(req, "x-forwarded-for", options.forwardedFor);
			if (options.body !== undefined) req = HttpClientRequest.bodyUnsafeJson(req, options.body);

			return client.execute(req).pipe(
				Effect.flatMap((res) =>
					res.status >= 200 && res.status < 300
						? res.json.pipe(
								Effect.flatMap(Schema.decodeUnknown(schema)),
								Effect.tapError((e) => Effect.logError("backend response did not match contract", path, e)),
								Effect.mapError(
									() =>
										new UpstreamError({ status: res.status, code: "invalid_upstream_response", detail: "" }),
								),
							)
						: Effect.flatMap(toError(res), Effect.fail),
				),
				Effect.timeoutFail({ duration: timeout, onTimeout: () => unavailable }),
				Effect.catchTag("RequestError", () => Effect.fail(unavailable)),
				Effect.catchTag("ResponseError", () => Effect.fail(unavailable)),
			);
		};

		return {
			request,
			get: <A, I>(path: string, schema: Schema.Schema<A, I>, options?: RequestOptions) =>
				request("GET", path, schema, options),
			post: <A, I>(path: string, schema: Schema.Schema<A, I>, options?: RequestOptions) =>
				request("POST", path, schema, options),
		} as const;
	}),
}) {}
