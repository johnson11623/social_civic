/**
 * IEBC 2022 boundaries in the browser, for finding a ward during sign-up.
 *
 * The data (src/data/iebc-2022.json, generated from db/seed by
 * `pnpm gen:boundaries`) is a separate content-hashed chunk: ~15 KB gzipped,
 * fetched once when the join page opens and cached for good. Search runs
 * here, on every keystroke, with no request: the same scoring as the API's
 * (internal/boundary/search.go), for wards.
 */

export type Area = { code: number; name: string };
export type WardNode = Area & { voters: number };
export type ConstituencyNode = Area & { children: WardNode[] };
export type CountyNode = Area & { children: ConstituencyNode[] };

export type WardHit = {
	code: number;
	name: string;
	label: string; // "Kiamwangi, Gatundu South, Kiambu"
	constituency: Area;
	county: Area;
};

type Raw = {
	version: string;
	counties: [number, string][];
	constituencies: [number, string, number][];
	wards: [number, string, number, number][];
};

type Entry = {
	hit: WardHit;
	voters: number;
	name: string;
	spaced: string;
	compact: string;
	tokens: string[];
	ctxTokens: string[];
};

export type Boundaries = { version: string; counties: CountyNode[]; entries: Entry[] };

/** Lowercase, drop apostrophes, everything else non-alphanumeric → one space. */
export function normalize(s: string): string {
	return s
		.toLowerCase()
		.replace(/['’`]/g, "")
		.replace(/[^a-z0-9]+/g, " ")
		.trim();
}

function build(raw: Raw): Boundaries {
	const counties = new Map<number, CountyNode>();
	for (const [code, name] of raw.counties) counties.set(code, { code, name, children: [] });
	const constituencies = new Map<number, ConstituencyNode & { county: CountyNode }>();
	for (const [code, name, countyCode] of raw.constituencies) {
		const county = counties.get(countyCode);
		if (!county) continue;
		const node = { code, name, children: [] as WardNode[] };
		county.children.push(node);
		constituencies.set(code, { ...node, county, children: node.children });
	}
	const entries: Entry[] = [];
	for (const [code, name, constituencyCode, voters] of raw.wards) {
		const c = constituencies.get(constituencyCode);
		if (!c) continue;
		c.children.push({ code, name, voters });
		const n = normalize(name);
		entries.push({
			hit: {
				code,
				name,
				label: `${name}, ${c.name}, ${c.county.name}`,
				constituency: { code: c.code, name: c.name },
				county: { code: c.county.code, name: c.county.name },
			},
			voters,
			name: n,
			spaced: ` ${n}`,
			compact: n.replaceAll(" ", ""),
			tokens: n.split(" "),
			ctxTokens: `${normalize(c.name)} ${normalize(c.county.name)}`.split(" "),
		});
	}
	return { version: raw.version, counties: [...counties.values()], entries };
}

let loading: Promise<Boundaries> | undefined;

/** The boundaries, loaded once per page (call early to prefetch). */
export function loadBoundaries(): Promise<Boundaries> {
	loading ??= import("@/data/iebc-2022.json")
		.then((m) => build((m.default ?? m) as Raw))
		.catch((e) => {
			loading = undefined; // let a retry fetch again
			throw e;
		});
	return loading;
}

// Scores as in internal/boundary/search.go.
const EXACT = 100;
const PREFIX = 90;
const COMPACT_PREFIX = 85; // "homabay" → Homa Bay
const WORD_PREFIX = 80; // "makadara" → Mji wa Kale/Makadara
const ALL_WORDS = 75;
const WITH_CONTEXT = 65; // "township kiharu" → Township ward in Kiharu
const SUBSTRING = 60;
const FUZZY = 50; // minus 10 per edit

const anyPrefix = (tokens: string[], p: string) => tokens.some((t) => t.startsWith(p));

function score(e: Entry, q: string, qSpaced: string, qCompact: string, qTokens: string[]): number {
	let best = 0;
	if (e.name === q) best = EXACT;
	else if (e.name.startsWith(q)) best = PREFIX;
	else if (qCompact.length >= 3 && e.compact.startsWith(qCompact)) best = COMPACT_PREFIX;
	else if (e.spaced.includes(qSpaced)) best = WORD_PREFIX;
	else if (q.length >= 3 && e.name.includes(q)) best = SUBSTRING;
	if (best >= ALL_WORDS) return best;

	if (qTokens.length > 1) {
		let own = 0;
		let all = true;
		for (const t of qTokens) {
			if (anyPrefix(e.tokens, t)) own++;
			else if (!anyPrefix(e.ctxTokens, t)) all = false;
		}
		if (all && own === qTokens.length) best = Math.max(best, ALL_WORDS);
		else if (all && own > 0) best = Math.max(best, WITH_CONTEXT);
	}

	if (best === 0 && q.length >= 4 && qTokens.length === 1) {
		const allowed = q.length >= 8 ? 2 : 1;
		for (const tok of e.tokens) {
			let d = editDistance(q, tok, allowed);
			if (tok.length > q.length) d = Math.min(d, editDistance(q, tok.slice(0, q.length), allowed));
			if (d <= allowed) best = Math.max(best, FUZZY - 10 * d);
		}
	}
	return best;
}

/** Optimal string alignment distance, or max+1 once it's exceeded. */
export function editDistance(a: string, b: string, max: number): number {
	if (Math.abs(a.length - b.length) > max) return max + 1;
	let prev2: number[] = [];
	let prev = Array.from({ length: b.length + 1 }, (_, j) => j);
	for (let i = 1; i <= a.length; i++) {
		const cur = [i];
		let rowMin = i;
		for (let j = 1; j <= b.length; j++) {
			const cost = a[i - 1] === b[j - 1] ? 0 : 1;
			let v = Math.min((prev[j] ?? 0) + 1, (cur[j - 1] ?? 0) + 1, (prev[j - 1] ?? 0) + cost);
			if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) {
				v = Math.min(v, (prev2[j - 2] ?? 0) + 1);
			}
			cur[j] = v;
			rowMin = Math.min(rowMin, v);
		}
		if (rowMin > max) return max + 1;
		prev2 = prev;
		prev = cur;
	}
	return Math.min(prev[b.length] ?? max + 1, max + 1);
}

/** Wards matching q, best first (ties: more registered voters, then name). */
export function searchWards(b: Boundaries, query: string, limit = 8): WardHit[] {
	const q = normalize(query);
	if (q.length < 2) return [];
	const qTokens = q.split(" ");
	const scored: { e: Entry; s: number }[] = [];
	for (const e of b.entries) {
		const s = score(e, q, ` ${q}`, q.replaceAll(" ", ""), qTokens);
		if (s > 0) scored.push({ e, s });
	}
	scored.sort((x, y) => y.s - x.s || y.e.voters - x.e.voters || x.e.hit.name.localeCompare(y.e.hit.name));
	return scored.slice(0, limit).map((x) => x.e.hit);
}
