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

import { MAX_CHANNEL_DESCRIPTION, MAX_POST_LENGTH } from "@/lib/limits";

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
	/** The access token was issued after a two-step verification code (F-08). */
	mfa: Schema.optionalWith(Schema.Boolean, { exact: true }),
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
	/** Profile photo; initials are shown without one. */
	avatarUrl: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("avatar_url")),
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

/** One processed image size: WebP with a JPEG fallback, on the media CDN. */
export const MediaVariant = Schema.Struct({
	name: Schema.String,
	width: Schema.Int,
	height: Schema.Int,
	/** Absent when the worker's ffmpeg has no WebP encoder (JPEG only). */
	webpUrl: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("webp_url")),
	jpegUrl: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("jpeg_url")),
});
export type MediaVariant = typeof MediaVariant.Type;

/** An uploaded image or video and, once processed, its variants (docs/media). */
export const Media = Schema.Struct({
	mediaId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("media_id")),
	kind: Schema.Literal("image", "video"),
	state: Schema.Literal("uploading", "processing", "ready", "failed"),
	altText: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("alt_text")),
	width: Schema.optionalWith(Schema.Int, { exact: true }),
	height: Schema.optionalWith(Schema.Int, { exact: true }),
	durationMs: Schema.optionalWith(Schema.Int, { exact: true }).pipe(Schema.fromKey("duration_ms")),
	/** Tiny blurred data: URI shown while the real image loads. */
	placeholder: Schema.optionalWith(Schema.String, { exact: true }),
	images: Schema.optionalWith(Schema.Array(MediaVariant), { exact: true }),
	poster: Schema.optionalWith(MediaVariant, { exact: true }),
	hlsUrl: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("hls_url")),
	error: Schema.optionalWith(Schema.String, { exact: true }),
});
export type Media = typeof Media.Type;

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
	/** Frozen or removed: the decision, its harm and the appeal deadline. */
	moderation: Schema.optionalWith(
		Schema.Struct({
			actionId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("action_id")),
			action: Schema.String,
			reasonCode: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("reason_code")),
			appealDueAt: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("appeal_due_at")),
		}),
		{ exact: true },
	),
	media: Schema.optionalWith(Media, { exact: true }),
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
	/** Whether the caller may post here (read-only channels: their creator). */
	canPost: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("can_post")),
	memberCount: Schema.optionalWith(Schema.Int, { exact: true }).pipe(Schema.fromKey("member_count")),
});
export type Channel = typeof Channel.Type;
export type ChannelCategory = typeof ChannelCategory.Type;

export const WardInfo = Schema.Struct({
	wardId: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("ward_id")),
	name: Schema.String,
	constituency: Schema.String,
	county: Schema.String,
});
export type WardInfo = typeof WardInfo.Type;

export const ChannelList = Schema.Struct({
	wardId: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("ward_id")),
	ward: Schema.optionalWith(WardInfo, { exact: true }),
	memberCount: Schema.optionalWith(Schema.Int, { exact: true }).pipe(Schema.fromKey("member_count")),
	items: Schema.Array(Channel),
});

export const CreateChannelPayload = Schema.Struct({
	name: Schema.String.pipe(Schema.maxLength(80)),
	description: Schema.optionalWith(Schema.String.pipe(Schema.maxLength(MAX_CHANNEL_DESCRIPTION * 2)), {
		exact: true,
	}),
	category: ChannelCategory,
	readOnly: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("read_only")),
});
export type CreateChannelPayload = typeof CreateChannelPayload.Type;

export const ChannelPostsParams = Schema.Struct({
	cursor: Schema.optional(Schema.String.pipe(Schema.maxLength(512))),
	limit: Schema.optional(Schema.NumberFromString.pipe(Schema.int(), Schema.between(1, 50))),
});

