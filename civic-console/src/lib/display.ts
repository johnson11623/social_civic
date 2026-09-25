/**
 * Display and accessibility preferences (T-W4.3.1.6, T-X.9, T-X.10):
 * theme, large text, high contrast and reduced motion. Kept in a plain
 * cookie so the server renders the page with them (no flash), applied as
 * data attributes on <html> that the design tokens respond to.
 */
import { readCookie } from "@/lib/i18n/lang";

export const DISPLAY_COOKIE = "civic_display";
export const DISPLAY_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export type Theme = "system" | "light" | "dark";

export type DisplayPrefs = {
	theme: Theme;
	largeText: boolean;
	highContrast: boolean;
	reducedMotion: boolean;
};

export const DEFAULT_DISPLAY: DisplayPrefs = {
	theme: "system",
	largeText: false,
	highContrast: false,
	reducedMotion: false,
};

/** "theme:dark,text,contrast,motion" ↔ prefs. Unknown parts are ignored. */
export function parseDisplay(raw: string | undefined): DisplayPrefs {
	const prefs = { ...DEFAULT_DISPLAY };
	for (const part of (raw ?? "").split(",")) {
		if (part === "theme:light" || part === "theme:dark") prefs.theme = part.slice(6) as Theme;
		else if (part === "text") prefs.largeText = true;
		else if (part === "contrast") prefs.highContrast = true;
		else if (part === "motion") prefs.reducedMotion = true;
	}
	return prefs;
}

export function serializeDisplay(p: DisplayPrefs): string {
	return [
		p.theme !== "system" && `theme:${p.theme}`,
		p.largeText && "text",
		p.highContrast && "contrast",
		p.reducedMotion && "motion",
	]
		.filter(Boolean)
		.join(",");
}

export const readDisplayCookie = (cookieHeader: string) =>
	parseDisplay(readCookie(cookieHeader, DISPLAY_COOKIE));

/** The <html> attributes for these prefs (absent = follow the OS). */
export function displayAttributes(p: DisplayPrefs): Record<string, string | undefined> {
	return {
		"data-theme": p.theme === "system" ? undefined : p.theme,
		"data-text": p.largeText ? "large" : undefined,
		"data-contrast": p.highContrast ? "high" : undefined,
		"data-motion": p.reducedMotion ? "reduce" : undefined,
	};
}

/** Apply in the browser now, and remember for the next page load. */
export function saveDisplay(p: DisplayPrefs) {
	const root = document.documentElement;
	for (const [name, value] of Object.entries(displayAttributes(p))) {
		if (value === undefined) root.removeAttribute(name);
		else root.setAttribute(name, value);
	}
	// biome-ignore lint/suspicious/noDocumentCookie: plain cookie so SSR renders the same preferences
	document.cookie = `${DISPLAY_COOKIE}=${encodeURIComponent(serializeDisplay(p))}; Path=/; Max-Age=${DISPLAY_COOKIE_MAX_AGE}; SameSite=Lax`;
}
