import { createFileRoute } from "@tanstack/react-router";

import { AccountPage } from "@/components/account/AccountPage";
import { describeError, settle } from "@/lib/api-errors";
import { useT } from "@/lib/i18n/I18nProvider";
import { callApiEither } from "@/runtimes/get-runtime";

/** The signed-in user's account and settings (behind the auth guard). */
export const Route = createFileRoute("/_authed/account")({
	loader: async () => ({ profile: settle(await callApiEither((api) => api.account.profile())) }),
	component: Account,
});

function Account() {
	const { profile } = Route.useLoaderData();
	const { t } = useT();
	if (!profile.ok) {
		return (
			<main id="main" className="mx-auto max-w-2xl px-4 py-12">
				<p role="alert" className="rounded-md border border-border p-4 text-body">
					{describeError(profile.error, t)}
				</p>
			</main>
		);
	}
	return <AccountPage profile={profile.value} />;
}
