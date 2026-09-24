import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Channel, ChannelList, Post } from "@/api/api-contract";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

// W2.1 — channel sidebar, channel creation and channel view against a
// mocked API client.

const api = {
	posts: {
		createChannel: vi.fn(),
		createPost: vi.fn(),
		channelPosts: vi.fn(),
		like: vi.fn(),
		unlike: vi.fn(),
	},
};
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

const { ChannelSidebar } = await import("@/components/civic/ChannelSidebar");
const { CreateChannelModal, normalizeChannelName } = await import("@/components/civic/CreateChannelModal");
const { ChannelView } = await import("@/components/civic/ChannelView");
const { WardShell } = await import("@/components/layout/WardShell");

const general: Channel = {
	channelId: "c-general",
	wardId: 551,
	name: "general",
	category: "general",
	readOnly: false,
	canPost: true,
};
const water: Channel = {
	channelId: "c-water",
	wardId: 551,
	name: "water-points",
	category: "services",
	readOnly: false,
	canPost: true,
};
const news: Channel = {
	channelId: "c-news",
	wardId: 551,
	name: "announcements",
	category: "general",
	readOnly: true,
	canPost: false,
};

const list: ChannelList = {
	wardId: 551,
	ward: { wardId: 551, name: "Kiamwangi", constituency: "Gatundu South", county: "Kiambu" },
	memberCount: 13808,
	items: [general, water, news],
};

beforeEach(() => {
	for (const fn of Object.values(api.posts)) fn.mockReset();
});

describe("ChannelSidebar (W2.1.1)", () => {
	it("shows the ward header, channels with the current one marked, and the levels", async () => {
		const { container } = await renderWithProviders(
			<ChannelSidebar channels={list} activeChannelId="c-water" onCreate={() => {}} />,
		);
		expect(screen.getByRole("heading", { level: 2, name: "Kiamwangi" })).toBeInTheDocument();
		expect(screen.getByText("Gatundu South · Kiambu · 13,808 members")).toBeInTheDocument();

		const nav = screen.getByRole("navigation", { name: "Channels" });
		const links = within(nav).getAllByRole("link");
		expect(links.map((l) => l.textContent)).toEqual(["#general", "#water-points", "#announcementsRead-only"]);
		expect(within(nav).getByRole("link", { name: "#water-points" })).toHaveAttribute("aria-current", "page");
		expect(within(nav).getByRole("link", { name: "#water-points" })).toHaveAttribute(
			"href",
			"/channels/c-water",
		);

		const levels = screen.getByRole("navigation", { name: "Levels" });
		expect(
			within(levels)
				.getAllByRole("link")
				.map((l) => l.textContent),
		).toEqual(["Ward", "Constituency", "County", "National"]);
		expect(within(levels).getByRole("link", { name: "County" })).toHaveAttribute("href", "/?level=county");
		await expectNoA11yViolations(container);
	});

	it("is keyboard navigable: every channel is reachable with Tab (T-W2.1.1.5)", async () => {
		const user = userEvent.setup();
		await renderWithProviders(<ChannelSidebar channels={list} onCreate={() => {}} />);
		await user.tab();
		expect(screen.getByRole("link", { name: "#general" })).toHaveFocus();
		await user.tab();
		expect(screen.getByRole("link", { name: "#water-points" })).toHaveFocus();
	});

	it("opens as a drawer below the desktop breakpoint (T-W2.1.1.4)", async () => {
		const user = userEvent.setup();
		await renderWithProviders(
			<WardShell channels={{ ok: true, value: list }}>
				<p>content</p>
			</WardShell>,
		);
		await user.click(screen.getByRole("button", { name: "Kiamwangi — Channels" }));
		const drawer = screen.getByRole("dialog", { name: "Channels" });
		await user.click(within(drawer).getByRole("link", { name: "#water-points" }));
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
	});
});

