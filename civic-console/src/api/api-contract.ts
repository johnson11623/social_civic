/**
 * T-W1.1.2.1 — the web app's typed API contract (Effect HttpApi).
 *
 * This is the backend-for-frontend (BFF) the browser talks to at /api/*. Its
 * handlers (api-impl.server.ts) call the Go platform API. Wire format stays
 * snake_case like the Go API; TypeScript sees camelCase via Schema.fromKey.
 *
 * Client-safe: imported by both runtimes. No server-only code here.
 * Groups for moderation and billing are added with their backends.
 */
import { HttpApi, HttpApiEndpoint, HttpApiGroup, HttpApiSchema } from "@effect/platform";
import { Schema } from "effect";

import { MAX_POST_LENGTH } from "@/lib/limits";

// ---- Errors (mirroring the Go API's RFC 7807 codes) -----------------------

export const FieldError = Schema.Struct({ field: Schema.String, code: Schema.String });

/** 422 — request failed validation; `detail` is localized, `code` is stable. */
export class ValidationFailed extends Schema.TaggedError<ValidationFailed>()(
	"ValidationFailed",
	{ code: Schema.String, detail: Schema.String, errors: Schema.Array(FieldError) },
	HttpApiSchema.annotations({ status: 422 }),
) {}

/** 400 — malformed input, e.g. a national ID that is not 8 digits. */
export class InvalidInput extends Schema.TaggedError<InvalidInput>()(
	"InvalidInput",
	{ code: Schema.String, detail: Schema.String },
	HttpApiSchema.annotations({ status: 400 }),
) {}

/** 401 — wrong login code, or no valid session. */
export class Unauthorized extends Schema.TaggedError<Unauthorized>()(
	"Unauthorized",
	{ code: Schema.String, detail: Schema.String },
	HttpApiSchema.annotations({ status: 401 }),
) {}

/** 409 — e.g. national ID already registered. */
export class Conflict extends Schema.TaggedError<Conflict>()(
	"Conflict",
	{ code: Schema.String, detail: Schema.String },
	HttpApiSchema.annotations({ status: 409 }),
) {}

/** 429 — rate limited; retry after `retryAfter` seconds. */
export class RateLimited extends Schema.TaggedError<RateLimited>()(
	"RateLimited",
	{ detail: Schema.String, retryAfter: Schema.Int },
	HttpApiSchema.annotations({ status: 429 }),
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
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("search", "/boundary/search")
			.setUrlParams(SearchParams)
			.addSuccess(SearchResponse)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	) {}

// ---- Auth (W1.3) ----------------------------------------------------------------
// Tokens never reach browser JavaScript: the BFF keeps them in httpOnly
// cookies (T-W1.3.2.2) and exposes only the session below.

export const NationalId = Schema.String.pipe(Schema.pattern(/^\d{8}$/));

export const RegisterPayload = Schema.Struct({
	nationalId: NationalId,
	displayName: Schema.String.pipe(Schema.minLength(1), Schema.maxLength(100)),
	preferredLang: Schema.Literal("en", "sw"),
	phone: Schema.String.pipe(Schema.maxLength(32)),
	wardId: Schema.Int.pipe(Schema.between(1, 1450)),
	consentVersion: Schema.String.pipe(Schema.minLength(1), Schema.maxLength(32)),
	consentGranted: Schema.Literal(true),
});
export type RegisterPayload = typeof RegisterPayload.Type;

export const Group = Schema.Struct({ level: Schema.Int, id: Schema.Int, name: Schema.String });

export const Registered = Schema.Struct({
	displayName: Schema.String,
	groups: Schema.Array(Group),
});

export const OtpRequested = Schema.Struct({ expiresIn: Schema.Int });

export const Scope = Schema.Struct({ ward: Schema.Int, constituency: Schema.Int, county: Schema.Int });

export const Session = Schema.Struct({
	authenticated: Schema.Boolean,
	subject: Schema.optionalWith(Schema.String, { exact: true }),
	scope: Schema.optionalWith(Scope, { exact: true }),
	expiresAt: Schema.optionalWith(Schema.Int, { exact: true }),
	/** True when this response rotated the tokens (the access token had expired). */
	refreshed: Schema.optionalWith(Schema.Boolean, { exact: true }),
});
export type Session = typeof Session.Type;

export class AuthGroup extends HttpApiGroup.make("auth")
	.add(
		HttpApiEndpoint.post("register", "/auth/register")
			.setPayload(RegisterPayload)
			.addSuccess(Registered, { status: 201 })
			.addError(InvalidInput)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("requestOtp", "/auth/otp")
			.setPayload(Schema.Struct({ nationalId: NationalId }))
			.addSuccess(OtpRequested, { status: 202 })
			.addError(InvalidInput)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("login", "/auth/login")
			.setPayload(
				Schema.Struct({ nationalId: NationalId, otp: Schema.String.pipe(Schema.pattern(/^\d{6}$/)) }),
			)
			.addSuccess(Session)
			.addError(InvalidInput)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	// Refreshes automatically when the access token has expired.
	.add(HttpApiEndpoint.get("session", "/auth/session").addSuccess(Session))
	.add(
		HttpApiEndpoint.post("refresh", "/auth/refresh")
			.addSuccess(Session)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(HttpApiEndpoint.post("logout", "/auth/logout").addSuccess(Session)) {}

// ---- Posts, feed, channels (W1.4) ------------------------------------------------

export const Author = Schema.Struct({
	publicId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("public_id")),
	displayName: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("display_name")),
});

