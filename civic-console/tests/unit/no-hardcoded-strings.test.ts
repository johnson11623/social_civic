import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";

// T-W1.2.2 / T-W1.1.3.5 — user-facing text must come from the EN/SW bundles
// via t(). Parses TSX with the TypeScript compiler and flags JSX text and
// user-facing attributes (aria-label, title, alt, placeholder) with words.
const SRC = join(import.meta.dirname, "../../src");
const SKIP = ["components/dev/"];
const ATTRS = new Set(["aria-label", "title", "alt", "placeholder"]);
const WORD = /\p{L}{2,}/u;

function tsx(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const path = join(dir, name);
		if (statSync(path).isDirectory()) return tsx(path);
		return name.endsWith(".tsx") ? [path] : [];
	});
}

function findHardcoded(source: string): string[] {
	const file = ts.createSourceFile("x.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
	const found: string[] = [];
	const visit = (node: ts.Node) => {
		if (ts.isJsxText(node)) {
			const text = node.getText().trim();
			if (WORD.test(text)) found.push(text);
		} else if (ts.isJsxAttribute(node) && ATTRS.has(node.name.getText())) {
			const init = node.initializer;
			if (init && ts.isStringLiteral(init) && WORD.test(init.text))
				found.push(`${node.name.getText()}="${init.text}"`);
		}
		ts.forEachChild(node, visit);
	};
	visit(file);
	return found;
}

describe("no hardcoded user-facing strings", () => {
	it("detects literals and ignores types and translated text (self-test)", () => {
		expect(findHardcoded(`const a = <p className="x">Hello there</p>;`)).toEqual(["Hello there"]);
		expect(findHardcoded(`const a = <button aria-label="Close">x</button>;`)).toEqual(['aria-label="Close"']);
		expect(findHardcoded(`const a = <p>{t("a.b")}</p>; const b = <span>404 · </span>;`)).toEqual([]);
		expect(findHardcoded(`type P = ComponentProps<"button"> & VariantProps<typeof button>;`)).toEqual([]);
		expect(findHardcoded(`const a = <img alt={t("x")} />;`)).toEqual([]);
	});

	it("finds none in src", () => {
		const offenders = tsx(SRC)
			.filter((f) => !SKIP.some((s) => relative(SRC, f).startsWith(s)))
			.flatMap((f) => findHardcoded(readFileSync(f, "utf8")).map((s) => `${relative(SRC, f)}: ${s}`));
		expect(offenders).toEqual([]);
	});
});
