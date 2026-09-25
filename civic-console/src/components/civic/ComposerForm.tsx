import {
	type ClipboardEvent,
	type DragEvent,
	type FormEvent,
	type KeyboardEvent,
	useEffect,
	useId,
	useRef,
	useState,
} from "react";

import type { Channel, Media } from "@/api/api-contract";
import { Avatar } from "@/components/ui/Avatar";
import { Button } from "@/components/ui/Button";
import { ChevronDownIcon, ImageIcon, VideoIcon, XIcon } from "@/components/ui/icons";
import { describeError } from "@/lib/api-errors";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { MAX_POST_LENGTH, MEDIA_TYPES } from "@/lib/limits";
import { checkFile, mediaKind, type UploadFailure, type UploadProgress, uploadMedia } from "@/lib/upload";
import { LAST_CHANNEL_KEY, postLength } from "./Composer";

type OnSubmit = (channel: Channel, content: string, media?: Media) => Promise<string | null>;

type Props = {
	channels: readonly Channel[];
	onSubmit: OnSubmit;
	onDone: () => void;
	/** A photo or video picked from the "Start a post" card. */
	initialFile?: File | undefined;
	authorName?: string | undefined;
};

function readLastChannel(): string | null {
	try {
		return localStorage.getItem(LAST_CHANNEL_KEY);
	} catch {
		return null;
	}
}

/** The count shows once a post gets close to the limit. */
const COUNTER_FROM = MAX_POST_LENGTH - 100;

/**
 * "Create a post" (loaded when the composer opens): who is posting and
 * where, a roomy text area, one photo or video shown large with a remove
 * button and upload progress over it, and a toolbar with the Post button.
 * Photos can also be pasted or dropped in; Ctrl/⌘+Enter posts.
 */