export const ChannelPostsPage = Schema.Struct({
	channelId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("channel_id")),
	items: Schema.Array(Post),
	nextCursor: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("next_cursor")),
	hasMore: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("has_more")),
});
export type ChannelPostsPage = typeof ChannelPostsPage.Type;
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
		HttpApiEndpoint.post("createChannel", "/channels")
			.setPayload(CreateChannelPayload)
			.addSuccess(Channel, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("channel", "/channels/:channelId")
			.setPath(Schema.Struct({ channelId: Schema.String }))
			.addSuccess(Channel)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("channelPosts", "/channels/:channelId/posts")
			.setPath(Schema.Struct({ channelId: Schema.String }))
			.setUrlParams(ChannelPostsParams)
			.addSuccess(ChannelPostsPage)
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("createPost", "/channels/:channelId/posts")
			.setPath(Schema.Struct({ channelId: Schema.String }))
			.setPayload(
				Schema.Struct({
					content: Schema.String.pipe(Schema.maxLength(MAX_POST_LENGTH * 2)),
					mediaId: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("media_id")),
				}),
			)
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

// ---- Moderation (W2.2) ------------------------------------------------------------

/** The platform's harm-based policy: the only grounds to report or remove. */
export const ReasonCode = Schema.Literal("hate_speech", "incitement", "privacy", "child_safety");
export type ReasonCode = typeof ReasonCode.Type;

export const RoleAssignment = Schema.Struct({
	assignmentId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("assignment_id")),
	role: Schema.String,
	/** Moderator roles: the level governed (1 ward … 4 national). */
	level: Schema.optionalWith(Schema.Int, { exact: true }),
	unitCode: Schema.optionalWith(Schema.Int, { exact: true }).pipe(Schema.fromKey("unit_code")),
});
export type RoleAssignment = typeof RoleAssignment.Type;
export const RoleList = Schema.Struct({ items: Schema.Array(RoleAssignment) });

export const ReportPayload = Schema.Struct({
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	reasonCode: Schema.propertySignature(ReasonCode).pipe(Schema.fromKey("reason_code")),
	details: Schema.optionalWith(Schema.String.pipe(Schema.maxLength(1000)), { exact: true }),
});
export const ReportFiled = Schema.Struct({
	reportId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("report_id")),
	state: Schema.String,
	queue: Schema.String,
});

export const QueueParams = Schema.Struct({
	level: Schema.optional(FeedLevel),
	filter: Schema.optional(Schema.Literal("open", "triage", "all")),
	sort: Schema.optional(Schema.Literal("age", "reports")),
});
export type QueueParams = typeof QueueParams.Type;

export const QueueItem = Schema.Struct({
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	level: LevelFromInt,
	wardId: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("ward_id")),
	content: Schema.NullOr(Schema.String),
	state: PostState,
	author: Author,
	reportCount: Schema.propertySignature(Schema.Int).pipe(Schema.fromKey("report_count")),
	reasons: Schema.Array(ReasonCode),
	firstReportedAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("first_reported_at")),
	postedAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("posted_at")),
});
export type QueueItem = typeof QueueItem.Type;
export const Queue = Schema.Struct({ items: Schema.Array(QueueItem) });

export const ModerationActionKind = Schema.Literal("hide", "delete", "freeze", "restore");
export type ModerationActionKind = typeof ModerationActionKind.Type;

export const ActionPayload = Schema.Struct({
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	action: ModerationActionKind,
	reasonCode: Schema.propertySignature(ReasonCode).pipe(Schema.fromKey("reason_code")),
	notes: Schema.optionalWith(Schema.String.pipe(Schema.maxLength(2000)), { exact: true }),
});
export const ActionRecorded = Schema.Struct({
	actionId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("action_id")),
	postState: Schema.propertySignature(PostState).pipe(Schema.fromKey("post_state")),
	appealDueAt: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("appeal_due_at")),
});

