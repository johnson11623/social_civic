import { createIsomorphicFn } from "@tanstack/react-start";

import { DEFAULT_LANG, isLang, LANG_COOKIE, type Lang, readCookie } from "./lang";
import { resolveRequestLang } from "./resolve-lang.server";

/**
 * Current language: from the request during SSR; in the browser from the
 * `lang` cookie, else the <html lang> the server rendered.
 */
export const resolveLang = createIsomorphicFn()
	.server((): Lang => resolveRequestLang())
	.client((): Lang => {
		const cookie = readCookie(document.cookie, LANG_COOKIE);
		if (isLang(cookie)) return cookie;
		const html = document.documentElement.lang;
		return isLang(html) ? html : DEFAULT_LANG;
	});
