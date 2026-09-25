import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { ChannelList } from "@/api/api-contract";
import ContextRail from "@/components/layout/ContextRail";
import { WardShell } from "@/components/layout/WardShell";
import { expectNoA11yViolations } from "../axe";
import { renderWithProviders } from "../render";

const channels: ChannelList = {
	wardId: 551,
	ward: { wardId: 551, name: "Kiamwangi", constituency: "Gatundu South", county: "Kiambu" },
	memberCount: 13808,
	items: [
		{ channelId: "c1", wardId: 551, name: "general", category: "general", readOnly: false, canPost: true },
	],
};

function mockScreen(wide: boolean) {
	vi.stubGlobal(
		"matchMedia",
		vi.fn().mockImplementation((query: string) => ({
			matches: wide,
			media: query,
			addEventListener: vi.fn(),
			removeEventListener: vi.fn(),
		})),
	);
	vi.stubGlobal("requestIdleCallback", (cb: () => void) => window.setTimeout(cb, 0));
	vi.stubGlobal("cancelIdleCallback", (id: number) => window.clearTimeout(id));
}

afterEach(() => vi.unstubAllGlobals());

describe("right-hand column", () => {
	it("labels the ad slot Sponsored in both languages and adds area context", async () => {
		const { container } = await renderWithProviders(<ContextRail channels={channels} />, { lang: "sw" });
		const sponsored = screen.getByRole("region", { name: "Sponsored · Limefadhiliwa" });
		expect(sponsored).toHaveTextContent("Wafikie wakazi wa kaunti yako");
		expect(screen.getByRole("region", { name: "Wadi yako kwa ufupi" })).toHaveTextContent("Kiamwangi");
		expect(screen.getByText("Wanachama 13,808")).toBeInTheDocument();
		expect(screen.getByRole("region", { name: "Jinsi machapisho yanavyopanda" })).toBeInTheDocument();
		await expectNoA11yViolations(container);
	});

	it("loads on wide screens once the browser is idle", async () => {
		mockScreen(true);
		await renderWithProviders(
			<WardShell channels={{ ok: true, value: channels }}>
				<p>feed</p>
			</WardShell>,
		);
		const rail = screen.getByRole("complementary", { name: "More from your area" });
		await waitFor(() => expect(rail).toHaveTextContent("Reach your county"));
	});

	it("never loads on phones and tablets", async () => {
		mockScreen(false);
		await renderWithProviders(
			<WardShell channels={{ ok: true, value: channels }}>
				<p>feed</p>
			</WardShell>,
		);
		await new Promise((r) => setTimeout(r, 20));
		expect(screen.getByRole("complementary", { name: "More from your area" })).toBeEmptyDOMElement();
	});
});
