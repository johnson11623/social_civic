import { createFileRoute } from "@tanstack/react-router";

import type { Level } from "@/api/api-contract";
import { WardShell } from "@/components/layout/WardShell";
import { ModerationQueue } from "@/components/moderation/ModerationQueue";
import { useT } from "@/lib/i18n/I18nProvider";
import { isLevel } from "@/lib/levels";
import { fetchChannels, fetchQueue, fetchRoles } from "@/lib/loaders";
import { moderatedLevels } from "@/lib/moderation";

type Filter = "open" | "triage" | "all";
type Sort = "age" | "reports";
type Search = { level?: Level; filter?: Filter; sort?: Sort };

/** W2.2 — the moderation queue for the signed-in moderator. */
export const Route = createFileRoute("/_authed/moderation")({
	validateSearch: (s: Record<string, unknown>): Search => ({
		...(isLevel(s.level) ? { level: s.level } : {}),
		...(s.filter === "triage" || s.filter === "all" ? { filter: s.filter } : {}),
		...(s.sort === "reports" ? { sort: s.sort } : {}),
	}),
	loaderDeps: ({ search }) => search,
	loader: async ({ deps }) => {
		const [roles, channels] = await Promise.all([fetchRoles(), fetchChannels()]);
		const held = roles.ok ? moderatedLevels(roles.value.items) : [];
		// Default to the lowest level the moderator governs.
		const level = deps.level ?? held[0] ?? "ward";
		const queue = held.length
			? await fetchQueue({ level, filter: deps.filter ?? "open", sort: deps.sort ?? "age" })
			: undefined;
		return { roles, channels, queue, level };
	},
	component: ModerationPage,
});

function ModerationPage() {
	const { roles, channels, queue, level } = Route.useLoaderData();
	const search = Route.useSearch();
	const navigate = Route.useNavigate();
	const { t } = useT();
	return (
		<main id="main" className="pb-28 md:pb-12">
			<WardShell channels={channels} isModerator>
				<div className="mx-auto max-w-4xl">
					{roles.ok && queue ? (
						<ModerationQueue
							roles={roles.value.items}
							queue={queue}
							level={level}
							filter={search.filter ?? "open"}
							sort={search.sort ?? "age"}
							onChange={(next) => void navigate({ search: (s) => ({ ...s, ...next }), resetScroll: false })}
						/>
					) : (
						<p role="alert" className="my-6 rounded-md border border-border p-4 text-body">
							{t("mod.notModerator")}
						</p>
					)}
				</div>
			</WardShell>
		</main>
	);
}
