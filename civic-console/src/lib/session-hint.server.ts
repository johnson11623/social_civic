import { getCookie } from "@tanstack/react-start/server";

import { SESSION_HINT_COOKIE } from "./session-hint";

/** Whether the current SSR request carries the session hint. Server-only. */
export const requestHasSessionHint = () => getCookie(SESSION_HINT_COOKIE) === "1";
