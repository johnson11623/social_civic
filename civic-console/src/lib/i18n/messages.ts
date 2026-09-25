/**
 * T-W1.1.3.1 — EN/SW message bundles. English is the source of truth for
 * keys; tests enforce that Kiswahili has every key with the same placeholders.
 * Both bundles are bundled (a few KB) so SSR and the first paint need no fetch.
 */
import en from "@/locales/en.json";
import sw from "@/locales/sw.json";

import type { Lang } from "./lang";

export type MessageKey = keyof typeof en;
export type Messages = Record<MessageKey, string>;

export const bundles: Record<Lang, Messages> = { en, sw };

export type Vars = Readonly<Record<string, string | number>>;

/** Translate `key`, filling `{name}` placeholders from `vars`. */
export function translate(lang: Lang, key: MessageKey, vars?: Vars): string {
	const template = bundles[lang][key] ?? bundles.en[key] ?? key;
	if (!vars) return template;
	return template.replace(/\{(\w+)\}/g, (match, name: string) => (name in vars ? String(vars[name]) : match));
}
