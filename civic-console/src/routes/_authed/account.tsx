import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";

import { Button } from "@/components/ui/Button";
import { useT } from "@/lib/i18n/I18nProvider";
import { useLogout } from "@/lib/use-logout";

export const Route = createFileRoute("/_authed/account")({ component: Account });

/** Protected page (behind the _authed guard). Profile and privacy settings land here next. */
function Account() {
	const { t } = useT();
	const logout = useLogout();
	const [busy, setBusy] = useState(false);
	return (
		<main id="main" className="mx-auto flex max-w-2xl flex-col gap-4 px-4 py-12">
			<h1 className="text-h1">{t("account.title")}</h1>
			<p className="text-body text-muted">{t("account.body")}</p>
			<Button
				variant="secondary"
				className="w-fit"
				disabled={busy}
				onClick={async () => {
					setBusy(true);
					await logout();
				}}
			>
				{t("landing.logout")}
			</Button>
		</main>
	);
}