export default function ComposerForm({ channels, onSubmit, onDone, initialFile, authorName }: Props) {
	const { t } = useT();
	const textareaId = useId();
	const channelId = useId();
	const errorId = `${textareaId}-error`;
	const textarea = useRef<HTMLTextAreaElement>(null);
	const photoInput = useRef<HTMLInputElement>(null);
	const videoInput = useRef<HTMLInputElement>(null);
	const aborter = useRef<AbortController | null>(null);

	// T-W1.4.2.2 — last used channel, else #general, else the first.
	const [channel, setChannel] = useState(() => {
		const known = (id: string | null) => channels.find((c) => c.channelId === id)?.channelId;
		return (
			known(readLastChannel()) ??
			channels.find((c) => c.name === "general")?.channelId ??
			channels[0]?.channelId ??
			""
		);
	});
	const [content, setContent] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [busy, setBusy] = useState(false);
	const [file, setFile] = useState<File | null>(null);
	const [preview, setPreview] = useState<string | null>(null);
	const [progress, setProgress] = useState<UploadProgress | null>(null);
	const [dragging, setDragging] = useState(false);

	const length = postLength(content);
	const over = length > MAX_POST_LENGTH;
	const kind = file ? mediaKind(file.type) : undefined;
	const canPost = !busy && !over && (length > 0 || file !== null);

	useEffect(() => textarea.current?.focus(), []);
	useEffect(() => () => aborter.current?.abort(), []);
	useEffect(() => {
		if (!preview) return;
		return () => URL.revokeObjectURL(preview);
	}, [preview]);

	// Grow with the text, like a chat box, up to the dialog's height.
	// biome-ignore lint/correctness/useExhaustiveDependencies: resize whenever the text changes
	useEffect(() => {
		const el = textarea.current;
		if (!el) return;
		el.style.height = "auto";
		el.style.height = `${el.scrollHeight}px`;
	}, [content]);

	const failureText = (f: UploadFailure): string => {
		switch (f.reason) {
			case "type":
				return t("composer.badType");
			case "size":
				return f.kind === "image" ? t("composer.tooBigImage") : t("composer.tooBigVideo");
			case "network":
				return t("composer.uploadFailed");
			case "processing":
				return t("composer.processFailed");
			default:
				return describeError(f.error, t);
		}
	};

	function pick(next: File | undefined | null) {
		if (!next) return;
		const bad = checkFile(next);
		if (bad) return setError(failureText(bad));
		setError(null);
		setFile(next);
		setPreview(URL.createObjectURL(next));
	}

	// biome-ignore lint/correctness/useExhaustiveDependencies: take the card's file once, on open
	useEffect(() => pick(initialFile), []);

	function removeMedia() {
		aborter.current?.abort();
		setFile(null);
		setPreview(null);
		setProgress(null);
	}

	async function submit(e?: FormEvent) {
		e?.preventDefault();
		// T-W1.4.2.6 — client-side checks first; nothing is sent.
		if (length === 0 && !file) return setError(t("composer.empty"));
		if (over) return setError(t("composer.tooLong", { max: MAX_POST_LENGTH }));
		const target = channels.find((c) => c.channelId === channel);
		if (!target || busy) return;
		setBusy(true);
		setError(null);
		let media: Media | undefined;
		if (file) {
			aborter.current = new AbortController();
			const up = await uploadMedia(file, "", setProgress, aborter.current.signal);
			setProgress(null);
			if (!up.ok) {
				setBusy(false);
				return setError(failureText(up.failure));
			}
			media = up.media;
		}
		const failure = await onSubmit(target, content.trim(), media);
		setBusy(false);
		if (failure) return setError(failure);
		try {
			localStorage.setItem(LAST_CHANNEL_KEY, target.channelId);
		} catch {
			// Storage disabled: the default is #general next time.
		}
		setContent("");
		removeMedia();
		onDone();
	}

	const onKeyDown = (e: KeyboardEvent) => {
		if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
			e.preventDefault();
			void submit();
		}
	};
	const onPaste = (e: ClipboardEvent) => {
		const pasted = Array.from(e.clipboardData.files).find((f) => mediaKind(f.type));
		if (pasted && !file) {
			e.preventDefault();
			pick(pasted);
		}
	};
	const onDrop = (e: DragEvent) => {
		e.preventDefault();
		setDragging(false);
		if (!busy) pick(e.dataTransfer.files[0]);
	};

	const tool =
		"inline-flex min-h-11 min-w-11 items-center justify-center rounded-full text-muted hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent disabled:opacity-40";

	return (
		<form
			onSubmit={submit}
			onKeyDown={onKeyDown}
			onDragOver={(e) => {
				e.preventDefault();
				setDragging(true);
			}}
			onDragLeave={() => setDragging(false)}
			onDrop={onDrop}
			noValidate
			className={cn("relative flex min-h-full flex-col gap-4", dragging && "rounded-md ring-3 ring-accent")}
		>
			{/* Who is posting, and where to. */}
			<div className="flex items-center gap-3">
				<Avatar name={authorName || "?"} size="md" />
				<div className="flex min-w-0 flex-col">
					{authorName && <span className="truncate font-medium text-ink">{authorName}</span>}
					<label htmlFor={channelId} className="sr-only">
						{t("composer.channel")}
					</label>
					<span className="relative inline-flex w-fit">
						<select
							id={channelId}
							value={channel}
							disabled={busy}
							onChange={(e) => setChannel(e.target.value)}
							className="min-h-9 cursor-pointer appearance-none rounded-full border border-border bg-paper py-1 pr-8 pl-3 text-small font-medium text-ink hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
						>
							{channels.map((c) => (
								<option key={c.channelId} value={c.channelId}>
									#{c.name}
								</option>
							))}
						</select>
						<span className="pointer-events-none absolute inset-y-0 right-2 flex items-center text-muted">
							<ChevronDownIcon className="h-4 w-4" />
						</span>
					</span>
				</div>
			</div>

			<label htmlFor={textareaId} className="sr-only">
				{t("composer.content")}
			</label>
			<textarea
				ref={textarea}
				id={textareaId}
				value={content}
				rows={4}
				placeholder={t("composer.content")}
				disabled={busy}
				onPaste={onPaste}
				onChange={(e) => {
					setContent(e.target.value);
					if (error) setError(null);
				}}
				aria-invalid={error ? true : undefined}
				aria-describedby={error ? errorId : undefined}
				className="max-h-96 min-h-28 w-full resize-none border-0 bg-transparent p-0 text-body text-ink placeholder:text-muted focus-visible:outline-none"
			/>

			{file && preview && kind && (
				<div className="relative overflow-hidden rounded-md border border-border bg-surface-2">
					{kind === "image" ? (
						<img src={preview} alt="" className="max-h-96 w-full object-contain" />
					) : (
						<video src={preview} controls muted playsInline className="max-h-96 w-full bg-kenya-black" />
					)}
					<button
						type="button"
						onClick={removeMedia}
						disabled={busy && progress?.stage === "processing"}
						className="absolute top-2 right-2 inline-flex h-9 w-9 items-center justify-center rounded-full bg-kenya-black/70 text-paper hover:bg-kenya-black focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
					>
						<XIcon className="h-5 w-5" />
						<span className="sr-only">{t("composer.remove")}</span>
					</button>
					{progress && (
						<div className="absolute inset-x-0 bottom-0 flex flex-col gap-1 bg-kenya-black/70 px-3 py-2 text-small text-paper">
							<p role="status">
								{progress.stage === "uploading"
									? t("composer.uploading", { percent: Math.round(progress.fraction * 100) })
									: t("composer.processing", { kind: t(`media.${kind}`) })}
							</p>
							{progress.stage === "uploading" && (
								<progress
									value={progress.fraction}
									max={1}
									aria-hidden="true"
									className="h-1.5 w-full accent-kenya-green"
								/>
							)}
						</div>
					)}
				</div>
			)}

			{error && (
				<p id={errorId} role="alert" className="text-small text-danger">
					{error}
				</p>
			)}

			{dragging && (
				<p className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-md bg-paper/90 text-body font-medium text-accent">
					{t("composer.dropHere")}
				</p>
			)}

			{/* Toolbar: add media on the left, the post button on the right. */}
			<div className="mt-auto flex items-center gap-1 border-t border-border pt-3">
				<button
					type="button"
					className={tool}
					disabled={busy || file !== null}
					onClick={() => photoInput.current?.click()}
				>
					<ImageIcon className="h-6 w-6 text-accent" />
					<span className="sr-only">{t("composer.addPhoto")}</span>
				</button>
				<button
					type="button"
					className={tool}
					disabled={busy || file !== null}
					onClick={() => videoInput.current?.click()}
				>
					<VideoIcon className="h-6 w-6 text-kenya-green" />
					<span className="sr-only">{t("composer.addVideo")}</span>
				</button>
				{[
					{ ref: photoInput, accept: MEDIA_TYPES.image },
					{ ref: videoInput, accept: MEDIA_TYPES.video },
				].map(({ ref, accept }) => (
					<input
						key={accept[0]}
						ref={ref}
						type="file"
						accept={accept.join(",")}
						className="sr-only"
						tabIndex={-1}
						aria-hidden="true"
						onChange={(e) => {
							pick(e.target.files?.[0]);
							e.target.value = "";
						}}
					/>
				))}
				<span className="ml-auto" />
				{length >= COUNTER_FROM && (
					<span className={cn("px-2 text-micro tabular-nums", over ? "text-danger" : "text-muted")}>
						{t("composer.charsLeft", { remaining: MAX_POST_LENGTH - length })}
					</span>
				)}
				<Button type="submit" disabled={!canPost} className="rounded-full">
					{busy ? t("composer.posting") : t("composer.submit")}
				</Button>
			</div>
		</form>
	);
}
