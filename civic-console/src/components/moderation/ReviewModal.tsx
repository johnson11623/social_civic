import { type FormEvent, useEffect, useId, useState } from "react";

import type {
	HistoryEntry,
	ModerationActionKind,
	QueueItem,
	ReasonCode,
	RoleAssignment,
} from "@/api/api-contract";
import { ReasonSelect } from "@/components/civic/ReasonSelect";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { Spinner } from "@/components/ui/Spinner";
import { describeError, settle } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { formatDate, formatNumber } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { topLevel, withinAuthority } from "@/lib/moderation";
import { callApiEither } from "@/runtimes/get-runtime";

type Props = {
	item: QueueItem;
	roles: readonly RoleAssignment[];
	onClose: () => void;
	onDone: (item: QueueItem, action: ModerationActionKind) => void;
};

/** Removals are confirmed first (T-W2.2.2.2); freezing and restoring are reversible. */
const DESTRUCTIVE: ReadonlySet<ModerationActionKind> = new Set(["hide", "delete"]);

/**
 * W2.2.2 — review a reported post: what was reported and why, the
 * moderator's authority, a decision with a harm-based reason, and the
 * post's decision history (T-W2.2.2.5).
 */
export function ReviewModal({ item, roles, onClose, onDone }: Props) {
	const { t, lang } = useT();
	const ids = { action: useId(), notes: useId() };
	const actions: ModerationActionKind[] =
		item.state === "frozen" ? ["hide", "delete", "restore"] : ["hide", "delete", "freeze"];
	const [action, setAction] = useState<ModerationActionKind>(actions[0] as ModerationActionKind);
	const [reason, setReason] = useState<ReasonCode | "">(item.reasons[0] ?? "");
	const [notes, setNotes] = useState("");
	const [confirming, setConfirming] = useState(false);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string>();
	const [done, setDone] = useState(false);
	const [history, setHistory] = useState<readonly HistoryEntry[] | null>(null);
	const top = topLevel(roles);

	const loadHistory = async () => {
		const res = settle(
			await callApiEither((api) => api.moderation.history({ path: { postId: item.postId } })),
		);
		setHistory(res.ok ? res.value.items : []);
	};
	// biome-ignore lint/correctness/useExhaustiveDependencies: load once per reviewed post
	useEffect(() => {
		void loadHistory();
	}, [item.postId]);

	async function record() {
		setBusy(true);
		setError(undefined);
		const res = settle(
			await callApiEither((api) =>
				api.moderation.act({
					payload: {
						postId: item.postId,
						action,
						reasonCode: reason as ReasonCode,
						...(notes.trim() ? { notes: notes.trim() } : {}),
					},
				}),
			),
		);
		setBusy(false);
		setConfirming(false);
		if (!res.ok) return setError(describeError(res.error, t));
		setDone(true);
		onDone(item, action);
		void loadHistory();
	}

	function submit(e: FormEvent) {
		e.preventDefault();
		if (!reason) return setError(t("reason.required"));
		// T-W2.2.2.4 — out of authority: say so, don't send.
		if (!withinAuthority(roles, item.level)) {
			return setError(t("action.outOfAuthority", { level: top ? t(`level.${top}`) : "—" }));
		}
		if (DESTRUCTIVE.has(action)) return setConfirming(true);
		void record();
	}

	return (
		<Modal open onClose={onClose} title={t("mod.reviewTitle")} fullScreenOnMobile>
			<article className="flex flex-col gap-2 rounded-md border border-border bg-surface p-3">
				<div className="flex flex-wrap items-center gap-2 text-small text-muted">
					<span className="font-medium text-ink">{item.author.displayName}</span>
					<Badge variant={item.level}>{t(`level.${item.level}`)}</Badge>
					{item.state === "frozen" && <Badge variant="danger">{t("moderation.frozenBadge")}</Badge>}
				</div>
				<p className="whitespace-pre-wrap break-words text-body text-ink">{item.content ?? "—"}</p>
				<p className="text-small text-muted">
					{t("mod.reports", { count: formatNumber(item.reportCount, lang) })} ·{" "}
					{item.reasons.map((r) => t(`reason.${r}`)).join(", ")}
				</p>
			</article>

			{top && <p className="text-small text-muted">{t("mod.authority", { levels: t(`level.${top}`) })}</p>}

			{done ? (
				<p role="status" className="rounded-md bg-elevated p-3 text-small text-ink">
					{t(`action.done.${action}`)}
				</p>
			) : confirming ? (
				<div role="alertdialog" aria-labelledby={`${ids.action}-confirm`} className="flex flex-col gap-3">
					<p id={`${ids.action}-confirm`} className="text-body">
						{t("action.confirm", { action: t(`action.${action}`) })}
					</p>
					<div className="flex justify-end gap-3">
						<Button variant="ghost" onClick={() => setConfirming(false)} disabled={busy}>
							{t("action.back")}
						</Button>
						<Button variant="danger" onClick={() => void record()} disabled={busy}>
							{t("action.confirmYes")}
						</Button>
					</div>
				</div>
			) : (
				<form onSubmit={submit} noValidate className="flex flex-col gap-4">
					<fieldset className="flex flex-col gap-2">
						<legend className="mb-2 text-small font-medium text-ink">{t("action.label")}</legend>
						<div className="flex flex-wrap gap-2">
							{actions.map((a) => (
								<label
									key={a}
									className={cn(
										"inline-flex min-h-11 cursor-pointer items-center rounded-full border px-4 text-small",
										"has-focus-visible:ring-3 has-focus-visible:ring-accent",
										action === a ? "border-ink bg-surface-2 font-medium" : "border-border",
									)}
								>
									<input
										type="radio"
										name={ids.action}
										className="sr-only"
										checked={action === a}
										onChange={() => setAction(a)}
									/>
									{t(`action.${a}`)}
								</label>
							))}
						</div>
					</fieldset>
					<ReasonSelect label={t("action.reason")} value={reason} onChange={setReason} />
					<div className="flex flex-col gap-2">
						<label htmlFor={ids.notes} className="text-small font-medium text-ink">
							{t("action.notes")} <span className="text-muted">({t("common.optional")})</span>
						</label>
						<textarea
							id={ids.notes}
							rows={2}
							value={notes}
							onChange={(e) => setNotes(e.target.value)}
							className="w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
						/>
					</div>
					{error && (
						<p role="alert" className="text-small text-danger">
							{error}
						</p>
					)}
					<div className="flex justify-end gap-3">
						<Button variant="ghost" onClick={onClose} disabled={busy}>
							{t("composer.cancel")}
						</Button>
						<Button type="submit" disabled={busy}>
							{t("action.submit")}
						</Button>
					</div>
				</form>
			)}

			<section
				aria-labelledby={`${ids.notes}-history`}
				className="flex flex-col gap-2 border-t border-border pt-3"
			>
				<h3 id={`${ids.notes}-history`} className="text-small font-medium text-ink">
					{t("history.title")}
				</h3>
				{history === null ? (
					<Spinner />
				) : history.length === 0 ? (
					<p className="text-small text-muted">{t("history.empty")}</p>
				) : (
					<ol className="flex flex-col gap-2">
						{history.map((h) => (
							<li key={h.actionId} className="text-small">
								<span className="font-medium">{t(`action.${h.action}`)}</span> · {t(`reason.${h.reasonCode}`)}{" "}
								· <time dateTime={h.createdAt}>{formatDate(h.createdAt, lang)}</time>
								{h.moderator && (
									<span className="text-muted"> · {t("mod.by", { name: h.moderator.displayName })}</span>
								)}
								{h.overturned && (
									<Badge variant="danger" className="ml-2">
										{t("history.overturned")}
									</Badge>
								)}
								{h.notes && <p className="text-muted">{h.notes}</p>}
							</li>
						))}
					</ol>
				)}
			</section>
		</Modal>
	);
}
