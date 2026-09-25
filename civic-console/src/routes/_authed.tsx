import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

import { Spinner } from "@/components/ui/Spinner";
import { useT } from "@/lib/i18n/I18nProvider";
import { sessionHintPresent } from "@/lib/session";
import { callApiPromise } from "@/runtimes/get-runtime";

/**
 * T-W1.3.2.5 — layout for pages that need a signed-in user.
 *
 * Signed out → /login?redirect=<this page>. One exception: during SSR the
 * refresh cookie isn't available (it is scoped to /api/auth), so if the
 * session hint says a refreshable session exists, the page renders a
 * "checking" state and the browser's SessionKeeper refreshes and re-runs this
 * guard instead of bouncing the user to login.
 */
export const Route = createFileRoute("/_authed")({
	beforeLoad: async ({ location }) => {
		const session = await callApiPromise((api) => api.auth.session());
		if (session.authenticated) return { session };
		if (typeof window === "undefined" && sessionHintPresent()) return { session: undefined };
		throw redirect({ to: "/login", search: { redirect: location.href } });
	},
	component: Authed,
});

function Authed() {
	const { session } = Route.useRouteContext();
	const { t } = useT();
	if (!session) {
		return (
			<main id="main" className="mx-auto flex max-w-7xl justify-center px-4 py-16">
				<Spinner label={t("auth.checking")} />
			</main>
		);
	}
	return <Outlet />;
}
