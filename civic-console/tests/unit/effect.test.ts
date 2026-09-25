import { Effect, Either, Schema } from "effect";
import { describe, expect, it } from "vitest";

// T-W1.1.1.5 / T-W1.1.1.6 — Effect runs, Schema (now part of `effect`) decodes,
// and the TanStack Start integration resolves.
describe("effect", () => {
	it("runs an effect program", async () => {
		const program = Effect.succeed(21).pipe(Effect.map((n) => n * 2));
		await expect(Effect.runPromise(program)).resolves.toBe(42);
	});

	it("types failures", () => {
		const result = Effect.runSync(Effect.either(Effect.fail({ _tag: "NotFound" as const })));
		expect(Either.isLeft(result) && result.left._tag).toBe("NotFound");
	});

	it("decodes with Schema", () => {
		const Ward = Schema.Struct({ code: Schema.Int, name: Schema.NonEmptyString });
		expect(Schema.decodeUnknownSync(Ward)({ code: 17, name: "Ziwa la Ng'ombe" })).toEqual({
			code: 17,
			name: "Ziwa la Ng'ombe",
		});
		expect(() => Schema.decodeUnknownSync(Ward)({ code: "17", name: "" })).toThrow();
	});

	it("resolves effect-tanstack-start", async () => {
		const client = await import("effect-tanstack-start/client");
		expect(typeof client.makeApiClientTag).toBe("function");
	});
});
