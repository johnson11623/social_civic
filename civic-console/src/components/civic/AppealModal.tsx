import { type FormEvent, useId, useState } from "react";

import type { Post } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { Modal } from "@/components/ui/Modal";
import { describeError, settle } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { formatDate } from "@/lib/format";
import { useT } from "@/lib/i18n/I18nProvider";
import { callApiEither } from "@/runtimes/get-runtime";

const MAX_STATEMENT = 2000;

type Props = {
	post: Post;
	onClose: () => void;
	/** Called with the decision's due date once the appeal is filed. */
	onFiled: (dueAt: string) => void;
};

/** Web App Design Flow 5 — appeal a decision: window shown, statement, submit. */
export function AppealModal({ post, onClose, onFiled }: Props) {
	const { t, lang } = useT();
	const id = useId();
	const [statement, setStatement] = useState("");
	const [error, setError] = useState<string>();
	const [busy, setBusy] = useState(false);
	const moderation = post.moderation;
	const length = Array.from(statement.trim()).length;

	async function submit(e: FormEvent) {
		e.preventDefault();
		if (!moderation) return;
		if (length === 0 || length > MAX_STATEMENT) return setError(t("moderation.statementInvalid"));
		setBusy(true);
		const res = settle(
			await callApiEither((api) =>
				api.moderation.appeal({
					payload: { moderationId: moderation.actionId, statement: statement.trim() },
				}),
			),
		);
		setBusy(false);
		if (!res.ok) return setError(describeError(res.error, t));
		onFiled(res.value.dueAt);
	}

	return (
		<Modal open onClose={onClose} title={t("appeal.title")} fullScreenOnMobile>
			<form onSubmit={submit} noValidate className="flex flex-col gap-4">
				{moderation?.appealDueAt && (
					<p className="text-small text-muted">
						{t("appeal.until", { date: formatDate(moderation.appealDueAt, lang) })}
					</p>
				)}
				<div className="flex flex-col gap-2">
					<label htmlFor={id} className="text-small font-medium text-ink">
						{t("appeal.statement")}
					</label>
					<textarea
						id={id}
						rows={6}
						value={statement}
						onChange={(e) => {
							setStatement(e.target.value);
							setError(undefined);
						}}
						aria-invalid={error ? true : undefined}
						aria-describedby={error ? `${id}-error` : undefined}
						className={cn(
							"w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink",
							"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
							error && "border-danger",
						)}
					/>
				</div>
				{error && (
					<p id={`${id}-error`} role="alert" className="text-small text-danger">
						{error}
					</p>
				)}
				<div className="flex justify-end gap-3">
					<Button variant="ghost" onClick={onClose} disabled={busy}>
						{t("composer.cancel")}
					</Button>
					<Button type="submit" disabled={busy}>
						{t("appeal.submit")}
					</Button>
				</div>
			</form>
		</Modal>
	);
}
