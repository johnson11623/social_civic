import { useRouterState } from "@tanstack/react-router";
import { lazy, Suspense, useRef, useState } from "react";

import type { Channel, Media } from "@/api/api-contract";
import { Avatar } from "@/components/ui/Avatar";
import { ImageIcon, PlusIcon, VideoIcon } from "@/components/ui/icons";
import { Modal } from "@/components/ui/Modal";
import { Spinner } from "@/components/ui/Spinner";
import { cn } from "@/lib/cn";
import { useT } from "@/lib/i18n/I18nProvider";
import { MEDIA_TYPES } from "@/lib/limits";

// The form (and the upload code) loads when the composer opens.
const ComposerForm = lazy(() => import("./ComposerForm"));

export const LAST_CHANNEL_KEY = "civic.lastChannel";

/** Characters as the platform counts them (code points, not UTF-16 units). */
export const postLength = (text: string) => Array.from(text.trim()).length;

type Props = {
	channels: readonly Channel[];
	/** Publishes the post (text and/or ready media); resolves to an error message, or null on success. */
	onSubmit: (channel: Channel, content: string, media?: Media) => Promise<string | null>;
};

/** The signed-in user's name, from the root loader (see __root.tsx). */
export function useViewerName(): string | undefined {
	return useRouterState({
		select: (s) =>
			(s.matches[0]?.loaderData as { user?: { displayName: string } | null } | undefined)?.user?.displayName,
	});
}

/**
 * W1.4.2 — "Start a post", as on LinkedIn: a card with the viewer's avatar,
 * a prompt and Photo / Video shortcuts; everything opens the "Create a post"
 * dialog (full screen on phones, where a floating button starts it).
 */
export function Composer({ channels, onSubmit }: Props) {
	const { t } = useT();
	const name = useViewerName();
	const [open, setOpen] = useState(false);
	const [initialFile, setInitialFile] = useState<File | undefined>();
	const photoInput = useRef<HTMLInputElement>(null);
	const videoInput = useRef<HTMLInputElement>(null);
	const postable = channels.filter((c) => c.canPost);

	if (postable.length === 0) {
		return (
			<p className="rounded-md border border-dashed border-border p-4 text-small text-muted">
				{t("composer.noChannels")}
			</p>
		);
	}

	const start = (file?: File) => {
		setInitialFile(file);
		setOpen(true);
	};
	const close = () => {
		setOpen(false);
		setInitialFile(undefined);
	};
	// The picker opens from the click itself (browsers require that).
	const picker = (ref: typeof photoInput, accept: readonly string[]) => (
		<input
			ref={ref}
			type="file"
			accept={accept.join(",")}
			className="sr-only"
			tabIndex={-1}
			aria-hidden="true"
			onChange={(e) => {
				const file = e.target.files?.[0];
				e.target.value = "";
				if (file) start(file);
			}}
		/>
	);
	const shortcut =
		"inline-flex min-h-11 flex-1 items-center justify-center gap-2 rounded-sm px-3 text-small font-medium text-muted hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent";

	return (
		<>
			<div className="hidden flex-col gap-2 rounded-md border border-border bg-paper p-3 md:flex">
				<div className="flex items-center gap-3">
					<Avatar name={name || "?"} size="md" />
					<button
						type="button"
						onClick={() => start()}
						className={cn(
							"min-h-12 flex-1 rounded-full border border-border px-4 text-left text-body font-medium text-muted",
							"hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
						)}
					>
						{t("composer.prompt")}
					</button>
				</div>
				<div className="flex">
					<button type="button" className={shortcut} onClick={() => photoInput.current?.click()}>
						<ImageIcon className="h-5 w-5 text-accent" />
						{t("composer.photo")}
					</button>
					<button type="button" className={shortcut} onClick={() => videoInput.current?.click()}>
						<VideoIcon className="h-5 w-5 text-kenya-green" />
						{t("composer.video")}
					</button>
				</div>
				{picker(photoInput, MEDIA_TYPES.image)}
				{picker(videoInput, MEDIA_TYPES.video)}
			</div>
			<button
				type="button"
				onClick={() => start()}
				className={cn(
					"fixed right-4 bottom-6 z-40 inline-flex h-14 w-14 items-center justify-center rounded-full bg-kenya-green text-on-accent shadow-lg md:hidden",
					"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-paper",
				)}
			>
				<PlusIcon className="h-6 w-6" />
				<span className="sr-only">{t("composer.open")}</span>
			</button>
			<Modal open={open} onClose={close} title={t("composer.title")} fullScreenOnMobile>
				{open && (
					<Suspense fallback={<Spinner />}>
						<ComposerForm
							channels={postable}
							onSubmit={onSubmit}
							onDone={close}
							initialFile={initialFile}
							authorName={name}
						/>
					</Suspense>
				)}
			</Modal>
		</>
	);
}
