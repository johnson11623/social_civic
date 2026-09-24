/**
 * Server-only client for the Go platform API. Never import from client code:
 * the .server.ts suffix keeps it (and BACKEND_URL) out of the browser bundle.
 */
import { FetchHttpClient, HttpClient, HttpClientRequest, type HttpClientResponse } from "@effect/platform";
import { Config, Duration, Effect, Option, Schema } from "effect";

import { BackendUnavailable, FieldError, UpstreamError, ValidationFailed } from "@/api/api-contract";

/** The Go API's RFC 7807 problem body (LLD v2.0 §6). */
const Problem = Schema.Struct({
	status: Schema.Int,
	code: Schema.String,
	detail: Schema.optionalWith(Schema.String, { default: () => "" }),
	errors: Schema.optionalWith(Schema.Array(FieldError), { default: () => [] }),
});

export type BackendError = ValidationFailed | BackendUnavailable | UpstreamError;

export interface GetOptions {
	readonly urlParams?: Readonly<Record<string, string | number | undefined>>;
	/** Caller's Accept-Language, forwarded so errors come back localized. */
	readonly acceptLanguage?: string | undefined;
}

const unavailable = new BackendUnavailable({ detail: "The platform service is unavailable." });

export class Backend extends Effect.Service<Backend>()("civic/Backend", {
	dependencies: [FetchHttpClient.layer],
	effect: Effect.gen(function* () {
		const baseUrl = yield* Config.string("BACKEND_URL").pipe(Config.withDefault("http://localhost:8090"));
		const timeout = yield* Config.duration("BACKEND_TIMEOUT").pipe(Config.withDefault(Duration.seconds(10)));
		const client = (yield* HttpClient.HttpClient).pipe(
			HttpClient.mapRequest(HttpClientRequest.prependUrl(baseUrl)),
		);

		const toProblem = (res: HttpClientResponse.HttpClientResponse) =>
			res.json.pipe(
				Effect.flatMap(Schema.decodeUnknown(Problem)),
				Effect.option,
				Effect.map((p): BackendError => {
					const problem = Option.getOrUndefined(p);
					if (res.status >= 500) return unavailable;
					if (!problem) return new UpstreamError({ status: res.status, code: "upstream_error", detail: "" });
					if (res.status === 422) {
						return new ValidationFailed({
							code: problem.code,
							detail: problem.detail,
							errors: problem.errors,
						});
					}
					return new UpstreamError({ status: res.status, code: problem.code, detail: problem.detail });
				}),
			);

		/** GET a JSON resource, decoding success with `schema`. */
		const get = <A, I>(
			path: string,
			schema: Schema.Schema<A, I>,
			options: GetOptions = {},
		): Effect.Effect<A, BackendError> => {
			const params = Object.fromEntries(
				Object.entries(options.urlParams ?? {})
					.filter((e): e is [string, string | number] => e[1] !== undefined)
					.map(([k, v]) => [k, String(v)]),
			);
			let request = HttpClientRequest.get(path).pipe(
				HttpClientRequest.acceptJson,
				HttpClientRequest.setUrlParams(params),
			);
			if (options.acceptLanguage) {
				request = HttpClientRequest.setHeader(request, "accept-language", options.acceptLanguage);
			}
			return client.execute(request).pipe(
				Effect.flatMap((res) =>
					res.status >= 200 && res.status < 300
						? res.json.pipe(
								Effect.flatMap(Schema.decodeUnknown(schema)),
								Effect.tapError((e) => Effect.logError("backend response did not match contract", path, e)),
								Effect.mapError(
									() =>
										new UpstreamError({
											status: res.status,
											code: "invalid_upstream_response",
											detail: "",
										}),
								),
							)
						: Effect.flatMap(toProblem(res), Effect.fail),
				),
				Effect.timeoutFail({ duration: timeout, onTimeout: () => unavailable }),
				Effect.catchTag("RequestError", () => Effect.fail(unavailable)),
				Effect.catchTag("ResponseError", () => Effect.fail(unavailable)),
			);
		};

		return { get } as const;
	}),
}) {}