/** The Go API numbers levels 1 (ward) to 4 (national). */
export const LevelFromInt = Schema.transform(Schema.Literal(1, 2, 3, 4), Level, {
	strict: true,
	decode: (n) => (({ 1: "ward", 2: "constituency", 3: "county", 4: "national" }) as const)[n],
	encode: (l) => (({ ward: 1, constituency: 2, county: 3, national: 4 }) as const)[l],
});

export const PostCounts = Schema.Struct({ likes: Schema.Int, replies: Schema.Int });

/** A sponsored post's immutable disclosure; both languages are always shown. */
export const SponsorLabel = Schema.Struct({ en: Schema.String, sw: Schema.String });

export const PostState = Schema.Literal("active", "frozen", "tombstoned");

export const Post = Schema.Struct({
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	channelId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("channel_id")),
	channel: Schema.optionalWith(Schema.String, { exact: true }),
	level: LevelFromInt,
	wardId: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("ward_id")),
	/** Null once the post is removed. */
	content: Schema.NullOr(Schema.String),
	score: Schema.Number,
	state: PostState,
	author: Schema.optionalWith(Author, { exact: true }),
	counts: PostCounts,
	liked: Schema.optionalWith(Schema.Boolean, { exact: true }),
	sponsored: Schema.Boolean,
	sponsoredLabel: Schema.optionalWith(SponsorLabel, { exact: true }).pipe(Schema.fromKey("sponsored_label")),
	rootId: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("root_id")),
	parentId: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("parent_id")),
	createdAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("created_at")),
});
export type Post = typeof Post.Type;

export const FeedLevel = Schema.Literal("all", "ward", "constituency", "county", "national");
export type FeedLevel = typeof FeedLevel.Type;

export const FeedParams = Schema.Struct({
	level: Schema.optional(FeedLevel),
	cursor: Schema.optional(Schema.String.pipe(Schema.maxLength(512))),
	limit: Schema.optional(Schema.NumberFromString.pipe(Schema.int(), Schema.between(1, 50))),
});

export const FeedPage = Schema.Struct({
	items: Schema.Array(Post),
	nextCursor: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("next_cursor")),
	hasMore: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("has_more")),
});
export type FeedPage = typeof FeedPage.Type;

export const ChannelCategory = Schema.Literal("general", "services", "opportunities", "safety", "culture");

export const Channel = Schema.Struct({
	channelId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("channel_id")),
	wardId: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("ward_id")),
	name: Schema.String,
	description: Schema.optionalWith(Schema.String, { exact: true }),
	category: ChannelCategory,
	readOnly: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("read_only")),
});
export type Channel = typeof Channel.Type;

export const ChannelList = Schema.Struct({
	wardId: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("ward_id")),
	items: Schema.Array(Channel),
});
export type ChannelList = typeof ChannelList.Type;

export const LikeState = Schema.Struct({
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	likes: Schema.Int,
	liked: Schema.Boolean,
});
export type LikeState = typeof LikeState.Type;

const PostPath = Schema.Struct({ postId: Schema.String });

export const ThreadParams = Schema.Struct({
	cursor: Schema.optional(Schema.String.pipe(Schema.maxLength(512))),
	limit: Schema.optional(Schema.NumberFromString.pipe(Schema.int(), Schema.between(1, 100))),
});

/** A page of the replies under a top-level post, oldest first. */
export const ThreadPage = Schema.Struct({
	/** The thread's top-level post (also when asked via a reply). */
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	items: Schema.Array(Post),
	nextCursor: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("next_cursor")),
	hasMore: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("has_more")),
});
export type ThreadPage = typeof ThreadPage.Type;

export class PostsGroup extends HttpApiGroup.make("posts")
	.add(
		HttpApiEndpoint.get("feed", "/feed")
			.setUrlParams(FeedParams)
			.addSuccess(FeedPage)
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("channels", "/channels")
			.addSuccess(ChannelList)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("createPost", "/channels/:channelId/posts")
			.setPath(Schema.Struct({ channelId: Schema.String }))
			.setPayload(Schema.Struct({ content: Schema.String.pipe(Schema.maxLength(MAX_POST_LENGTH * 2)) }))
			.addSuccess(Post, { status: 201 })
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("post", "/posts/:postId")
			.setPath(PostPath)
			.addSuccess(Post)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("replies", "/posts/:postId/replies")
			.setPath(PostPath)
			.setUrlParams(ThreadParams)
			.addSuccess(ThreadPage)
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("reply", "/posts/:postId/replies")
			.setPath(PostPath)
			.setPayload(Schema.Struct({ content: Schema.String.pipe(Schema.maxLength(MAX_POST_LENGTH * 2)) }))
			.addSuccess(Post, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("like", "/posts/:postId/likes")
			.setPath(PostPath)
			.addSuccess(LikeState, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.del("unlike", "/posts/:postId/likes")
			.setPath(PostPath)
			.addSuccess(LikeState)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	) {}

// ---- Contract -----------------------------------------------------------------

export class ApiContract extends HttpApi.make("civic")
	.add(SystemGroup)
	.add(BoundaryGroup)
	.add(AuthGroup)
	.add(PostsGroup)
	.prefix("/api") {}
