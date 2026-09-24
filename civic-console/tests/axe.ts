import axe from "axe-core";
import { expect } from "vitest";

/** Fail on any axe-core WCAG 2.1 A/AA violation inside `container`. */
export async function expectNoA11yViolations(container: Element) {
	const { violations } = await axe.run(container, {
		runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"] },
		// jsdom has no layout or CSS: contrast is checked in the browser (Playwright/Lighthouse).
		rules: { "color-contrast": { enabled: false } },
	});
	expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([]);
}
