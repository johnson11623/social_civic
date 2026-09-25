import { createRootRoute, HeadContent, Scripts } from "@tanstack/react-router";
import { lazy, type ReactNode, Suspense } from "react";

import { SessionKeeper } from "@/components/auth/SessionKeeper";
import { type HeaderUser, SiteHeader } from "@/components/layout/SiteHeader";
import { ToastProvider } from "@/components/ui/Toast";
import { settle } from "@/lib/api-errors";
import { displayAttributes } from "@/lib/display";
import { resolveDisplay } from "@/lib/display-resolve";
import { I18nProvider } from "@/lib/i18n/I18nProvider";
import { translate } from "@/lib/i18n/messages";
import { resolveLang } from "@/lib/i18n/resolve-lang";
import { callApiEither, callApiPromise } from "@/runtimes/get-runtime";
import appCss from "@/styles/global.css?url";

// Devtools are dev-only: the lazy import keeps them out of the production
// bundle entirely (3G budget: ≤ 150KB initial JS).
const Devtools = import.meta.env.DEV ? lazy(() => import("@/components/dev/Devtools")) : () => null;

export const Route = createRootRoute({
	// Language for this render: cookie → Accept-Language → Kiswahili.
	beforeLoad: () => ({ lang: resolveLang(), display: resolveDisplay() }),
	// Who is signed in, for the header avatar. Loaded once; login, logout and
	// profile changes invalidate the router, which reloads it.
	staleTime: Number.POSITIVE_INFINITY,
	loader: async (): Promise<{ user: HeaderUser }> => {
		const session = await callApiPromise((api) => api.auth.session()).catch(() => ({ authenticated: false }));
		if (!session.authenticated) return { user: null };
		const profile = settle(await callApiEither((api) => api.account.profile()));
		return profile.ok
			? { user: { displayName: profile.value.displayName, avatarUrl: profile.value.avatar?.url } }
			: { user: { displayName: "" } };
	},
	head: ({ match }) => ({
		meta: [
			{ charSet: "utf-8" },
			{ name: "viewport", content: "width=device-width, initial-scale=1" },
			{ name: "color-scheme", content: "light dark" },
			{ title: translate(match.context.lang, "app.name") },
		],
		links: [
			{ rel: "stylesheet", href: appCss },
			{ rel: "icon", href: "/favicon.svg", type: "image/svg+xml" },
		],
	}),
	shellComponent: RootDocument,
});

function RootDocument({ children }: { children: ReactNode }) {
	const { lang, display } = Route.useRouteContext();
	const { user } = Route.useLoaderData() ?? { user: null };
	return (
		// suppressHydrationWarning: browser extensions (e.g. Grammarly) add
		// attributes to <html>/<body> before React hydrates. It only silences
		// attribute mismatches on these two elements, never their children.
		<html lang={lang} {...displayAttributes(display)} suppressHydrationWarning>
			<head>
				<HeadContent />
			</head>
			<body className="bg-paper text-ink" suppressHydrationWarning>
				<I18nProvider initialLang={lang}>
					<ToastProvider>
						<SiteHeader user={user} />
						{children}
						<SessionKeeper />
					</ToastProvider>
				</I18nProvider>
				<Suspense>
					<Devtools />
				</Suspense>
				<Scripts />
			</body>
		</html>
	);
}
