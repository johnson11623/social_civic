/**
 * Supported languages and how the preferred one is chosen (client-safe).
 * Order of precedence: the `lang` cookie (the user's explicit choice), then
 * Accept-Language, then Kiswahili (platform default, LLD v2.0 §10).
 */
export const LANGS = ["sw", "en"] as const;
export type Lang = (typeof LANGS)[number];

export const DEFAULT_LANG: Lang = "sw";
export const LANG_COOKIE = "lang";
export const LANG_STORAGE_KEY = "civic.lang";
export const LANG_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export function isLang(value: unknown): value is Lang {
	return typeof value === "string" && (LANGS as readonly string[]).includes(value);
}

/** Best supported language from an Accept-Language header (e.g. "sw-KE", "en-GB,en;q=0.9"). */
export function parseAcceptLanguage(header: string | null | undefined): Lang | undefined {
	if (!header) return undefined;
	const ranked = header
		.split(",")
		.map((part, index) => {
			const [tag = "", ...params] = part.trim().split(";");
			const q = params.map((p) => p.trim()).find((p) => p.startsWith("q="));
			const quality = q ? Number(q.slice(2)) : 1;
			return {
				base: tag.toLowerCase().split("-")[0] ?? "",
				quality: Number.isFinite(quality) ? quality : 0,
				index,
			};
		})
		.filter((r) => r.quality > 0)
		.sort((a, b) => b.quality - a.quality || a.index - b.index);
	return ranked.map((r) => r.base).find(isLang);
}

/** Resolve from a cookie value and an Accept-Language header. */
export function chooseLang(
	cookie: string | null | undefined,
	acceptLanguage: string | null | undefined,
): Lang {
	if (isLang(cookie)) return cookie;
	return parseAcceptLanguage(acceptLanguage) ?? DEFAULT_LANG;
}

/** Read a cookie from a Cookie header / document.cookie string. */
export function readCookie(cookieHeader: string, name: string): string | undefined {
	for (const part of cookieHeader.split(";")) {
		const [k, ...v] = part.trim().split("=");
		if (k === name) return decodeURIComponent(v.join("="));
	}
	return undefined;
}
