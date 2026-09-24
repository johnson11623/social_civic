import { describe, expect, it } from "vitest";

import { cn } from "@/lib/cn";
import { formatDate, formatKES, formatNumber, formatRelative } from "@/lib/format";
import { tv } from "@/lib/variants";

// Intl uses non-breaking spaces; compare on normal spaces.
const n = (s: string) => s.replace(/ | /g, " ");

describe("cn (T-W1.2.1.1)", () => {
	it("later conflicting classes win", () => {
		expect(cn("bg-accent px-4", "bg-danger")).toBe("px-4 bg-danger");
		expect(cn("text-small", "text-body")).toBe("text-body");
		expect(cn("text-ink", "text-muted")).toBe("text-muted");
	});

	it("keeps a size token and a colour token together", () => {
		expect(cn("text-body text-ink")).toBe("text-body text-ink");
		expect(cn("text-micro", "text-on-accent")).toBe("text-micro text-on-accent");
	});

	it("handles conditionals", () => {
		expect(cn("p-4", false && "p-8", undefined, ["rounded-md", { "shadow-sm": true }])).toBe(
			"p-4 rounded-md shadow-sm",
		);
	});
});

describe("tv (T-W1.2.1.2)", () => {
	const chip = tv({
		base: "rounded-sm text-small",
		variants: { tone: { ok: "bg-kenya-green text-on-accent", bad: "bg-danger text-on-accent" } },
		defaultVariants: { tone: "ok" },
	});
	it("applies variants and merges overrides with tokens", () => {
		expect(chip()).toBe("rounded-sm text-small bg-kenya-green text-on-accent");
		expect(chip({ tone: "bad", class: "text-body" })).toBe("rounded-sm bg-danger text-on-accent text-body");
	});
});

describe("formatters (T-W1.2.1.3)", () => {
	it("formats KES from minor units", () => {
		expect(n(formatKES(25000, "en"))).toBe("Ksh 250");
		expect(n(formatKES(20000000, "sw"))).toBe("Ksh 200,000");
		expect(n(formatKES(123450, "en"))).toBe("Ksh 1,234.50");
	});

	it("formats numbers with grouping", () => {
		expect(formatNumber(22102532, "sw")).toBe("22,102,532");
	});

	it("formats dates in Kenyan time and the user's language", () => {
		expect(formatDate("2026-03-01T08:15:00Z", "en")).toBe("1 March 2026");
		expect(formatDate("2026-03-01T08:15:00Z", "sw")).toBe("1 Machi 2026");
		// 22:30 UTC on 28 Feb is already 1 March in Nairobi (UTC+3).
		expect(formatDate("2026-02-28T22:30:00Z", "en")).toBe("1 March 2026");
	});

	it("formats relative times", () => {
		const now = new Date("2026-03-01T12:00:00Z");
		expect(formatRelative("2026-03-01T11:48:00Z", "en", now)).toBe("12 minutes ago");
		expect(formatRelative("2026-03-01T11:48:00Z", "sw", now)).toBe("dakika 12 zilizopita");
		expect(formatRelative("2026-02-28T12:00:00Z", "en", now)).toBe("yesterday");
		expect(formatRelative("2026-02-28T12:00:00Z", "sw", now)).toBe("jana");
		expect(formatRelative("2026-03-01T11:59:50Z", "en", now)).toBe("now");
		expect(formatRelative("2026-03-15T12:00:00Z", "en", now)).toBe("in 2 weeks");
	});
});
