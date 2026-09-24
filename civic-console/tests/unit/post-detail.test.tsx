import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Post, ThreadPage } from "@/api/api-contract";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

// W1.4.3 — post detail and threaded replies against a mocked API client.

const api = { posts: { reply: vi.fn(), replies: vi.fn(), like: vi.fn(), unlike: vi.fn() } };
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

const { PostDetail } = await import("@/components/civic/PostDetail");
const { PostCard } = await import("@/components/civic/PostCard");

const base = {
	channelId: "c1",
	channel: "general",
	level: "ward",
	wardId: 551,
	score: 3,
	state: "active",
	liked: false,
	sponsored: false,
	createdAt: new Date().toISOString(),
} as const;

const root: Post = {
	...base,
	postId: "root",
	content: "Water point broken at Kiamwangi market",
	author: { publicId: "a1", displayName: "Brian" },
	counts: { likes: 4, replies: 2 },
};
const r1: Post = {
	...base,
	postId: "r1",
	rootId: "root",
	parentId: "root",
	content: "Tuko pamoja. Nitawasiliana na chief.",
	author: { publicId: "a2", displayName: "Amina" },
	counts: { likes: 5, replies: 1 },
};
const r2: Post = {
	...base,
	postId: "r2",
	rootId: "root",
	parentId: "r1",
	content: "Asante Amina",
	author: { publicId: "a1", displayName: "Brian" },
	counts: { likes: 0, replies: 0 },
};

const thread = (items: Post[], more?: string): { ok: true; value: ThreadPage } => ({
	ok: true,
	value: { postId: "root", items, hasMore: more !== undefined, ...(more ? { nextCursor: more } : {}) },
});

beforeEach(() => {
	for (const fn of Object.values(api.posts)) fn.mockReset();
});

describe("post detail (T-W1.4.3.1, T-W1.4.3.2)", () => {
	it("shows the post and its replies nested under what they answer", async () => {
		const { container } = await renderWithProviders(<PostDetail post={root} thread={thread([r1, r2])} />);
		expect(screen.getByText("Water point broken at Kiamwangi market")).toBeInTheDocument();
		const replies = screen.getByRole("region", { name: "Replies" });
		const top = within(replies).getAllByRole("list")[0] as HTMLElement;
		const [first] = within(top).getAllByRole("listitem");
		expect(first).toHaveTextContent("Tuko pamoja");
		// r2 answers r1: it sits in a list inside r1's item.
		expect(within(first as HTMLElement).getByRole("list")).toHaveTextContent("Asante Amina");
		await expectNoA11yViolations(container);
	});

	it("links feed cards to their thread", async () => {
		await renderWithProviders(<PostCard post={root} onLike={() => {}} onWhy={() => {}} linkThread />);
		expect(screen.getByRole("link", { name: "Replies 2" })).toHaveAttribute("href", "/posts/root");
	});

	it("says so when there are no replies, and loads more on demand", async () => {
		const user = userEvent.setup();
		api.posts.replies.mockReturnValue(Effect.succeed(thread([r2]).value));
		await renderWithProviders(<PostDetail post={root} thread={thread([r1], "next-1")} />);
		await user.click(screen.getByRole("button", { name: "Show more replies" }));
		expect(await screen.findByText("Asante Amina")).toBeInTheDocument();
		expect(api.posts.replies).toHaveBeenCalledWith({
			path: { postId: "root" },
			urlParams: { limit: 50, cursor: "next-1" },
		});

		await renderWithProviders(<PostDetail post={{ ...root, postId: "other" }} thread={thread([])} />);
		expect(screen.getByText("No replies yet. Be the first to respond.")).toBeInTheDocument();
	});

	it("shows a removed post without a reply box", async () => {
		await renderWithProviders(
			<PostDetail post={{ ...root, state: "tombstoned", content: null }} thread={thread([])} />,
		);
		expect(screen.getByText("This post was removed by a moderator.")).toBeInTheDocument();
		expect(screen.queryByRole("textbox")).toBeNull();
	});
});

