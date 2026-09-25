import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Post, QueueItem, RoleAssignment } from "@/api/api-contract";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

// W2.2 and moderation on posts, against a mocked API client.

const api = {
	moderation: { report: vi.fn(), act: vi.fn(), history: vi.fn(), appeal: vi.fn() },
	posts: { replies: vi.fn(), reply: vi.fn(), like: vi.fn(), unlike: vi.fn() },
};
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

const { PostDetail } = await import("@/components/civic/PostDetail");
const { ModerationQueue } = await import("@/components/moderation/ModerationQueue");

const soon = new Date(Date.now() + 5 * 24 * 3600 * 1000).toISOString();
const past = new Date(Date.now() - 24 * 3600 * 1000).toISOString();

const post = (over: Partial<Post> = {}): Post => ({
	postId: "p1",
	channelId: "c1",
	channel: "general",
	level: "ward",
	wardId: 551,
	content: "Water point broken",
	score: 1,
	state: "active",
	author: { publicId: "author-1", displayName: "Brian" },
	counts: { likes: 1, replies: 0 },
	liked: false,
	sponsored: false,
	createdAt: new Date().toISOString(),
	...over,
});
const emptyThread = { ok: true as const, value: { postId: "p1", items: [], hasMore: false } };
const removed = (appealDueAt?: string) =>
	post({
		state: "tombstoned",
		content: null,
		moderation: {
			actionId: "m1",
			action: "hide",
			reasonCode: "hate_speech",
			...(appealDueAt ? { appealDueAt } : {}),
		},
	});

beforeEach(() => {
	for (const g of Object.values(api)) for (const fn of Object.values(g)) fn.mockReset();
	api.moderation.history.mockReturnValue(Effect.succeed({ postId: "p1", items: [] }));
});

describe("reporting a post", () => {
	it("offers only the policy's harms and sends the report", async () => {
		const user = userEvent.setup();
		api.moderation.report.mockReturnValue(
			Effect.succeed({ reportId: "r1", state: "open", queue: "ward_mod" }),
		);
		await renderWithProviders(<PostDetail post={post()} thread={emptyThread} />, { lang: "sw" });
		await user.click(screen.getByRole("button", { name: "Ripoti" }));
		const dialog = await screen.findByRole("dialog", { name: "Ripoti chapisho hili" });
		const select = within(dialog).getByRole("combobox", { name: "Madhara" });
		expect(
			within(select)
				.getAllByRole("option")
				.map((o) => o.textContent),
		).toEqual([
			"Chagua…",
			"Matamshi ya chuki",
			"Uchochezi wa vurugu",
			"Ukiukaji wa faragha",
			"Usalama wa watoto",
		]);

		await user.click(within(dialog).getByRole("button", { name: "Tuma ripoti" }));
		expect(within(dialog).getByRole("alert")).toHaveTextContent("Chagua madhara");
		expect(api.moderation.report).not.toHaveBeenCalled();

		await user.selectOptions(select, "privacy");
		await user.type(within(dialog).getByRole("textbox"), "Posted my phone number");
		await user.click(within(dialog).getByRole("button", { name: "Tuma ripoti" }));
		expect(api.moderation.report).toHaveBeenCalledWith({
			payload: { postId: "p1", reasonCode: "privacy", details: "Posted my phone number" },
		});
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
	});

	it("shows the platform's refusal (already reported)", async () => {
		const user = userEvent.setup();
		api.moderation.report.mockReturnValue(
			Effect.fail({
				_tag: "Conflict",
				code: "already_reported",
				detail: "You've already reported this post.",
			}),
		);
		await renderWithProviders(<PostDetail post={post()} thread={emptyThread} />);
		await user.click(screen.getByRole("button", { name: "Report" }));
		const dialog = await screen.findByRole("dialog");
		await user.selectOptions(within(dialog).getByRole("combobox"), "hate_speech");
		await user.click(within(dialog).getByRole("button", { name: "Send report" }));
		expect(await within(dialog).findByRole("alert")).toHaveTextContent("You've already reported this post.");
	});
});

