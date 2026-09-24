import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

// T-W1.1.3.5 — user-facing text must come from the EN/SW bundles via t().
// Flags JSX text and user-facing attributes (aria-label, title, alt,
// placeholder) that contain words.
const SRC = join(import.meta.dirname, "../../src");
const SKIP = ["components/dev/"];

function tsx(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const path = join(dir, name);
		if (statSync(path).isDirectory()) return tsx(path);
		return name.endsWith(".tsx") ? [path] : [];
	});
}

const WORD = /\p{L}{2,}/u;

function findHardcoded(source: string): string[] {
	const found: string[] = [];
	const code = source.replace(/\{\/\*[\s\S]*?\*\/\}/g, "").replace(/\/\/.*$/gm, "");
	for (const m of code.matchAll(/>([^<>{}]+)</g)) {
		const text = (m[1] ?? "").trim();
		if (text && WORD.test(text) && !/^[\w.]+\s*=>/.test(text)) found.push(text);
	}
	for (const m of code.matchAll(/\b(aria-label|title|alt|placeholder)="([^"]*)"/g)) {
		if (WORD.test(m[2] ?? "")) found.push(`${m[1]}="${m[2]}"`);
	}
	return found;
}

describe("no hardcoded user-facing strings", () => {
	it("detects literals (self-test)", () => {
		expect(findHardcoded(`<p className="x">Hello there</p>`)).toEqual(["Hello there"]);
		expect(findHardcoded(`<button aria-label="Close">x</button>`)).toEqual(['aria-label="Close"']);
		expect(findHardcoded(`<p>{t("a.b")}</p> <span>404 · </span>`)).toEqual([]);
	});

	it("finds none in src", () => {
		const offenders = tsx(SRC)
			.filter((f) => !SKIP.some((s) => relative(SRC, f).startsWith(s)))
			.flatMap((f) => findHardcoded(readFileSync(f, "utf8")).map((s) => `${relative(SRC, f)}: ${s}`));
		expect(offenders).toEqual([]);
	});
});
