import {
	createMemoryHistory,
	createRootRoute,
	createRoute,
	createRouter,
	Outlet,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Effect } from "effect";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { I18nProvider } from "@/lib/i18n/I18nProvider";

// The wizard against a mocked typed API client (the BFF itself is covered by
// tests/integration). callApiPromise runs the callback against `api`.
const api = {
	boundary: {
		tree: vi.fn(),
		search: vi.fn(),
	},
	auth: { register: vi.fn(), requestOtp: vi.fn(), login: vi.fn(), session: vi.fn(), logout: vi.fn() },
};
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) => Effect.runPromise(fn(api)),
	callApiEither: (fn: (a: typeof api) => Effect.Effect<unknown, unknown>) =>
		Effect.runPromise(Effect.either(fn(api))),
}));

const { Route: JoinRoute } = await import("@/routes/join");
const { Route: LoginRoute } = await import("@/routes/login");

const tree = {
	version: "iebc-2022",
	counties: [
		{
			level: "county",
			code: 22,
			iebcCode: "022",
			name: "Kiambu",
			children: [
				{
					level: "constituency",
					code: 111,
					iebcCode: "111",
					name: "Gatundu South",
					children: [
						{ level: "ward", code: 551, iebcCode: "0551", name: "Kiamwangi" },
						{ level: "ward", code: 552, iebcCode: "0552", name: "Kiganjo" },
					],
				},
			],
		},
	],
};

function mount(path: "/join" | "/login", Component: () => React.ReactNode) {
	const root = createRootRoute({
		component: () => (
			<I18nProvider initialLang="en">
				<Outlet />
			</I18nProvider>
		),
	});
	const page = createRoute({ getParentRoute: () => root, path, component: Component });
	const home = createRoute({ getParentRoute: () => root, path: "/", component: () => <p>home</p> });
	const router = createRouter({
		routeTree: root.addChildren([page, home]),
		history: createMemoryHistory({ initialEntries: [path] }),
	});
	render(<RouterProvider router={router} />);
	return router;
}

const fail = (e: object) => Effect.fail(e);

beforeEach(() => {
	for (const group of Object.values(api)) for (const fn of Object.values(group)) fn.mockReset();
	api.boundary.tree.mockReturnValue(Effect.succeed(tree));
	api.boundary.search.mockReturnValue(Effect.succeed({ query: "", items: [] }));
	api.auth.register.mockReturnValue(Effect.succeed({ displayName: "Wanjiku M.", groups: [] }));
	api.auth.requestOtp.mockReturnValue(Effect.succeed({ expiresIn: 300 }));
	api.auth.login.mockReturnValue(Effect.succeed({ authenticated: true, subject: "u" }));
});

const user = () => userEvent.setup();

async function throughProfile(u: ReturnType<typeof user>) {
	// Consent: box starts unticked and blocks progress.
	const box = await screen.findByRole("checkbox");
	expect(box).not.toBeChecked();
	await u.click(screen.getByRole("button", { name: "Continue" }));
	expect(screen.getByRole("alert")).toHaveTextContent("Please tick the box");
	await u.click(box);
	await u.click(screen.getByRole("button", { name: "Continue" }));

	// Identity: ID masked; invalid input stops, valid continues.
	const id = await screen.findByLabelText("National ID number");
	expect(id).toHaveAttribute("type", "password");
	await u.type(id, "1234 5678");
	await u.type(screen.getByLabelText("Mobile number"), "0712 345 678");
	await u.click(screen.getByRole("button", { name: "Continue" }));

	// Ward: cascade County → Constituency → Ward.
	const county = await screen.findByLabelText("County");
	await u.selectOptions(county, "22");
	await u.selectOptions(screen.getByLabelText("Constituency"), "111");
	await u.selectOptions(screen.getByLabelText("Ward"), "551");
	expect(screen.getByText("Selected: Kiamwangi, Gatundu South, Kiambu")).toBeInTheDocument();
	await u.click(screen.getByRole("button", { name: "Continue" }));

	// Profile.
	await u.type(await screen.findByLabelText("Display name"), "Wanjiku M.");
	await u.click(screen.getByRole("radio", { name: "Kiswahili" }));
}