export const HistoryEntry = Schema.Struct({
	actionId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("action_id")),
	action: ModerationActionKind,
	reasonCode: Schema.propertySignature(ReasonCode).pipe(Schema.fromKey("reason_code")),
	level: LevelFromInt,
	postState: Schema.propertySignature(PostState).pipe(Schema.fromKey("post_state")),
	appealDueAt: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("appeal_due_at")),
	overturned: Schema.Boolean,
	createdAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("created_at")),
	notes: Schema.optionalWith(Schema.String, { exact: true }),
	moderator: Schema.optionalWith(Author, { exact: true }),
});
export type HistoryEntry = typeof HistoryEntry.Type;
export const History = Schema.Struct({
	postId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("post_id")),
	items: Schema.Array(HistoryEntry),
});

export const AppealPayload = Schema.Struct({
	moderationId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("moderation_id")),
	statement: Schema.String.pipe(Schema.maxLength(4000)),
});
export const AppealFiled = Schema.Struct({
	appealId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("appeal_id")),
	state: Schema.String,
	dueAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("due_at")),
});

export class ModerationGroup extends HttpApiGroup.make("moderation")
	.add(
		HttpApiEndpoint.get("roles", "/me/roles")
			.addSuccess(RoleList)
			.addError(Unauthorized)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("report", "/reports")
			.setPayload(ReportPayload)
			.addSuccess(ReportFiled, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("queue", "/moderation/queue")
			.setUrlParams(QueueParams)
			.addSuccess(Queue)
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("act", "/moderation/actions")
			.setPayload(ActionPayload)
			.addSuccess(ActionRecorded, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("history", "/posts/:postId/moderation")
			.setPath(Schema.Struct({ postId: Schema.String }))
			.addSuccess(History)
			.addError(Unauthorized)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("appeal", "/appeals")
			.setPayload(AppealPayload)
			.addSuccess(AppealFiled, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	) {}

// ---- Account (profile, settings, privacy, two-step verification) -----------------

export const Profile = Schema.Struct({
	publicId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("public_id")),
	displayName: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("display_name")),
	preferredLang: Schema.propertySignature(Schema.Literal("en", "sw")).pipe(Schema.fromKey("preferred_lang")),
	memberSince: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("member_since")),
	ward: Schema.optionalWith(WardInfo, { exact: true }),
	consent: Schema.Struct({
		version: Schema.String,
		active: Schema.Boolean,
		grantedAt: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("granted_at")),
		withdrawnAt: Schema.optionalWith(Schema.String, { exact: true }).pipe(Schema.fromKey("withdrawn_at")),
	}),
	mfaEnabled: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("mfa_enabled")),
	avatar: Schema.optionalWith(
		Schema.Struct({
			mediaId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("media_id")),
			url: Schema.String,
		}),
		{ exact: true },
	),
	erasure: Schema.optionalWith(
		Schema.Struct({
			requestId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("request_id")),
			state: Schema.Literal("pending", "failed"),
			requestedAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("requested_at")),
			completionBy: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("completion_by")),
		}),
		{ exact: true },
	),
});
export type Profile = typeof Profile.Type;

export const ProfileUpdate = Schema.Struct({
	displayName: Schema.optionalWith(Schema.String.pipe(Schema.maxLength(200)), { exact: true }).pipe(
		Schema.fromKey("display_name"),
	),
	preferredLang: Schema.optionalWith(Schema.Literal("en", "sw"), { exact: true }).pipe(
		Schema.fromKey("preferred_lang"),
	),
});

export const AvatarChoice = Schema.Struct({
	mediaId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("media_id")),
});

export const ConsentWithdrawn = Schema.Struct({
	withdrawn: Schema.Boolean,
	version: Schema.String,
	effectiveAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("effective_at")),
});

export const ErasureRequested = Schema.Struct({
	requestId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("request_id")),
	state: Schema.String,
	completionBy: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("completion_by")),
	retained: Schema.Array(Schema.String),
});

export const MfaStatus = Schema.Struct({
	enabled: Schema.Boolean,
	steppedUp: Schema.propertySignature(Schema.Boolean).pipe(Schema.fromKey("stepped_up")),
});

