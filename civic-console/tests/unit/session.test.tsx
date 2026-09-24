import { isRedirect } from "@tanstack/react-router";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { REFRESH_LEAD_SECONDS, SessionKeeper } from "@/components/auth/SessionKeeper";
import { safeRedirect } from "@/lib/session";
import { hasSessionHint } from "@/lib/session-hint";

const invalidate = vi.fn(async () => {});
vi.mock("@tanstack/react-router", async (orig) => ({
	...(await orig<typeof import("@tanstack/react-router")>()),
	useRouter: () => ({ invalidate }),
}));

const session = vi.fn();
vi.mock("@/runtimes/get-runtime", () => ({
	callApiPromise: (fn: (api: unknown) => unknown) => fn({ auth: { session: () => session() } }),
}));

describe("safeRedirect (no open redirects)", () => {
	it.each([
		["/account", "/account"],
		["/account?tab=privacy#consent", "/account?tab=privacy#consent"],
		[undefined, "/"],
		["", "/"],
		["https://evil.test", "/"],
		["//evil.test", "/"],
		["/\\evil.test", "/"],
		["javascript:alert(1)", "/"],
		["account", "/"],
	])("%s → %s", (input, want) => expect(safeRedirect(input)).toBe(want));
});

describe("hasSessionHint", () => {
	it("reads the hint cookie exactly", () => {
		expect(hasSessionHint("lang=sw; civic_s=1")).toBe(true);
		expect(hasSessionHint("civic_s=0")).toBe(false);
		expect(hasSessionHint("xcivic_s=1")).toBe(false);
		expect(hasSessionHint(undefined)).toBe(false);
	});
});

describe("SessionKeeper (T-W1.3.2.3)", () => {
	const json = (body: object, status = 200) =>
		Promise.resolve(
			new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } }),
		);
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		vi.useFakeTimers({ now: new Date("2026-03-01T08:00:00Z") });
		fetchMock = vi.fn();
		vi.stubGlobal("fetch", fetchMock);
		invalidate.mockClear();
	});
	afterEach(() => {
		vi.useRealTimers();
		vi.unstubAllGlobals();
		// biome-ignore lint/suspicious/noDocumentCookie: test cleanup
		document.cookie = "civic_s=; Max-Age=0; Path=/";
	});

	const flush = () => act(async () => {});
	const nowS = () => Math.floor(Date.now() / 1000);

	it("makes no requests for anonymous visitors", async () => {
		render(<SessionKeeper />);
		await flush();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it("refreshes a minute before expiry", async () => {
		// biome-ignore lint/suspicious/noDocumentCookie: simulate the BFF's hint cookie
		document.cookie = "civic_s=1; Path=/";
		const exp = nowS() + 900;
		fetchMock.mockImplementation((url: string) =>
			url.endsWith("/session")
				? json({ authenticated: true, expiresAt: exp })
				: json({ authenticated: true, expiresAt: exp + 900, refreshed: true }),
		);
		render(<SessionKeeper />);
		await flush();
		expect(fetchMock).toHaveBeenCalledWith("/api/auth/session", expect.anything());

		await act(async () => vi.advanceTimersByTime((900 - REFRESH_LEAD_SECONDS) * 1000 - 1000));
		expect(fetchMock).toHaveBeenCalledTimes(1);
		await act(async () => vi.advanceTimersByTime(1000));
		expect(fetchMock).toHaveBeenLastCalledWith(
			"/api/auth/refresh",
			expect.objectContaining({ method: "POST" }),
		);
	});

	it("re-renders the page when the session was refreshed after expiry, or lost", async () => {
		// biome-ignore lint/suspicious/noDocumentCookie: simulate the BFF's hint cookie
		document.cookie = "civic_s=1; Path=/";
		fetchMock.mockImplementationOnce(() =>
			json({ authenticated: true, expiresAt: nowS() + 900, refreshed: true }),
		);
		render(<SessionKeeper />);
		await flush();
		expect(invalidate).toHaveBeenCalledTimes(1);

		fetchMock.mockImplementationOnce(() => json({ authenticated: false }));
		Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
		await act(async () => document.dispatchEvent(new Event("visibilitychange")));
		expect(invalidate).toHaveBeenCalledTimes(2);
	});
});

describe("_authed guard (T-W1.3.2.5)", () => {
	it("redirects signed-out users to login with the page to return to", async () => {
		const { Route } = await import("@/routes/_authed");
		session.mockResolvedValue({ authenticated: false });
		const beforeLoad = Route.options.beforeLoad as (ctx: { location: { href: string } }) => Promise<unknown>;
		const thrown = await beforeLoad({ location: { href: "/account?tab=privacy" } }).catch((e: unknown) => e);
		expect(isRedirect(thrown)).toBe(true);
		expect((thrown as { options: { to: string; search: unknown } }).options).toMatchObject({
			to: "/login",
			search: { redirect: "/account?tab=privacy" },
		});
	});

	it("lets signed-in users through with their session", async () => {
		const { Route } = await import("@/routes/_authed");
		session.mockResolvedValue({ authenticated: true, subject: "u" });
		const beforeLoad = Route.options.beforeLoad as (ctx: { location: { href: string } }) => Promise<unknown>;
		await expect(beforeLoad({ location: { href: "/account" } })).resolves.toEqual({
			session: { authenticated: true, subject: "u" },
		});
	});
});
