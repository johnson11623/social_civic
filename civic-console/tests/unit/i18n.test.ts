import { describe, expect, it } from "vitest";

import { chooseLang, parseAcceptLanguage, readCookie } from "@/lib/i18n/lang";
import { bundles, type MessageKey, translate } from "@/lib/i18n/messages";

describe("language selection", () => {
	it("parses Accept-Language with q-values and regional tags", () => {
		const cases: Array<[string | undefined, string | undefined]> = [
			[undefined, undefined],
			["", undefined],
			["sw", "sw"],
			["sw-KE", "sw"],
			["en-GB,en;q=0.9", "en"],
			["fr-FR,en;q=0.8,sw;q=0.5", "en"],
			["fr-FR,sw;q=0.9,en;q=0.8", "sw"],
			["en;q=0,sw;q=0.1", "sw"],
			["fr", undefined],
			["*", undefined],
			["garbage;;;q=banana", undefined],
		];
		for (const [header, want] of cases) expect(parseAcceptLanguage(header), String(header)).toBe(want);
	});

	it("prefers the cookie, then Accept-Language, then Kiswahili", () => {
		expect(chooseLang("en", "sw-KE")).toBe("en");
		expect(chooseLang("xx", "en-US")).toBe("en");
		expect(chooseLang(undefined, "fr")).toBe("sw");
		expect(chooseLang(undefined, undefined)).toBe("sw");
	});

	it("reads cookies", () => {
		expect(readCookie("a=1; lang=sw; b=2", "lang")).toBe("sw");
		expect(readCookie("language=en", "lang")).toBeUndefined();
	});
});

describe("message bundles", () => {
	const keys = Object.keys(bundles.en) as MessageKey[];

	it("Kiswahili has exactly the English keys, all non-empty", () => {
		expect(Object.keys(bundles.sw).sort()).toEqual([...keys].sort());
		for (const lang of ["en", "sw"] as const) {
			for (const k of keys) expect(bundles[lang][k].trim(), `${lang}:${k}`).not.toBe("");
		}
	});

	it("placeholders match between languages", () => {
		const vars = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
		for (const k of keys) expect(vars(bundles.sw[k]), k).toEqual(vars(bundles.en[k]));
	});

	it("interpolates and leaves unknown placeholders visible", () => {
		expect(translate("en", "platform.status", { status: "ok" })).toBe("Platform API: ok");
		expect(translate("sw", "platform.status", { status: "sawa" })).toBe("API ya jukwaa: sawa");
		expect(translate("en", "platform.status")).toBe("Platform API: {status}");
	});
});
