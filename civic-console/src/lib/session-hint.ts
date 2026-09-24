/**
 * A non-secret cookie ("civic_s=1") set next to the httpOnly session cookies,
 * so code that cannot read those cookies still knows a refreshable session
 * may exist. It never grants anything by itself.
 */
export const SESSION_HINT_COOKIE = "civic_s";

export function hasSessionHint(cookieHeader: string | undefined): boolean {
	return (cookieHeader ?? "").split(";").some((c) => c.trim() === `${SESSION_HINT_COOKIE}=1`);
}