export const MfaEnrolment = Schema.Struct({
	secret: Schema.String,
	otpauthUri: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("otpauth_uri")),
});

export const MfaCode = Schema.Struct({ code: Schema.String.pipe(Schema.pattern(/^\d{6}$/)) });

export class AccountGroup extends HttpApiGroup.make("account")
	.add(
		HttpApiEndpoint.get("profile", "/me")
			.addSuccess(Profile)
			.addError(Unauthorized)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.patch("updateProfile", "/me")
			.setPayload(ProfileUpdate)
			.addSuccess(Profile)
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.put("setAvatar", "/me/avatar")
			.setPayload(AvatarChoice)
			.addSuccess(Profile)
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.del("removeAvatar", "/me/avatar")
			.addSuccess(Profile)
			.addError(Unauthorized)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("withdrawConsent", "/me/consent/withdraw")
			.addSuccess(ConsentWithdrawn)
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("requestErasure", "/me/erasure")
			.setPayload(
				Schema.Struct({
					reason: Schema.optionalWith(Schema.String.pipe(Schema.maxLength(1000)), { exact: true }),
				}),
			)
			.addSuccess(ErasureRequested, { status: 202 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("mfaStatus", "/me/mfa")
			.addSuccess(MfaStatus)
			.addError(Unauthorized)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("mfaEnrol", "/me/mfa/totp")
			.addSuccess(MfaEnrolment, { status: 201 })
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("mfaActivate", "/me/mfa/totp/verify")
			.setPayload(MfaCode)
			.addSuccess(Session)
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("stepUp", "/auth/mfa")
			.setPayload(MfaCode)
			.addSuccess(Session)
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	) {}

// ---- Media uploads (docs/media) ---------------------------------------------------

export const UploadRequest = Schema.Struct({
	mimeType: Schema.propertySignature(Schema.String.pipe(Schema.maxLength(100))).pipe(
		Schema.fromKey("mime_type"),
	),
	sizeBytes: Schema.propertySignature(Schema.Int.pipe(Schema.positive())).pipe(Schema.fromKey("size_bytes")),
	altText: Schema.propertySignature(Schema.String.pipe(Schema.maxLength(2000))).pipe(
		Schema.fromKey("alt_text"),
	),
});

export const UploadTicket = Schema.Struct({
	mediaId: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("media_id")),
	kind: Schema.Literal("image", "video"),
	upload: Schema.Struct({
		url: Schema.String,
		method: Schema.Literal("PUT"),
		headers: Schema.Record({ key: Schema.String, value: Schema.String }),
		expiresAt: Schema.propertySignature(Schema.String).pipe(Schema.fromKey("expires_at")),
	}),
});
export type UploadTicket = typeof UploadTicket.Type;

const MediaPath = Schema.Struct({ mediaId: Schema.String });

export class MediaGroup extends HttpApiGroup.make("media")
	.add(
		HttpApiEndpoint.post("createUpload", "/media/uploads")
			.setPayload(UploadRequest)
			.addSuccess(UploadTicket, { status: 201 })
			.addError(Unauthorized)
			.addError(ValidationFailed)
			.addError(RateLimited)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.post("complete", "/media/:mediaId/complete")
			.setPath(MediaPath)
			.addSuccess(Media)
			.addError(Unauthorized)
			.addError(Conflict)
			.addError(ValidationFailed)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	)
	.add(
		HttpApiEndpoint.get("get", "/media/:mediaId")
			.setPath(MediaPath)
			.addSuccess(Media)
			.addError(Unauthorized)
			.addError(BackendUnavailable)
			.addError(UpstreamError),
	) {}

// ---- Contract -----------------------------------------------------------------

export class ApiContract extends HttpApi.make("civic")
	.add(SystemGroup)
	.add(BoundaryGroup)
	.add(AuthGroup)
	.add(PostsGroup)
	.add(ModerationGroup)
	.add(AccountGroup)
	.add(MediaGroup)
	.prefix("/api") {}
