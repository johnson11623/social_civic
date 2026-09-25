import { type FormEvent, useId, useState } from "react";

import type { Post, ReasonCode } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { useToast } from "@/components/ui/Toast";
import { describeError, settle } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { callApiEither } from "@/runtimes/get-runtime";
import { ReasonSelect } from "./ReasonSelect";

const MAX_DETAILS = 500;

/**
 * Report a post for one of the policy's harms (EPIC 3.1.1). The intro says
 * plainly what isn't a reason: disagreement and criticism.
 */
export function ReportModal({ post, onClose }: { post: Post; onClose: () => void }) {
	const { t } = useT();
	const toast = useToast();
	const detailsId = useId();
	const [reason, setReason] = useState<ReasonCode | "">("");
	const [details, setDetails] = useState("");
	const [error, setError] = useState<{ reason?: string | undefined; form?: string | undefined }>({});
	const [busy, setBusy] = useState(false);
	const length = Array.from(details.trim()).length;

	async function submit(e: FormEvent) {
		e.preventDefault();
		if (!reason) return setError({ reason: t("reason.required") });
		if (length > MAX_DETAILS) return setError({ form: t("report.detailsTooLong", { max: MAX_DETAILS }) });
		setBusy(true);
		const res = settle(
			await callApiEither((api) =>
				api.moderation.report({
					payload: {
						postId: post.postId,
						reasonCode: reason,
						...(details.trim() ? { details: details.trim() } : {}),
					},
				}),
			),
		);
		setBusy(false);
		if (!res.ok) return setError({ form: describeError(res.error, t) });
		toast(t("report.sent"));
		onClose();
	}

	return (
		<Modal open onClose={onClose} title={t("report.title")} fullScreenOnMobile>
			<form onSubmit={submit} noValidate className="flex flex-col gap-4">
				<p className="text-small text-muted">{t("report.intro")}</p>
				<ReasonSelect
					label={t("reason.label")}
					value={reason}
					onChange={(r) => {
						setReason(r);
						setError({});
					}}
					error={error.reason}
				/>
				<div className="flex flex-col gap-2">
					<label htmlFor={detailsId} className="text-small font-medium text-ink">
						{t("report.details")} <span className="text-muted">({t("common.optional")})</span>
					</label>
					<textarea
						id={detailsId}
						rows={3}
						value={details}
						onChange={(e) => setDetails(e.target.value)}
						className={cn(
							"w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink",
							"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
							length > MAX_DETAILS && "border-danger",
						)}
					/>
				</div>
				{error.form && (
					<p role="alert" className="text-small text-danger">
						{error.form}
					</p>
				)}
				<div className="flex justify-end gap-3">
					<Button variant="ghost" onClick={onClose} disabled={busy}>
						{t("composer.cancel")}
					</Button>
					<Button type="submit" disabled={busy}>
						{t("report.submit")}
					</Button>
				</div>
			</form>
		</Modal>
	);
}
