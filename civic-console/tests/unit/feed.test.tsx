import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Channel, FeedPage, Post } from "@/api/api-contract";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

// W1.4.1 / W1.4.2 — the home feed and composer against a mocked typed API
// client (the BFF is covered by tests/integration/posts.test.ts).

const api = {
	posts: { feed: vi.fn(), channels: vi.fn(), createPost: vi.fn(), like: vi.fn(), unlike: vi.fn() },
};

vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

// T-W1.4.1.8 — count card renders through the Avatar every card draws once.
const avatarRenders = vi.hoisted(() => new Map<string, number>());
vi.mock("@/components/ui/Avatar", () => ({
	Avatar: ({ name, src }: { name: string; src?: string }) => {
		avatarRenders.set(name, (avatarRenders.get(name) ?? 0) + 1);
		return src ? <img alt={name} src={src} /> : <span role="img" aria-label={name} />;
	},
}));

const { HomeFeed } = await import("@/components/civic/HomeFeed");
const { LAST_CHANNEL_KEY } = await import("@/components/civic/Composer");

let seq = 0;
function post(over: Partial<Post> = {}): Post {
	seq++;
	return {
		postId: `p${seq}`,
		channelId: "c-general",
		channel: "general",
		level: "ward",
		wardId: 551,
		content: `Post number ${seq}`,
		score: 10,
		state: "active",
		author: { publicId: `a${seq}`, displayName: `Author ${seq}` },
		counts: { likes: 2, replies: 1 },
		liked: false,
		sponsored: false,
		createdAt: new Date().toISOString(),
		...over,
	};
}

const channels: Channel[] = [
	{
		channelId: "c-general",
		wardId: 551,
		name: "general",
		category: "general",
		readOnly: false,
		canPost: true,
	},
	{ channelId: "c-water", wardId: 551, name: "water", category: "services", readOnly: false, canPost: true },
	{
		channelId: "c-news",
		wardId: 551,
		name: "announcements",
		category: "general",
		readOnly: true,
		canPost: false,
	},
];

const page = (items: Post[], more?: string): FeedPage => ({
	items,
	hasMore: more !== undefined,
	...(more ? { nextCursor: more } : {}),
});

async function renderHome(items: Post[], opts: { more?: string; lang?: "en" | "sw" } = {}) {
	const onLevelChange = vi.fn();
	const view = await renderWithProviders(
		<HomeFeed
			level="ward"
			feed={{ ok: true, value: page(items, opts.more) }}
			channels={{ ok: true, value: { wardId: 551, items: channels } }}
			onLevelChange={onLevelChange}
		/>,
		{ lang: opts.lang ?? "en" },
	);
	return { ...view, onLevelChange };
}

beforeEach(() => {
	for (const fn of Object.values(api.posts)) fn.mockReset();
	avatarRenders.clear();
	localStorage.clear();
});

