import { type FormEvent, useEffect, useId, useRef, useState } from "react";

import type { Channel } from "@/api/api-contract";
import { Button } from "@/components/ui/Button";
import { PlusIcon } from "@/components/ui/icons";
import { Modal } from "@/components/ui/Modal";
import { Select } from "@/components/ui/Select";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { MAX_POST_LENGTH } from "@/lib/limits";

export const LAST_CHANNEL_KEY = "civic.lastChannel";

/** Characters as the platform counts them (code points, not UTF-16 units). */
export const postLength = (text: string) => Array.from(text.trim()).length;

type Props = {
	channels: readonly Channel[];
	/** Publishes the post; resolves to an error message, or null on success. */
	onSubmit: (channel: Channel, content: string) => Promise<string | null>;
};

function readLastChannel(): string | null {
	try {
		return localStorage.getItem(LAST_CHANNEL_KEY);
	} catch {
		return null;
	}
}

/**
 * W1.4.2 — post composer. Desktop: an inline prompt that expands in the
 * feed. Phones: a floating "New post" button opening a full-screen modal
 * (T-W1.4.2.1, T-W1.4.2.7). One form, so a draft is never duplicated.
 */
export function Composer({ channels, onSubmit }: Props) {
	const { t } = useT();
	const [mode, setMode] = useState<"closed" | "inline" | "modal">("closed");
	const postable = channels.filter((c) => !c.readOnly);

	if (postable.length === 0) {
		return (
			<p className="rounded-md border border-dashed border-border p-4 text-small text-muted">
				{t("composer.noChannels")}
			</p>
		);
	}

	const form = <ComposerForm channels={postable} onSubmit={onSubmit} onDone={() => setMode("closed")} />;

	return (
		<>
			{mode === "inline" ? (
				<section
					aria-label={t("composer.title")}
					className="hidden rounded-md border border-border p-4 md:block"
				>
					{form}
				</section>
			) : (
				<button
					type="button"
					onClick={() => setMode("inline")}
					className={cn(
						"hidden min-h-13 w-full items-center gap-3 rounded-md border border-border bg-surface px-4 text-left text-body text-muted md:flex",
						"hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
					)}
				>
					<PlusIcon className="h-5 w-5 text-kenya-green" />
					{t("composer.prompt")}
				</button>
			)}
			<button
				type="button"
				onClick={() => setMode("modal")}
				className={cn(
					"fixed right-4 bottom-6 z-40 inline-flex h-14 w-14 items-center justify-center rounded-full bg-kenya-green text-on-accent shadow-lg md:hidden",
					"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-paper",
				)}
			>
				<PlusIcon className="h-6 w-6" />
				<span className="sr-only">{t("composer.open")}</span>
			</button>
			<Modal
				open={mode === "modal"}
				onClose={() => setMode("closed")}
				title={t("composer.title")}
				fullScreenOnMobile
			>
				{mode === "modal" && form}
			</Modal>
		</>
	);
}

function ComposerForm({
	channels,
	onSubmit,
	onDone,
}: {
	channels: readonly Channel[];
	onSubmit: Props["onSubmit"];
	onDone: () => void;
}) {
	const { t } = useT();
	const textareaId = useId();
	const counterId = `${textareaId}-counter`;
	const errorId = `${textareaId}-error`;
	const textarea = useRef<HTMLTextAreaElement>(null);

	// T-W1.4.2.2 — last used channel, else #general, else the first.
	const [channelId, setChannelId] = useState(() => {
		const last = readLastChannel();
		const known = (id: string | null) => channels.find((c) => c.channelId === id)?.channelId;
		return (
			known(last) ?? channels.find((c) => c.name === "general")?.channelId ?? channels[0]?.channelId ?? ""
		);
	});
	const [content, setContent] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);

	useEffect(() => textarea.current?.focus(), []);

	const length = postLength(content);
	const over = length > MAX_POST_LENGTH;

	async function submit(e: FormEvent) {
		e.preventDefault();
		// T-W1.4.2.6 — client-side checks first; nothing is sent.
		if (length === 0) return setError(t("composer.empty"));
		if (over) return setError(t("composer.tooLong", { max: MAX_POST_LENGTH }));
		const channel = channels.find((c) => c.channelId === channelId);
		if (!channel) return;
		setBusy(true);
		setError(null);
		const failure = await onSubmit(channel, content.trim());
		setBusy(false);
		if (failure) {
			setError(failure);
			return;
		}
		try {
			localStorage.setItem(LAST_CHANNEL_KEY, channel.channelId);
		} catch {
			// Storage disabled: the default is #general next time.
		}
		setContent("");
		onDone();
	}

	return (
		<form onSubmit={submit} noValidate className="flex flex-col gap-4">
			<Select
				label={t("composer.channel")}
				value={channelId}
				onChange={(e) => setChannelId(e.target.value)}
				options={channels.map((c) => ({ value: c.channelId, label: `#${c.name}` }))}
			/>
			<div className="flex flex-col gap-2">
				<label htmlFor={textareaId} className="text-small font-medium text-ink">
					{t("composer.content")}
				</label>
				<textarea
					ref={textarea}
					id={textareaId}
					value={content}
					rows={5}
					onChange={(e) => {
						setContent(e.target.value);
						if (error) setError(null);
					}}
					aria-invalid={error ? true : undefined}
					aria-describedby={[counterId, error ? errorId : null].filter(Boolean).join(" ")}
					className={cn(
						"min-h-32 w-full resize-y rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink",
						"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
						(error || over) && "border-danger",
					)}
				/>
				<div className="flex items-start justify-between gap-3">
					<p className="text-micro text-muted">{t("composer.scope")}</p>
					{/* T-W1.4.2.3 — visible count; the remaining count is what's announced. */}
					<p
						id={counterId}
						className={cn("shrink-0 text-micro tabular-nums", over ? "text-danger" : "text-muted")}
					>
						<span aria-hidden="true">{t("composer.counter", { count: length, max: MAX_POST_LENGTH })}</span>
						<span className="sr-only">
							{t("composer.counterLabel", { remaining: MAX_POST_LENGTH - length })}
						</span>
					</p>
				</div>
				{error && (
					<p id={errorId} role="alert" className="text-small text-danger">
						{error}
					</p>
				)}
			</div>
			<div className="flex justify-end gap-3">
				<Button variant="ghost" onClick={onDone} disabled={busy}>
					{t("composer.cancel")}
				</Button>
				<Button type="submit" disabled={busy}>
					{t("composer.submit")}
				</Button>
			</div>
		</form>
	);
}