describe("removed and frozen posts (T-W1.4.3.5)", () => {
	it("says who removed it and why, and lets the author appeal in the window", async () => {
		const user = userEvent.setup();
		api.moderation.appeal.mockReturnValue(
			Effect.succeed({ appealId: "ap1", state: "open", dueAt: "2026-10-08T00:00:00Z" }),
		);
		await renderWithProviders(<PostDetail post={removed(soon)} thread={emptyThread} viewerId="author-1" />);
		expect(screen.getByRole("note")).toHaveTextContent("Removed by the Ward moderator — reason: Hate speech");
		expect(screen.queryByRole("textbox", { name: "Your reply" })).toBeNull();

		await user.click(screen.getByRole("button", { name: "Appeal" }));
		const dialog = await screen.findByRole("dialog", { name: "Appeal this decision" });
		await user.click(within(dialog).getByRole("button", { name: "File appeal" }));
		expect(within(dialog).getByRole("alert")).toHaveTextContent("Explain your appeal");
		await user.type(within(dialog).getByRole("textbox"), "I was quoting the chief's notice.");
		await user.click(within(dialog).getByRole("button", { name: "File appeal" }));
		expect(api.moderation.appeal).toHaveBeenCalledWith({
			payload: { moderationId: "m1", statement: "I was quoting the chief's notice." },
		});
		expect(await screen.findByText(/Appeal filed\. A decision is due by 8 October 2026/)).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Appeal" })).toBeNull();
	});

	it("offers no appeal to others, or after the window", async () => {
		await renderWithProviders(
			<PostDetail post={removed(soon)} thread={emptyThread} viewerId="someone-else" />,
		);
		expect(screen.queryByRole("button", { name: "Appeal" })).toBeNull();
		await renderWithProviders(<PostDetail post={removed(past)} thread={emptyThread} viewerId="author-1" />);
		expect(screen.getByText(/The appeal window closed on/)).toBeInTheDocument();
	});

	it("shows frozen posts with the reason and pauses likes", async () => {
		await renderWithProviders(
			<PostDetail
				post={post({
					state: "frozen",
					moderation: { actionId: "m2", action: "freeze", reasonCode: "incitement", appealDueAt: soon },
				})}
				thread={emptyThread}
			/>,
		);
		expect(screen.getByRole("note")).toHaveTextContent(
			"Under review by moderators — reason: Incitement to violence",
		);
		expect(screen.getByText("Water point broken")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /^Like/ })).toBeDisabled();
	});
});

describe("moderation queue (W2.2.1)", () => {
	const wardMod: RoleAssignment[] = [{ assignmentId: "a1", role: "ward_mod", level: 1, unitCode: 551 }];
	const item = (over: Partial<QueueItem> = {}): QueueItem => ({
		postId: "q1",
		level: "ward",
		wardId: 551,
		content: "Offending text",
		state: "active",
		author: { publicId: "a", displayName: "Amina" },
		reportCount: 3,
		reasons: ["hate_speech", "incitement"],
		firstReportedAt: new Date(Date.now() - 12 * 60 * 1000).toISOString(),
		postedAt: new Date(Date.now() - 3600 * 1000).toISOString(),
		...over,
	});
	const render = async (items: QueueItem[], roles = wardMod) => {
		const onChange = vi.fn();
		const view = await renderWithProviders(
			<ModerationQueue
				roles={roles}
				queue={{ ok: true, value: { items } }}
				level="ward"
				filter="open"
				sort="age"
				onChange={onChange}
			/>,
		);
		return { ...view, onChange };
	};

	it("shows each item's age, reports, reasons and preview, and the moderator's authority", async () => {
		const { container } = await render([item()]);
		expect(screen.getByText("Your authority: Ward")).toBeInTheDocument();
		const table = screen.getByRole("table");
		const [row] = within(table).getAllByRole("row").slice(1);
		expect(row).toHaveTextContent("Offending text");
		expect(row).toHaveTextContent("12 minutes ago");
		expect(row).toHaveTextContent("3");
		expect(row).toHaveTextContent("Hate speech, Incitement to violence");
		// Phones get cards with the same facts.
		const [card] = screen.getAllByRole("listitem");
		expect(card).toHaveTextContent("3 reports");
		await expectNoA11yViolations(container);
	});

	it("changes level, filter and sort (T-W2.2.1.1, T-W2.2.1.2)", async () => {
		const user = userEvent.setup();
		const { onChange } = await render([item()]);
		await user.click(screen.getByRole("tab", { name: "County" }));
		expect(onChange).toHaveBeenCalledWith({ level: "county" });
		await user.click(screen.getByRole("radio", { name: "Frozen" }));
		expect(onChange).toHaveBeenCalledWith({ filter: "triage" });
		await user.selectOptions(screen.getByRole("combobox", { name: "Sort" }), "reports");
		expect(onChange).toHaveBeenCalledWith({ sort: "reports" });
	});

	it("says when there's nothing to review", async () => {
		await render([]);
		expect(screen.getByText("Nothing to review here.")).toBeInTheDocument();
	});
});