describe("replying (T-W1.4.3.3)", () => {
	it("posts a reply to the post, shown at once and then confirmed", async () => {
		const user = userEvent.setup();
		let resolve: (p: Post) => void = () => {};
		api.posts.reply.mockReturnValue(Effect.promise(() => new Promise<Post>((r) => (resolve = r))));
		await renderWithProviders(<PostDetail post={root} thread={thread([r1])} />);

		const box = screen.getByRole("textbox", { name: "Your reply" });
		await user.type(box, "  I'll call the water office  ");
		await user.click(within(box.closest("form") as HTMLElement).getByRole("button", { name: "Reply" }));
		expect(api.posts.reply).toHaveBeenCalledWith({
			path: { postId: "root" },
			payload: { content: "I'll call the water office" },
		});
		const pending = screen.getByText("I'll call the water office", { selector: "p" }).closest("article");
		expect(pending).toHaveAttribute("aria-busy", "true");
		expect(pending).toHaveTextContent("Posting…");

		await act(async () =>
			resolve({
				...r1,
				postId: "r9",
				parentId: "root",
				content: "I'll call the water office",
				counts: { likes: 0, replies: 0 },
			}),
		);
		await waitFor(() =>
			expect(
				screen.getByText("I'll call the water office", { selector: "p" }).closest("article"),
			).not.toHaveAttribute("aria-busy"),
		);
		// The post's reply count follows.
		expect(screen.getAllByRole("article")[0]).toHaveTextContent("Replies 3");
		expect(screen.getByRole("textbox", { name: "Your reply" })).toHaveValue("");
	});

	it("answers a reply inline, nested under it", async () => {
		const user = userEvent.setup();
		api.posts.reply.mockReturnValue(
			Effect.succeed({ ...r2, postId: "r10", parentId: "r1", content: "Chief amesema kesho" }),
		);
		await renderWithProviders(<PostDetail post={root} thread={thread([r1])} />);
		const amina = screen.getByText(/Tuko pamoja/).closest("li") as HTMLElement;
		await user.click(within(amina).getByRole("button", { name: "Reply", expanded: false }));
		const box = within(amina).getByRole("textbox", { name: "Reply to Amina" });
		expect(box).toHaveFocus();
		await user.type(box, "Chief amesema kesho");
		await user.click(within(amina).getAllByRole("button", { name: "Reply" }).at(-1) as HTMLElement);

		expect(api.posts.reply).toHaveBeenCalledWith({
			path: { postId: "r1" },
			payload: { content: "Chief amesema kesho" },
		});
		const nested = await within(amina).findByRole("list");
		expect(nested).toHaveTextContent("Chief amesema kesho");
		expect(within(amina).queryByRole("textbox")).toBeNull();
	});

	it("takes the reply back and keeps the draft when it fails", async () => {
		const user = userEvent.setup();
		api.posts.reply.mockReturnValue(Effect.fail({ _tag: "RateLimited", detail: "", retryAfter: 60 }));
		await renderWithProviders(<PostDetail post={root} thread={thread([])} />);
		await user.type(screen.getByRole("textbox", { name: "Your reply" }), "Twentieth reply");
		await user.click(screen.getByRole("button", { name: "Reply" }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			/Your reply wasn't posted\. Too many attempts/,
		);
		expect(screen.getByText("No replies yet. Be the first to respond.")).toBeInTheDocument();
		expect(screen.getByRole("textbox", { name: "Your reply" })).toHaveValue("Twentieth reply");
	});

	it("refuses an empty reply without a request", async () => {
		const user = userEvent.setup();
		await renderWithProviders(<PostDetail post={root} thread={thread([])} />);
		await user.click(screen.getByRole("button", { name: "Reply" }));
		expect(screen.getByRole("alert")).toHaveTextContent("Write something before posting.");
		expect(api.posts.reply).not.toHaveBeenCalled();
	});

	it("likes a reply optimistically", async () => {
		const user = userEvent.setup();
		api.posts.like.mockReturnValue(Effect.succeed({ postId: "r1", likes: 6, liked: true }));
		await renderWithProviders(<PostDetail post={root} thread={thread([r1])} />);
		await user.click(screen.getByRole("button", { name: "Like 5" }));
		expect(await screen.findByRole("button", { name: "Like 6", pressed: true })).toBeInTheDocument();
		expect(api.posts.like).toHaveBeenCalledWith({ path: { postId: "r1" } });
	});
});
