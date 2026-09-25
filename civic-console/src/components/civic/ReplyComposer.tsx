import { type FormEvent, useId, useState } from "react";

import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { MAX_POST_LENGTH } from "@/lib/limits";
import { postLength } from "./Composer";

type Props = {
	label: string;
	/** Publishes the reply; resolves to an error message, or null on success. */
	onSubmit: (content: string) => Promise<string | null>;
	onCancel?: () => void;
	autoFocus?: boolean;
};

/**
 * T-W1.4.3.3 — inline reply box: same limits and checks as the composer,
 * the draft kept if publishing fails.
 */
export function ReplyComposer({ label, onSubmit, onCancel, autoFocus = false }: Props) {
	const { t } = useT();
	const id = useId();
	const [content, setContent] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);
	const length = postLength(content);
	const over = length > MAX_POST_LENGTH;

	async function submit(e: FormEvent) {
		e.preventDefault();
		if (length === 0) return setError(t("composer.empty"));
		if (over) return setError(t("composer.tooLong", { max: MAX_POST_LENGTH }));
		setBusy(true);
		setError(null);
		const failure = await onSubmit(content.trim());
		setBusy(false);
		if (failure) return setError(failure);
		setContent("");
		onCancel?.();
	}

	return (
		<form onSubmit={submit} noValidate className="flex flex-col gap-2">
			<label htmlFor={id} className="sr-only">
				{label}
			</label>
			<textarea
				id={id}
				value={content}
				rows={2}
				placeholder={label}
				// biome-ignore lint/a11y/noAutofocus: opened on request by the Reply button
				autoFocus={autoFocus}
				onChange={(e) => {
					setContent(e.target.value);
					if (error) setError(null);
				}}
				aria-invalid={error ? true : undefined}
				aria-describedby={error ? `${id}-error` : `${id}-count`}
				className={cn(
					"min-h-20 w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink placeholder:text-muted",
					"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
					(error || over) && "border-danger",
				)}
			/>
			{error && (
				<p id={`${id}-error`} role="alert" className="text-small text-danger">
					{error}
				</p>
			)}
			<div className="flex items-center justify-end gap-3">
				<p
					id={`${id}-count`}
					className={cn("mr-auto text-micro tabular-nums", over ? "text-danger" : "text-muted")}
				>
					<span aria-hidden="true">{t("composer.counter", { count: length, max: MAX_POST_LENGTH })}</span>
					<span className="sr-only">
						{t("composer.counterLabel", { remaining: MAX_POST_LENGTH - length })}
					</span>
				</p>
				{onCancel && (
					<Button variant="ghost" size="sm" onClick={onCancel} disabled={busy}>
						{t("composer.cancel")}
					</Button>
				)}
				<Button type="submit" size="sm" disabled={busy}>
					{t("reply.submit")}
				</Button>
			</div>
		</form>
	);
}
