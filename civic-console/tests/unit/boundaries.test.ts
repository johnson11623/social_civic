import { describe, expect, it } from "vitest";

import { editDistance, loadBoundaries, normalize, searchWards } from "@/lib/boundaries";

// Ward search in the browser: the same behaviour as the API's (search.go).

describe("boundaries in the browser", () => {
	it("has all of IEBC 2022", async () => {
		const b = await loadBoundaries();
		expect(b.counties).toHaveLength(47);
		expect(b.counties.flatMap((c) => c.children)).toHaveLength(290);
		expect(b.entries).toHaveLength(1450);
	});

	it("normalizes like the API", () => {
		expect(normalize("ZIWA LA NG'OMBE")).toBe("ziwa la ngombe");
		expect(normalize("Mji wa Kale/Makadara")).toBe("mji wa kale makadara");
		expect(editDistance("kiamwngi", "kiamwangi", 1)).toBe(1);
		expect(editDistance("abcd", "abdc", 1)).toBe(1); // transposition
	});

	it("finds wards by prefix, word, context and typo", async () => {
		const b = await loadBoundaries();
		const first = (q: string) => searchWards(b, q)[0]?.label;
		expect(first("mtongwe")).toBe("Mtongwe, Likoni, Mombasa");
		expect(first("kiamwngi")).toBe("Kiamwangi, Gatundu South, Kiambu"); // typo
		expect(first("makadara")).toMatch(/Makadara/); // a later word
		expect(searchWards(b, "township kiharu")[0]?.label).toMatch(/Township, Kiharu/);
		expect(searchWards(b, "k")).toEqual([]);
		expect(searchWards(b, "port", 3)).toHaveLength(3);
	});

	it("searches fast enough for every keystroke", async () => {
		const b = await loadBoundaries();
		const start = performance.now();
		for (const q of ["ki", "kia", "kiam", "kiamw", "kiamwn", "kiamwng", "kiamwngi"]) searchWards(b, q);
		expect(performance.now() - start).toBeLessThan(100);
	});
});
