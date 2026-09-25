/**
 * T-X.2 — CSRF protection for cookie-authenticated /api requests: any
 * state-changing request must come from this site (Origin, or Sec-Fetch-Site
 * when a browser omits Origin). Safe methods pass through.
 */
const SAFE = new Set(["GET", "HEAD", "OPTIONS"]);

export function sameOrigin(request: Request): boolean {
	if (SAFE.has(request.method)) return true;
	const host = request.headers.get("x-forwarded-host") ?? request.headers.get("host");
	const origin = request.headers.get("origin");
	if (origin) {
		try {
			return new URL(origin).host === host;
		} catch {
			return false;
		}
	}
	return request.headers.get("sec-fetch-site") === "same-origin";
}

export function withCsrfProtection(
	handler: (args: { request: Request }) => Promise<Response>,
): (args: { request: Request }) => Promise<Response> {
	return async (args) => {
		if (!sameOrigin(args.request)) {
			return Response.json(
				{ _tag: "Forbidden", code: "cross_origin_request", detail: "Cross-site request refused." },
				{ status: 403 },
			);
		}
		return handler(args);
	};
}
