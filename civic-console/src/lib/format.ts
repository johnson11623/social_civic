/**
 * T-W1.2.1.3 — localized formatting (Web App Design §8: localized dates and
 * numbers, KES currency). Money is integer minor units, as in the API.
 */
import type { Lang } from "@/lib/i18n/lang";

const LOCALE: Record<Lang, string> = { en: "en-KE", sw: "sw-KE" };

const cache = new Map<string, Intl.NumberFormat | Intl.DateTimeFormat | Intl.RelativeTimeFormat>();
function memo<T extends Intl.NumberFormat | Intl.DateTimeFormat | Intl.RelativeTimeFormat>(
	key: string,
	make: () => T,
): T {
	let f = cache.get(key) as T | undefined;
	if (!f) {
		f = make();
		cache.set(key, f);
	}
	return f;
}

/** 25000 → "Ksh 250.00". Whole shillings drop the cents: 20000000 → "Ksh 200,000". */
export function formatKES(amountMinor: number, lang: Lang): string {
	const whole = amountMinor % 100 === 0;
	return memo(
		`kes:${lang}:${whole}`,
		() =>
			new Intl.NumberFormat(LOCALE[lang], {
				style: "currency",
				currency: "KES",
				minimumFractionDigits: whole ? 0 : 2,
				maximumFractionDigits: 2,
			}),
	).format(amountMinor / 100);
}

/** Integer with grouping: 22102532 → "22,102,532". */
export function formatNumber(value: number, lang: Lang): string {
	return memo(`num:${lang}`, () => new Intl.NumberFormat(LOCALE[lang])).format(value);
}

/** "1 March 2026" / "1 Machi 2026" (Africa/Nairobi). */
export function formatDate(date: Date | string, lang: Lang): string {
	return memo(
		`date:${lang}`,
		() => new Intl.DateTimeFormat(LOCALE[lang], { dateStyle: "long", timeZone: "Africa/Nairobi" }),
	).format(new Date(date));
}

const UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
	["year", 365 * 24 * 3600],
	["month", 30 * 24 * 3600],
	["week", 7 * 24 * 3600],
	["day", 24 * 3600],
	["hour", 3600],
	["minute", 60],
	["second", 1],
];

/** "12 minutes ago" / "dakika 12 zilizopita", "yesterday" / "jana". */
export function formatRelative(date: Date | string, lang: Lang, now: Date = new Date()): string {
	const seconds = Math.round((new Date(date).getTime() - now.getTime()) / 1000);
	const rtf = memo(`rel:${lang}`, () => new Intl.RelativeTimeFormat(LOCALE[lang], { numeric: "auto" }));
	if (Math.abs(seconds) < 45) return rtf.format(0, "second");
	for (const [unit, size] of UNITS) {
		if (Math.abs(seconds) >= size || unit === "second") return rtf.format(Math.round(seconds / size), unit);
	}
	return rtf.format(0, "second");
}
