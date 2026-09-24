import { createRootRoute, HeadContent, Scripts } from "@tanstack/react-router";
import { lazy, type ReactNode, Suspense } from "react";

import { SiteHeader } from "@/components/layout/SiteHeader";
import { I18nProvider } from "@/lib/i18n/I18nProvider";
import { translate } from "@/lib/i18n/messages";
import { resolveLang } from "@/lib/i18n/resolve-lang";
import appCss from "@/styles/global.css?url";

// Devtools are dev-only: the lazy import keeps them out of the production
// bundle entirely (3G budget: ≤ 150KB initial JS).
const Devtools = import.meta.env.DEV ? lazy(() => import("@/components/dev/Devtools")) : () => null;

export const Route = createRootRoute({
	// Language for this render: cookie → Accept-Language → Kiswahili.
	beforeLoad: () => ({ lang: resolveLang() }),
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
	const { lang } = Route.useRouteContext();
	return (
		// suppressHydrationWarning: browser extensions (e.g. Grammarly) add
		// attributes to <html>/<body> before React hydrates. It only silences
		// attribute mismatches on these two elements, never their children.
		<html lang={lang} suppressHydrationWarning>
			<head>
				<HeadContent />
			</head>
			<body className="bg-paper text-ink" suppressHydrationWarning>
				<I18nProvider initialLang={lang}>
					<SiteHeader />
					{children}
				</I18nProvider>
				<Suspense>
					<Devtools />
				</Suspense>
				<Scripts />
			</body>
		</html>
	);
}