describe("registration wizard (W1.3.1)", () => {
	it("registers, verifies the phone by SMS code and lands home signed in (T-W1.3.1.7)", async () => {
		const router = mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await throughProfile(u);
		await u.click(screen.getByRole("button", { name: "Create account" }));

		await screen.findByRole("heading", { name: "Enter the code we sent" });
		expect(api.auth.register).toHaveBeenCalledWith({
			payload: {
				nationalId: "12345678",
				displayName: "Wanjiku M.",
				preferredLang: "sw",
				phone: "+254712345678",
				wardId: 551,
				consentVersion: "2026-01",
				consentGranted: true,
			},
		});
		expect(api.auth.requestOtp).toHaveBeenCalledWith({ payload: { nationalId: "12345678" } });
		expect(screen.getByText("We sent a 6-digit code by SMS to +254 7•• ••• 678.")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Send a new code" })).toBeDisabled(); // countdown

		await u.type(screen.getByLabelText("6-digit code"), "123456");
		await u.click(screen.getByRole("button", { name: "Verify and continue" }));
		await waitFor(() => expect(router.state.location.pathname).toBe("/"));
		expect(api.auth.login).toHaveBeenCalledWith({ payload: { nationalId: "12345678", otp: "123456" } });
	});

	it("shows a wrong code inline and stays on the step", async () => {
		api.auth.login.mockReturnValue(fail({ _tag: "Unauthorized", code: "invalid_credentials", detail: "" }));
		const router = mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await throughProfile(u);
		await u.click(screen.getByRole("button", { name: "Create account" }));
		await u.type(await screen.findByLabelText("6-digit code"), "000000");
		await u.click(screen.getByRole("button", { name: "Verify and continue" }));
		expect(await screen.findByText(/That code is wrong or has expired/)).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/join");
	});

	it("offers login when the ID is already registered", async () => {
		api.auth.register.mockReturnValue(fail({ _tag: "Conflict", code: "id_already_registered", detail: "" }));
		mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await throughProfile(u);
		await u.click(screen.getByRole("button", { name: "Create account" }));
		expect(await screen.findByText("This national ID is already registered.")).toBeInTheDocument();
		expect(screen.getByRole("link", { name: "Log in instead" })).toHaveAttribute("href", "/login");
		expect(api.auth.requestOtp).not.toHaveBeenCalled();
	});

	it("sends the user back to the ID step when the server rejects the ID", async () => {
		api.auth.register.mockReturnValue(
			fail({ _tag: "InvalidInput", code: "invalid_id", detail: "Nambari si sahihi." }),
		);
		mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await throughProfile(u);
		await u.click(screen.getByRole("button", { name: "Create account" }));
		expect(await screen.findByLabelText("National ID number")).toBeInTheDocument();
		expect(screen.getByText("Nambari si sahihi.")).toBeInTheDocument();
	});

	it("explains rate limits in minutes", async () => {
		api.auth.register.mockReturnValue(fail({ _tag: "RateLimited", detail: "", retryAfter: 3540 }));
		mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await throughProfile(u);
		await u.click(screen.getByRole("button", { name: "Create account" }));
		expect(
			await screen.findByText("Too many attempts. Please wait 59 min and try again."),
		).toBeInTheDocument();
	});

	it("rejects invalid ID and phone before any request", async () => {
		mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await u.click(await screen.findByRole("checkbox"));
		await u.click(screen.getByRole("button", { name: "Continue" }));
		await u.type(await screen.findByLabelText("National ID number"), "123");
		await u.type(screen.getByLabelText("Mobile number"), "020 222 2222");
		await u.click(screen.getByRole("button", { name: "Continue" }));
		expect(screen.getByText("Enter your 8-digit national ID number.")).toBeInTheDocument();
		expect(screen.getByText("Enter a Kenyan mobile number, for example 0712 345 678.")).toBeInTheDocument();
		expect(api.auth.register).not.toHaveBeenCalled();
	});

	it("finds a ward by search and fills the cascade", async () => {
		api.boundary.search.mockReturnValue(
			Effect.succeed({
				query: "kiamwngi",
				items: [
					{
						level: "ward",
						code: 551,
						iebcCode: "0551",
						name: "Kiamwangi",
						label: "Kiamwangi, Gatundu South, Kiambu",
						constituency: { level: "constituency", code: 111, iebcCode: "111", name: "Gatundu South" },
						county: { level: "county", code: 22, iebcCode: "022", name: "Kiambu" },
					},
				],
			}),
		);
		mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await u.click(await screen.findByRole("checkbox"));
		await u.click(screen.getByRole("button", { name: "Continue" }));
		await u.type(await screen.findByLabelText("National ID number"), "12345678");
		await u.type(screen.getByLabelText("Mobile number"), "0712345678");
		await u.click(screen.getByRole("button", { name: "Continue" }));

		await screen.findByLabelText("County");
		await u.type(screen.getByLabelText("Search for your ward"), "kiamwngi");
		await u.click(await screen.findByRole("button", { name: "Kiamwangi, Gatundu South, Kiambu" }));
		expect(screen.getByLabelText("Ward")).toHaveValue("551");
		expect(screen.getByText("Selected: Kiamwangi, Gatundu South, Kiambu")).toBeInTheDocument();
		expect(api.boundary.search).toHaveBeenLastCalledWith({
			urlParams: { q: "kiamwngi", level: "ward", limit: 8 },
		});
	});

	it("explains why we ask in a dialog, with the sign-language slot", async () => {
		mount("/join", JoinRoute.options.component as () => React.ReactNode);
		const u = user();
		await u.click(await screen.findByRole("button", { name: "Why we ask" }));
		const dialog = screen.getByRole("dialog", { name: "Why we ask" });
		expect(within(dialog).getByText("Kenyan Sign Language summary")).toBeInTheDocument();
		await u.click(within(dialog).getByRole("button", { name: "Close" }));
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
	});
});

describe("login (T-W1.3.2.1)", () => {
	it("sends a code to the registered phone and signs in", async () => {
		const router = mount("/login", LoginRoute.options.component as () => React.ReactNode);
		const u = user();
		await u.type(await screen.findByLabelText("National ID number"), "12345678");
		await u.click(screen.getByRole("button", { name: "Send code" }));
		await u.type(await screen.findByLabelText("6-digit code"), "654321");
		await u.click(screen.getByRole("button", { name: "Verify and continue" }));
		await waitFor(() => expect(router.state.location.pathname).toBe("/"));
		expect(api.auth.requestOtp).toHaveBeenCalledWith({ payload: { nationalId: "12345678" } });
		expect(api.auth.login).toHaveBeenCalledWith({ payload: { nationalId: "12345678", otp: "654321" } });
	});
});
