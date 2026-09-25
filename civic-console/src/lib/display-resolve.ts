import { createIsomorphicFn } from "@tanstack/react-start";

import { type DisplayPrefs, readDisplayCookie } from "./display";
import { resolveRequestDisplay } from "./display-resolve.server";

/** Display preferences for this render: the request cookie on the server, document.cookie in the browser. */
export const resolveDisplay = createIsomorphicFn()
	.server((): DisplayPrefs => resolveRequestDisplay())
	.client((): DisplayPrefs => readDisplayCookie(document.cookie));
