import { createRootRoute, HeadContent, Scripts } from "@tanstack/react-router";
import { lazy, type ReactNode, Suspense } from "react";

import appCss from "@/styles/global.css?url";

// Devtools are dev-only: the lazy import keeps them out of the production
// bundle entirely (3G budget: ≤ 150KB initial JS).
const Devtools = import.meta.env.DEV ? lazy(() => import("@/components/dev/Devtools")) : () => null;

export const Route = createRootRoute({
	head: () => ({
		meta: [
			{ charSet: "utf-8" },
			{ name: "viewport", content: "width=device-width, initial-scale=1" },
			{ name: "color-scheme", content: "light dark" },
			{ title: "Civic Platform" },
		],
		links: [
			{ rel: "stylesheet", href: appCss },
			{ rel: "icon", href: "/favicon.svg", type: "image/svg+xml" },
		],
	}),
	shellComponent: RootDocument,
});

function RootDocument({ children }: { children: ReactNode }) {
	return (
		// suppressHydrationWarning: browser extensions (e.g. Grammarly) add
		// attributes to <html>/<body> before React hydrates. It only silences
		// attribute mismatches on these two elements, never their children.
		<html lang="en" suppressHydrationWarning>
			<head>
				<HeadContent />
			</head>
			<body className="bg-paper text-ink" suppressHydrationWarning>
				{children}
				<Suspense>
					<Devtools />
				</Suspense>
				<Scripts />
			</body>
		</html>
	);
}