describe("LevelTabs (T-W1.4.1.1)", () => {
	it("switches the feed per level with correct ARIA tabs", async () => {
		const user = userEvent.setup();
		const county = post({ level: "county", content: "County budget hearing" });
		api.posts.feed.mockReturnValue(Effect.succeed(page([county])));
		const { onLevelChange } = await renderHome([post()]);

		const tabs = screen.getAllByRole("tab");
		expect(tabs.map((t) => t.textContent)).toEqual(["Ward", "Constituency", "County", "National"]);
		expect(screen.getByRole("tab", { name: "Ward" })).toHaveAttribute("aria-selected", "true");
		expect(screen.getByRole("tabpanel")).toHaveAttribute(
			"aria-labelledby",
			screen.getByRole("tab", { name: "Ward" }).id,
		);

		await user.click(screen.getByRole("tab", { name: "County" }));
		expect(await screen.findByText("County budget hearing")).toBeInTheDocument();
		expect(api.posts.feed).toHaveBeenCalledWith({ urlParams: { level: "county", limit: 20 } });
		expect(screen.getByRole("tab", { name: "County" })).toHaveAttribute("aria-selected", "true");
		expect(screen.getByRole("tab", { name: "County" })).toHaveAttribute("tabindex", "0");
		expect(screen.getByRole("tab", { name: "Ward" })).toHaveAttribute("tabindex", "-1");
		expect(onLevelChange).toHaveBeenCalledWith("county");

		// Arrow keys move and select (roving focus).
		await user.keyboard("{ArrowRight}");
		expect(screen.getByRole("tab", { name: "National" })).toHaveFocus();
		await waitFor(() =>
			expect(api.posts.feed).toHaveBeenLastCalledWith({ urlParams: { level: "national", limit: 20 } }),
		);
	});

	it("shows card-shaped skeletons while a level loads (T-W1.4.1.7)", async () => {
		const user = userEvent.setup();
		let resolve: (p: FeedPage) => void = () => {};
		api.posts.feed.mockReturnValue(Effect.promise(() => new Promise<FeedPage>((r) => (resolve = r))));
		await renderHome([post()]);
		await user.click(screen.getByRole("tab", { name: "National" }));
		expect(screen.getByRole("status")).toHaveTextContent("Loading posts");
		expect(screen.getByRole("tabpanel").querySelector('[aria-busy="true"]')).not.toBeNull();
		await act(async () => resolve(page([])));
		expect(await screen.findByText(/Nothing has reached this level yet/)).toBeInTheDocument();
	});

	it("offers a retry when a level fails to load", async () => {
		const user = userEvent.setup();
		api.posts.feed.mockReturnValueOnce(Effect.fail({ _tag: "BackendUnavailable", detail: "" }));
		await renderHome([post()]);
		await user.click(screen.getByRole("tab", { name: "County" }));
		const alert = await screen.findByRole("alert");
		expect(alert).toHaveTextContent("We couldn't load the feed.");
		api.posts.feed.mockReturnValueOnce(Effect.succeed(page([post({ content: "Back again" })])));
		await user.click(within(alert).getByRole("button", { name: "Try again" }));
		expect(await screen.findByText("Back again")).toBeInTheDocument();
	});
});

describe("Feed pagination (T-W1.4.1.2)", () => {
	it("loads the next page on demand, without duplicates", async () => {
		const user = userEvent.setup();
		const first = [post(), post()];
		const second = [first[1] as Post, post({ content: "From page two" })];
		api.posts.feed.mockReturnValue(Effect.succeed(page(second)));
		await renderHome(first, { more: "cursor-1" });

		await user.click(screen.getByRole("button", { name: "Load more" }));
		expect(await screen.findByText("From page two")).toBeInTheDocument();
		expect(api.posts.feed).toHaveBeenCalledWith({
			urlParams: { level: "ward", limit: 20, cursor: "cursor-1" },
		});
		expect(screen.getAllByRole("article")).toHaveLength(3);
		expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
		expect(screen.getByText("You're all caught up.")).toBeInTheDocument();
	});
});