describe("reviewing (W2.2.2)", () => {
	const wardMod: RoleAssignment[] = [{ assignmentId: "a1", role: "ward_mod", level: 1, unitCode: 551 }];
	const queueItem = (over: Partial<QueueItem> = {}): QueueItem => ({
		postId: "q1",
		level: "ward",
		wardId: 551,
		content: "Offending text",
		state: "active",
		author: { publicId: "a", displayName: "Amina" },
		reportCount: 2,
		reasons: ["hate_speech"],
		firstReportedAt: new Date().toISOString(),
		postedAt: new Date().toISOString(),
		...over,
	});
	const open = async (items: QueueItem[], roles = wardMod) => {
		const user = userEvent.setup();
		await renderWithProviders(
			<ModerationQueue
				roles={roles}
				queue={{ ok: true, value: { items } }}
				level="ward"
				filter="open"
				sort="age"
				onChange={() => {}}
			/>,
		);
		await user.click(within(screen.getByRole("table")).getByRole("button", { name: "Review" }));
		return { user, dialog: await screen.findByRole("dialog", { name: "Review reported post" }) };
	};

	it("confirms a removal, records it, updates the queue and shows it in history", async () => {
		api.moderation.act.mockReturnValue(Effect.succeed({ actionId: "m1", postState: "tombstoned" }));
		const { user, dialog } = await open([queueItem()]);
		expect(await within(dialog).findByText("No decisions yet.")).toBeInTheDocument();
		// The reason defaults to what was reported; the list is the fixed policy.
		expect(within(dialog).getByRole("combobox", { name: "Reason" })).toHaveValue("hate_speech");

		await user.click(within(dialog).getByRole("radio", { name: "Delete" }));
		await user.click(within(dialog).getByRole("button", { name: "Record decision" }));
		// T-W2.2.2.2 — nothing sent until confirmed.
		expect(within(dialog).getByRole("alertdialog")).toHaveTextContent("Delete this post?");
		expect(api.moderation.act).not.toHaveBeenCalled();

		api.moderation.history.mockReturnValue(
			Effect.succeed({
				postId: "q1",
				items: [
					{
						actionId: "m1",
						action: "delete",
						reasonCode: "hate_speech",
						level: "ward",
						postState: "tombstoned",
						overturned: false,
						createdAt: new Date().toISOString(),
						moderator: { publicId: "me", displayName: "Mod Wanjiru" },
					},
				],
			}),
		);
		await user.click(within(dialog).getByRole("button", { name: "Yes, record it" }));
		expect(api.moderation.act).toHaveBeenCalledWith({
			payload: { postId: "q1", action: "delete", reasonCode: "hate_speech" },
		});
		expect(await within(dialog).findByRole("status")).toHaveTextContent("Post deleted");
		// T-W2.2.2.5 — the decision appears in the history.
		expect(await within(dialog).findByText(/by Mod Wanjiru/)).toBeInTheDocument();
		// T-W2.2.2.3 — and the item leaves the queue.
		await act(async () => {});
		expect(screen.getByText("Nothing to review here.")).toBeInTheDocument();
	});

	it("freezes without confirmation, and offers restore for frozen posts", async () => {
		api.moderation.act.mockReturnValue(Effect.succeed({ actionId: "m2", postState: "frozen" }));
		const { user, dialog } = await open([queueItem()]);
		expect(within(dialog).queryByRole("radio", { name: "Restore" })).toBeNull();
		await user.click(within(dialog).getByRole("radio", { name: "Freeze" }));
		await user.click(within(dialog).getByRole("button", { name: "Record decision" }));
		expect(api.moderation.act).toHaveBeenCalledWith({
			payload: { postId: "q1", action: "freeze", reasonCode: "hate_speech" },
		});
	});

	it("stops an out-of-authority action before sending it (T-W2.2.2.4)", async () => {
		const { user, dialog } = await open([queueItem({ level: "county" })]);
		await user.click(within(dialog).getByRole("radio", { name: "Freeze" }));
		await user.click(within(dialog).getByRole("button", { name: "Record decision" }));
		expect(within(dialog).getByRole("alert")).toHaveTextContent(
			"above your authority: you moderate up to Ward level",
		);
		expect(api.moderation.act).not.toHaveBeenCalled();
	});

	it("shows the platform's refusal", async () => {
		api.moderation.act.mockReturnValue(
			Effect.fail({
				_tag: "UpstreamError",
				status: 403,
				code: "insufficient_authority",
				detail: "You don't have the authority to do that.",
			}),
		);
		const { user, dialog } = await open([queueItem({ state: "frozen" })]);
		expect(within(dialog).getByRole("radio", { name: "Restore" })).toBeInTheDocument();
		await user.click(within(dialog).getByRole("radio", { name: "Restore" }));
		await user.click(within(dialog).getByRole("button", { name: "Record decision" }));
		expect(await within(dialog).findByRole("alert")).toHaveTextContent(
			"You don't have the authority to do that.",
		);
	});
});
