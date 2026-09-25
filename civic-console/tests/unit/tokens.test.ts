import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// T-W1.1.1.4 — "no arbitrary values unless tokenized" (Atomic Design + Tailwind §9).
const SRC = join(import.meta.dirname, "../../src");

function files(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const path = join(dir, name);
		if (statSync(path).isDirectory()) return files(path);
		return /\.(tsx?|css)$/.test(name) && !name.endsWith(".gen.ts") ? [path] : [];
	});
}

describe("design tokens", () => {
	const sources = files(SRC).filter((f) => f.endsWith(".tsx") || f.endsWith(".ts"));

	it("uses no arbitrary Tailwind values or raw colours in components", () => {
		const offenders: string[] = [];
		for (const file of sources) {
			const text = readFileSync(file, "utf8");
			for (const m of text.matchAll(/className=\{?[`"'][^`"']*[`"']/g)) {
				if (/-\[[^\]]+\]|#[0-9a-fA-F]{3,8}\b/.test(m[0])) offenders.push(`${file}: ${m[0]}`);
			}
		}
		expect(offenders).toEqual([]);
	});

	it("maps every colour token into the Tailwind theme", () => {
		const tokens = readFileSync(join(SRC, "styles/tokens.css"), "utf8");
		const theme = readFileSync(join(SRC, "styles/global.css"), "utf8");
		const names = [...tokens.matchAll(/^\s*--([a-z0-9-]+):\s*#/gim)].map((m) => m[1]);
		expect(names.length).toBeGreaterThan(10);
		for (const name of new Set(names)) {
			expect(theme, `--color-${name} missing from @theme`).toContain(`--color-${name}: var(--${name})`);
		}
	});
});