describe("loading as you scroll", () => {
	// The sentinel's observer; tests say when the end of the list comes near.
	let near: (() => void) | undefined;
	beforeEach(() => {
		near = undefined;
		vi.stubGlobal(
			"IntersectionObserver",
			class {
				constructor(private cb: IntersectionObserverCallback) {}
				observe(el: Element) {
					near = () =>
						this.cb([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as never);
				}
				disconnect() {}
			},
		);
	});
	afterEach(() => {
		vi.unstubAllGlobals();
		Reflect.deleteProperty(navigator, "connection");
	});

	it("fetches the next page before the reader reaches the end", async () => {
		api.posts.feed.mockReturnValue(Effect.succeed(page([post({ content: "From page two" })])));
		await renderHome([post(), post()], { more: "cursor-1" });
		expect(screen.queryByRole("button", { name: "Load more" })).toBeNull(); // no button to press
		await act(async () => near?.());
		expect(await screen.findByText("From page two")).toBeInTheDocument();
		expect(api.posts.feed).toHaveBeenCalledWith({
			urlParams: { level: "ward", limit: 20, cursor: "cursor-1" },
		});
		expect(screen.getByText("You're all caught up.")).toBeInTheDocument();
	});

	it("asks first when data saver is on", async () => {
		Object.defineProperty(navigator, "connection", { value: { saveData: true }, configurable: true });
		await renderHome([post()], { more: "cursor-1" });
		expect(screen.getByRole("button", { name: "Load more" })).toBeInTheDocument();
		await act(async () => near?.());
		expect(api.posts.feed).not.toHaveBeenCalled();
	});

	it("gives the first photo priority and lazy-loads the rest", async () => {
		const photo = (id: string) => ({
			mediaId: id,
			kind: "image" as const,
			state: "ready" as const,
			altText: id,
			width: 1200,
			height: 800,
			images: [{ name: "medium", width: 1080, height: 720, jpegUrl: `/media/variants/${id}/medium.jpg` }],
		});
		await renderHome([post({ media: photo("first") }), post({ media: photo("second") })]);
		expect(screen.getByRole("img", { name: "first" })).toHaveAttribute("loading", "eager");
		expect(screen.getByRole("img", { name: "first" })).toHaveAttribute("fetchpriority", "high");
		expect(screen.getByRole("img", { name: "second" })).toHaveAttribute("loading", "lazy");
	});
});

describe("PostCard (T-W1.4.1.3–T-W1.4.1.5)", () => {
	it("renders author, channel, content and counts", async () => {
		await renderHome([post({ content: "Water point broken", counts: { likes: 42, replies: 8 } })]);
		const card = screen.getByRole("article");
		expect(within(card).getByText("Water point broken")).toBeInTheDocument();
		expect(within(card).getByText(/#general/)).toBeInTheDocument();
		expect(within(card).getByRole("button", { name: "Like 42", pressed: false })).toBeInTheDocument();
		expect(within(card).getByText("Replies")).toBeInTheDocument();
		expect(within(card).getByRole("img", { name: "Level: Ward" })).toBeInTheDocument();
	});

	it("labels sponsored posts in both languages, whatever the UI language (T-W1.4.1.4)", async () => {
		await renderHome(
			[
				post({
					sponsored: true,
					sponsoredLabel: { en: "Sponsored civic message", sw: "Ujumbe wa kiraia uliofadhiliwa" },
				}),
			],
			{ lang: "sw" },
		);
		const note = screen.getByRole("note", { name: "Limefadhiliwa" });
		expect(note).toHaveTextContent("Sponsored civic message");
		expect(note).toHaveTextContent("Ujumbe wa kiraia uliofadhiliwa");
	});

	it("explains elevated posts in a modal (T-W1.4.1.5)", async () => {
		const user = userEvent.setup();
		await renderHome([post({ level: "constituency", score: 143.7, counts: { likes: 42, replies: 8 } })]);
		expect(screen.getByText("Elevated from Ward to Constituency")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Why am I seeing this?" }));
		const dialog = await screen.findByRole("dialog", { name: "Why you're seeing this" });
		expect(dialog).toHaveTextContent("143.7");
		expect(dialog).toHaveTextContent("42 likes · 8 replies");
		expect(dialog).toHaveTextContent(
			/elevated from Ward to Constituency because its relevance score exceeded/,
		);
	});

	it("explains ward posts too, in Kiswahili", async () => {
		const user = userEvent.setup();
		await renderHome([post()], { lang: "sw" });
		await user.click(screen.getByRole("button", { name: "Kwa nini naona hili?" }));
		expect(await screen.findByRole("dialog")).toHaveTextContent(/Chapisho hili ni la wadi yako/);
	});

	it("keeps a removed post's place with a notice and no content", async () => {
		await renderHome([post({ state: "tombstoned", content: null })]);
		expect(screen.getByRole("article")).toHaveTextContent("This post was removed by a moderator.");
		expect(screen.queryByRole("button", { name: /Like/ })).toBeNull();
	});

	it("has no accessibility violations", async () => {
		const { container } = await renderHome([
			post(),
			post({ level: "national" }),
			post({ sponsored: true, sponsoredLabel: { en: "Sponsored", sw: "Imefadhiliwa" } }),
		]);
		await expectNoA11yViolations(container);
	});
});

describe("Optimistic likes (T-W1.4.1.6)", () => {
	it("flips at once and settles on the server's count", async () => {
		const user = userEvent.setup();
		let resolve: (v: unknown) => void = () => {};
		api.posts.like.mockReturnValue(Effect.promise(() => new Promise((r) => (resolve = r))));
		const p = post({ counts: { likes: 2, replies: 0 } });
		await renderHome([p]);

		await user.click(screen.getByRole("button", { name: "Like 2" }));
		expect(screen.getByRole("button", { name: "Like 3", pressed: true })).toBeInTheDocument();
		expect(api.posts.like).toHaveBeenCalledWith({ path: { postId: p.postId } });
		await act(async () => resolve({ postId: p.postId, likes: 5, liked: true }));
		expect(await screen.findByRole("button", { name: "Like 5", pressed: true })).toBeInTheDocument();
	});

	it("reverts when the server refuses", async () => {
		const user = userEvent.setup();
		api.posts.unlike.mockReturnValue(Effect.fail({ _tag: "BackendUnavailable", detail: "" }));
		await renderHome([post({ liked: true, counts: { likes: 7, replies: 0 } })]);
		await user.click(screen.getByRole("button", { name: "Like 7", pressed: true }));
		expect(await screen.findByRole("button", { name: "Like 7", pressed: true })).toBeInTheDocument();
		expect(api.posts.unlike).toHaveBeenCalledOnce();
	});

	it("re-renders only the card that changed (T-W1.4.1.8)", async () => {
		const user = userEvent.setup();
		api.posts.like.mockReturnValue(Effect.succeed({ postId: "x", likes: 3, liked: true }));
		const a = post({ counts: { likes: 2, replies: 0 } });
		const b = post();
		await renderHome([a, b]);
		const before = {
			a: avatarRenders.get(a.author?.displayName ?? ""),
			b: avatarRenders.get(b.author?.displayName ?? ""),
		};

		await user.click(
			within(screen.getAllByRole("article")[0] as HTMLElement).getByRole("button", { name: "Like 2" }),
		);
		await screen.findByRole("button", { name: "Like 3", pressed: true });
		expect(avatarRenders.get(b.author?.displayName ?? "")).toBe(before.b);
		expect(avatarRenders.get(a.author?.displayName ?? "")).toBeGreaterThan(before.a ?? 0);
	});
});

describe("author photos", () => {
	it("shows the author's profile photo, or initials without one", async () => {
		await renderHome([
			post({ author: { publicId: "a1", displayName: "Amina", avatarUrl: "http://cdn/a1/thumbnail.jpg" } }),
			post({ author: { publicId: "a2", displayName: "Otieno Kamau" } }),
		]);
		expect(screen.getByRole("img", { name: "Amina" })).toHaveAttribute("src", "http://cdn/a1/thumbnail.jpg");
		expect(screen.getByRole("img", { name: "Otieno Kamau" })).not.toHaveAttribute("src");
	});
});

describe("Composer (W1.4.2)", () => {
	it("opens full-screen from the floating button, defaulting to the last used channel", async () => {
		const user = userEvent.setup();
		localStorage.setItem(LAST_CHANNEL_KEY, "c-water");
		await renderHome([]);
		await user.click(screen.getByRole("button", { name: "New post" }));
		const dialog = screen.getByRole("dialog", { name: "Create a post" });
		const select = await within(dialog).findByRole("combobox", { name: "Channel" });
		expect(select).toHaveValue("c-water");
		// Read-only channels are not offered.
		expect(within(select).queryByRole("option", { name: "#announcements" })).toBeNull();
		expect(within(dialog).getByRole("textbox", { name: "What do you want to talk about?" })).toHaveFocus();
	});

	it("defaults to #general and counts characters up to the limit (T-W1.4.2.2, T-W1.4.2.3)", async () => {
		const user = userEvent.setup();
		await renderHome([]);
		await user.click(screen.getByRole("button", { name: "Start a post" }));
		expect(await screen.findByRole("combobox", { name: "Channel" })).toHaveValue("c-general");
		const box = screen.getByRole("textbox", { name: "What do you want to talk about?" });
		await user.type(box, "Habari");
		// The count appears only near the limit.
		expect(screen.queryByText(/left$/)).toBeNull();
		expect(screen.getByRole("button", { name: "Post" })).toBeEnabled();

		await user.clear(box);
		await user.click(box);
		await user.paste("x".repeat(501));
		expect(screen.getByText("-1 left")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Post" })).toBeDisabled();
		expect(api.posts.createPost).not.toHaveBeenCalled();
	});

	it("refuses an empty post without sending anything (T-W1.4.2.6)", async () => {
		const user = userEvent.setup();
		await renderHome([]);
		await user.click(screen.getByRole("button", { name: "Start a post" }));
		await user.type(await screen.findByRole("textbox"), "   ");
		expect(screen.getByRole("button", { name: "Post" })).toBeDisabled();
		await user.keyboard("{Control>}{Enter}{/Control}");
		expect(screen.getByRole("alert")).toHaveTextContent("Write something before posting.");
		expect(api.posts.createPost).not.toHaveBeenCalled();
	});

	it("shows the post at the top straight away, then the server's copy (T-W1.4.2.5)", async () => {
		const user = userEvent.setup();
		let resolve: (p: Post) => void = () => {};
		api.posts.createPost.mockReturnValue(Effect.promise(() => new Promise<Post>((r) => (resolve = r))));
		await renderHome([post({ content: "Older post" })]);

		await user.click(screen.getByRole("button", { name: "Start a post" }));
		await user.selectOptions(await screen.findByRole("combobox", { name: "Channel" }), "c-water");
		await user.type(screen.getByRole("textbox"), "  Borehole fixed today  ");
		await user.click(screen.getByRole("button", { name: "Post" }));

		const [top] = screen.getAllByRole("article");
		expect(top).toHaveTextContent("Borehole fixed today");
		expect(top).toHaveTextContent("Posting…");
		expect(top).toHaveAttribute("aria-busy", "true");
		expect(api.posts.createPost).toHaveBeenCalledWith({
			path: { channelId: "c-water" },
			payload: { content: "Borehole fixed today" },
		});

		await act(async () =>
			resolve(
				post({ postId: "server-1", channelId: "c-water", channel: "water", content: "Borehole fixed today" }),
			),
		);
		await waitFor(() => expect(screen.getAllByRole("article")[0]).not.toHaveTextContent("Posting…"));
		expect(screen.getAllByRole("article")).toHaveLength(2);
		expect(localStorage.getItem(LAST_CHANNEL_KEY)).toBe("c-water");
		expect(screen.queryByRole("textbox")).toBeNull(); // composer closed
	});

	it("takes the post back and keeps the draft when publishing fails", async () => {
		const user = userEvent.setup();
		api.posts.createPost.mockReturnValue(Effect.fail({ _tag: "RateLimited", detail: "", retryAfter: 60 }));
		await renderHome([post({ content: "Older post" })]);
		await user.click(screen.getByRole("button", { name: "Start a post" }));
		await user.type(await screen.findByRole("textbox"), "Eleventh post this minute");
		await user.click(screen.getByRole("button", { name: "Post" }));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			/Your post wasn't published\. Too many attempts/,
		);
		expect(screen.getAllByRole("article")).toHaveLength(1);
		expect(screen.getByRole("textbox")).toHaveValue("Eleventh post this minute");
	});
});
