import { getCookie, getRequestHeader } from "@tanstack/react-start/server";

import { chooseLang, LANG_COOKIE, type Lang } from "./lang";

/** Language for the current SSR request. Server-only. */
export function resolveRequestLang(): Lang {
	return chooseLang(getCookie(LANG_COOKIE), getRequestHeader("accept-language"));
}
