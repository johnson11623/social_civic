/**
 * T-W1.1.2.1 — the web app's typed API contract (Effect HttpApi).
 *
 * This is the backend-for-frontend (BFF) the browser talks to at /api/*. Its
 * handlers (api-impl.server.ts) call the Go platform API. Wire format stays
 * snake_case like the Go API; TypeScript sees camelCase via Schema.fromKey.
 *
 * Client-safe: imported by both runtimes. No server-only code here.
 * Groups for posts, moderation and billing are added with their backends.
 */
import { HttpApi, HttpApiEndpoint, HttpApiGroup, HttpApiSchema } from "@effect/platform";
import { Schema } from "effect";

// ---- Errors (mirroring the Go API's RFC 7807 codes) -----------------------

export const FieldError = Schema.Struct({ field: Schema.String, code: Schema.String });

/** 422 — request failed validation; `detail` is localized, `code` is stable. */
export class ValidationFailed extends Schema.TaggedError<ValidationFailed>()(
	"ValidationFailed",
	{ code: Schema.String, detail: Schema.String, errors: Schema.Array(FieldError) },
	HttpApiSchema.annotations({ status: 422 }),
) {}

/** 503 — the platform API is unreachable or failing. */
export class BackendUnavailable extends Schema.TaggedError<BackendUnavailable>()(
	"BackendUnavailable",
	{ detail: Schema.String },
	HttpApiSchema.annotations({ status: 503 }),
) {}

/** Any other platform error, passed through with its status and code. */
export class UpstreamError extends Schema.TaggedError<UpstreamError>()(
	"UpstreamError",
	{ status: Schema.Int, code: Schema.String, detail: Schema.String },
	HttpApiSchema.annotations({ status: 502 }),
) {}

// ---- System -----------------------------------------------------------------

export const Health = Schema.Struct({
	status: Schema.Literal("ok"),
	backend: Schema.Literal("ok", "unavailable"),
});

export class SystemGroup extends HttpApiGroup.make("system").add(
	HttpApiEndpoint.get("health", "/health").addSuccess(Health),
) {}

// ---- Boundary (IEBC counties, constituencies, wards) --------------------------

export const Level = Schema.Literal("ward", "constituency", "county", "national");
export type Level = typeof Level.Type;

const unitFields = {
	level: Level,
	code: Schema.Int,
	iebcCode: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("iebc_code")),
	name: Schema.String,
};

export const UnitRef = Schema.Struct(unitFields);
export type UnitRef = typeof UnitRef.Type;

export interface TreeNode {
	readonly level: Level;
	readonly code: number;
	readonly iebcCode: string;
	readonly name: string;
	readonly children?: ReadonlyArray<TreeNode>;
}
interface TreeNodeEncoded {
	readonly level: Level;
	readonly code: number;
	readonly iebc_code: string;
	readonly name: string;
	readonly children?: ReadonlyArray<TreeNodeEncoded>;
}
export const TreeNode: Schema.Schema<TreeNode, TreeNodeEncoded> = Schema.Struct({
	...unitFields,
	children: Schema.optionalWith(
		Schema.Array(Schema.suspend((): Schema.Schema<TreeNode, TreeNodeEncoded> => TreeNode)),
		{ exact: true },
	),
});

export const BoundaryTree = Schema.Struct({ version: Schema.String, counties: Schema.Array(TreeNode) });
export type BoundaryTree = typeof BoundaryTree.Type;

export const SearchResult = Schema.Struct({
	...unitFields,
	label: Schema.String,
	constituency: Schema.optionalWith(UnitRef, { exact: true }),
	county: Schema.optionalWith(UnitRef, { exact: true }),
});
export type SearchResult = typeof SearchResult.Type;

export const SearchResponse = Schema.Struct({ query: Schema.String, items: Schema.Array(SearchResult) });
export type SearchResponse = typeof SearchResponse.Type;

export const SearchParams = Schema.Struct({
	q: Schema.String,
	level: Schema.optional(Schema.Literal("ward", "constituency", "county")),
	limit: Schema.optional(Schema.NumberFromString.pipe(Schema.int(), Schema.between(1, 50))),
});

export class BoundaryGroup extends HttpApiGroup.make("boundary")
	.add(
		HttpApiEndpoint.get("tree", "/boundary/tree")
			.addSuccess(BoundaryTree)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("search", "/boundary/search")
			.setUrlParams(SearchParams)
			.addSuccess(SearchResponse)
			.addError(ValidationFailed)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	) {}

// ---- Contract -----------------------------------------------------------------

export class ApiContract extends HttpApi.make("civic").add(SystemGroup).add(BoundaryGroup).prefix("/api") {}
