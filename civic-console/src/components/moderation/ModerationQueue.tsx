import { lazy, Suspense, useId, useState } from "react";

import type { Level, QueueItem, QueueParams, RoleAssignment } from "@/api/api-contract";
import { LevelTabs, tabId } from "@/components/civic/LevelTabs";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Select } from "@/components/ui/Select";
import { useToast } from "@/components/ui/Toast";
import { describeError, type Settled } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { formatNumber, formatRelative } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { moderatedLevels } from "@/lib/moderation";

const ReviewModal = lazy(() => import("./ReviewModal").then((m) => ({ default: m.ReviewModal })));

type Filter = NonNullable<QueueParams["filter"]>;
type Sort = NonNullable<QueueParams["sort"]>;

type Props = {
	roles: readonly RoleAssignment[];
	queue: Settled<{ readonly items: readonly QueueItem[] }>;
	level: Level;
	filter: Filter;
	sort: Sort;
	onChange: (next: { level?: Level; filter?: Filter; sort?: Sort }) => void;
};

const FILTERS: readonly Filter[] = ["open", "triage", "all"];

/**
 * W2.2.1 — the moderator's queue: level tabs (T-W2.2.1.1), open/frozen/all
 * and sort (T-W2.2.1.2), each item's age, reports, reasons and preview
 * (T-W2.2.1.3), the moderator's authority (T-W2.2.1.4); a table on
 * desktop, cards on phones (T-W2.2.1.5).
 */
export function ModerationQueue({ roles, queue, level, filter, sort, onChange }: Props) {
	const { t, lang } = useT();
	const toast = useToast();
	const panelId = useId();
	const filterName = useId();
	const [removed, setRemoved] = useState<ReadonlySet<string>>(new Set());
	const [reviewing, setReviewing] = useState<QueueItem | null>(null);
	const levels = moderatedLevels(roles);

	const items = queue.ok ? queue.value.items.filter((i) => !removed.has(i.postId)) : [];
	const reasons = (item: QueueItem) => item.reasons.map((r) => t(`reason.${r}`)).join(", ");
	const preview = (item: QueueItem) => item.content ?? "—";

	return (
		<div className="flex flex-col gap-4 py-6">
			<header className="flex flex-wrap items-center justify-between gap-3">
				<h1 className="text-h1">{t("mod.title")}</h1>
				<Badge variant="ward">
					{t("mod.authority", { levels: levels.map((l) => t(`level.${l}`)).join(" · ") })}
				</Badge>
			</header>

			<LevelTabs value={level} onChange={(l) => onChange({ level: l })} panelId={panelId} />

			<div className="flex flex-wrap items-end gap-4">
				<fieldset className="flex flex-col gap-2">
					<legend className="mb-2 text-small font-medium text-ink">{t("mod.filter")}</legend>
					<div className="flex gap-2">
						{FILTERS.map((f) => (
							<label
								key={f}
								className={cn(
									"inline-flex min-h-11 cursor-pointer items-center rounded-full border px-4 text-small",
									"has-focus-visible:ring-3 has-focus-visible:ring-accent",
									filter === f ? "border-ink bg-surface-2 font-medium" : "border-border",
								)}
							>
								<input
									type="radio"
									name={filterName}
									className="sr-only"
									checked={filter === f}
									onChange={() => onChange({ filter: f })}
								/>
								{t(`mod.filter.${f}`)}
							</label>
						))}
					</div>
				</fieldset>
				<div className="w-56">
					<Select
						label={t("mod.sort")}
						value={sort}
						onChange={(e) => onChange({ sort: e.target.value as Sort })}
						options={[
							{ value: "age", label: t("mod.sort.age") },
							{ value: "reports", label: t("mod.sort.reports") },
						]}
					/>
				</div>
			</div>

			<section id={panelId} role="tabpanel" aria-labelledby={tabId(level)}>
				{!queue.ok ? (
					<p role="alert" className="text-small text-danger">
						{describeError(queue.error, t)}
					</p>
				) : items.length === 0 ? (
					<p className="rounded-md border border-dashed border-border p-6 text-body text-muted">
						{t("mod.empty")}
					</p>
				) : (
					<>
						<table className="hidden w-full border-collapse text-left text-small md:table">
							<caption className="sr-only">{t("mod.title")}</caption>
							<thead className="border-b border-border text-muted">
								<tr>
									<th scope="col" className="py-2 pr-3 font-medium">
										{t("mod.col.post")}
									</th>
									<th scope="col" className="py-2 pr-3 font-medium">
										{t("mod.col.age")}
									</th>
									<th scope="col" className="py-2 pr-3 font-medium">
										{t("mod.col.reports")}
									</th>
									<th scope="col" className="py-2 pr-3 font-medium">
										{t("mod.col.reasons")}
									</th>
									<th scope="col" className="py-2 font-medium">
										<span className="sr-only">{t("mod.col.action")}</span>
									</th>
								</tr>
							</thead>
							<tbody>
								{items.map((item) => (
									<tr key={item.postId} className="border-b border-border align-top">
										<td className="max-w-md py-3 pr-3">
											<p className="line-clamp-2 text-body text-ink">{preview(item)}</p>
											<p className="text-micro text-muted">
												{t("mod.by", { name: item.author.displayName })}
											</p>
										</td>
										<td className="py-3 pr-3 whitespace-nowrap">
											<time dateTime={item.firstReportedAt}>
												{formatRelative(item.firstReportedAt, lang)}
											</time>
										</td>
										<td className="py-3 pr-3 tabular-nums">{formatNumber(item.reportCount, lang)}</td>
										<td className="py-3 pr-3">{reasons(item)}</td>
										<td className="py-3 text-right">
											<Button size="sm" onClick={() => setReviewing(item)}>
												{t("mod.review")}
											</Button>
										</td>
									</tr>
								))}
							</tbody>
						</table>
						<ul className="flex flex-col gap-3 md:hidden">
							{items.map((item) => (
								<li key={item.postId} className="flex flex-col gap-2 rounded-md border border-border p-4">
									<p className="text-small text-muted">
										<time dateTime={item.firstReportedAt}>{formatRelative(item.firstReportedAt, lang)}</time>
										{" · "}
										{t("mod.reports", { count: formatNumber(item.reportCount, lang) })}
									</p>
									<p className="text-body font-medium text-ink">{reasons(item)}</p>
									<p className="line-clamp-3 text-small text-ink">{preview(item)}</p>
									<Button className="self-start" onClick={() => setReviewing(item)}>
										{t("mod.review")}
									</Button>
								</li>
							))}
						</ul>
					</>
				)}
			</section>

			{reviewing && (
				<Suspense>
					<ReviewModal
						item={reviewing}
						roles={roles}
						onClose={() => setReviewing(null)}
						onDone={(item, action) => {
							// T-W2.2.2.3 — recorded: toast, and the item leaves the queue.
							setRemoved((ids) => new Set(ids).add(item.postId));
							toast(t(`action.done.${action}`));
						}}
					/>
				</Suspense>
			)}
		</div>
	);
}
