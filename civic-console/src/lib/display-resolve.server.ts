import { getCookie } from "@tanstack/react-start/server";

import { DISPLAY_COOKIE, type DisplayPrefs, parseDisplay } from "./display";

/** Display preferences for the current SSR request. Server-only. */
export const resolveRequestDisplay = (): DisplayPrefs => parseDisplay(getCookie(DISPLAY_COOKIE));