describe("CreateChannelModal (W2.1.2)", () => {
	const open = async () => {
		const onCreated = vi.fn();
		await renderWithProviders(<CreateChannelModal open onClose={() => {}} onCreated={onCreated} />);
		return { onCreated, dialog: screen.getByRole("dialog", { name: "Create a channel" }) };
	};

	it("normalizes typed names to the platform's rule", () => {
		expect(normalizeChannelName("  Water Points ")).toBe("water-points");
	});

	it("rejects an invalid name inline without a request (T-W2.1.2.2)", async () => {
		const user = userEvent.setup();
		const { dialog } = await open();
		await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "maji_safi!");
		await user.click(within(dialog).getByRole("button", { name: "Create channel" }));
		expect(within(dialog).getByRole("alert")).toHaveTextContent(/lowercase letters or numbers/);
		expect(api.posts.createChannel).not.toHaveBeenCalled();
	});

	it("sends name, category, description and read-only (T-W2.1.2.3, T-W2.1.2.4)", async () => {
		const user = userEvent.setup();
		const created = { ...news, channelId: "c-new", name: "mca-updates", canPost: true };
		api.posts.createChannel.mockReturnValue(Effect.succeed(created));
		const { dialog, onCreated } = await open();
		await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "MCA Updates");
		await user.click(within(dialog).getByRole("radio", { name: "Safety" }));
		expect(within(dialog).getByRole("radio", { name: "Safety" })).toBeChecked();
		await user.type(within(dialog).getByRole("textbox", { name: /Description/ }), "Notices from the MCA");
		await user.click(within(dialog).getByRole("radio", { name: "Only me (announcements)" }));
		await user.click(within(dialog).getByRole("button", { name: "Create channel" }));

		expect(api.posts.createChannel).toHaveBeenCalledWith({
			payload: {
				name: "mca-updates",
				category: "safety",
				readOnly: true,
				description: "Notices from the MCA",
			},
		});
		await waitFor(() => expect(onCreated).toHaveBeenCalledWith(created));
	});

	it("shows a taken name on the name field", async () => {
		const user = userEvent.setup();
		api.posts.createChannel.mockReturnValue(
			Effect.fail({ _tag: "Conflict", code: "name_taken", detail: "" }),
		);
		const { dialog } = await open();
		await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "water-points");
		await user.click(within(dialog).getByRole("button", { name: "Create channel" }));
		const name = within(dialog).getByRole("textbox", { name: "Name" });
		await waitFor(() => expect(name).toHaveAccessibleDescription(/already has a channel with that name/));
		expect(name).toHaveAttribute("aria-invalid", "true");
	});

	it("shows a reserved name from the platform", async () => {
		const user = userEvent.setup();
		api.posts.createChannel.mockReturnValue(
			Effect.fail({
				_tag: "ValidationFailed",
				code: "validation_failed",
				detail: "",
				errors: [{ field: "name", code: "reserved" }],
			}),
		);
		const { dialog } = await open();
		await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "admin");
		await user.click(within(dialog).getByRole("button", { name: "Create channel" }));
		expect(
			await within(dialog).findByText("That name is reserved. Please choose another."),
		).toBeInTheDocument();
	});
});

describe("ChannelView (W2.1.3)", () => {
	const post = (content: string): Post => ({
		postId: `p-${content}`,
		channelId: "c-water",
		channel: "water-points",
		level: "ward",
		wardId: 551,
		content,
		score: 1,
		state: "active",
		author: { publicId: "a", displayName: "Amina" },
		counts: { likes: 0, replies: 0 },
		liked: false,
		sponsored: false,
		createdAt: new Date().toISOString(),
	});

	it("shows the channel header and only this channel's posts, with a composer that posts here", async () => {
		const user = userEvent.setup();
		api.posts.createPost.mockReturnValue(Effect.succeed(post("Borehole fixed")));
		await renderWithProviders(
			<ChannelView
				channel={{ ...water, description: "Water points and boreholes", memberCount: 120 }}
				posts={{
					ok: true,
					value: { channelId: "c-water", items: [post("Tap dry since Monday")], hasMore: false },
				}}
			/>,
		);
		expect(screen.getByRole("heading", { level: 1, name: "#water-points" })).toBeInTheDocument();
		expect(screen.getByText("Water points and boreholes")).toBeInTheDocument();
		expect(screen.getByText("120 members")).toBeInTheDocument();
		expect(screen.getByText("Tap dry since Monday")).toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: "Share something with your ward" }));
		const select = screen.getByRole("combobox", { name: "Channel" });
		expect(
			within(select)
				.getAllByRole("option")
				.map((o) => o.textContent),
		).toEqual(["#water-points"]);
		await user.type(screen.getByRole("textbox"), "Borehole fixed");
		await user.click(screen.getByRole("button", { name: "Post" }));
		expect(api.posts.createPost).toHaveBeenCalledWith({
			path: { channelId: "c-water" },
			payload: { content: "Borehole fixed" },
		});
		await waitFor(() => expect(screen.getAllByRole("article")[0]).toHaveTextContent("Borehole fixed"));
	});

	it("hides the composer in a read-only channel (T-W2.1.3.4)", async () => {
		await renderWithProviders(
			<ChannelView
				channel={news}
				posts={{ ok: true, value: { channelId: "c-news", items: [], hasMore: false } }}
			/>,
		);
		expect(screen.queryByRole("button", { name: "Share something with your ward" })).toBeNull();
		expect(screen.queryByRole("button", { name: "New post" })).toBeNull();
		expect(screen.getByText(/only the channel's creator posts here/)).toBeInTheDocument();
		expect(screen.getByText("No posts in #announcements yet.")).toBeInTheDocument();
	});

	it("pages with Load more", async () => {
		const user = userEvent.setup();
		api.posts.channelPosts.mockReturnValue(
			Effect.succeed({ channelId: "c-water", items: [post("Older one")], hasMore: false }),
		);
		await renderWithProviders(
			<ChannelView
				channel={water}
				posts={{
					ok: true,
					value: { channelId: "c-water", items: [post("Newest")], nextCursor: "n1", hasMore: true },
				}}
			/>,
		);
		await user.click(screen.getByRole("button", { name: "Load more" }));
		expect(await screen.findByText("Older one")).toBeInTheDocument();
		expect(api.posts.channelPosts).toHaveBeenCalledWith({
			path: { channelId: "c-water" },
			urlParams: { limit: 20, cursor: "n1" },
		});
	});
});
