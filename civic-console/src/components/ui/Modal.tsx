import { type ReactNode, useEffect, useId, useRef } from "react";

import { useT } from "@/lib/i18n/I18nProvider";

type Props = {
	open: boolean;
	onClose: () => void;
	title: string;
	children: ReactNode;
};

/**
 * Primitive: Modal on the native <dialog> (T-X.8): the browser traps focus,
 * closes on Escape and makes the page behind inert; focus returns to the
 * element that opened it.
 */
export function Modal({ open, onClose, title, children }: Props) {
	const { t } = useT();
	const ref = useRef<HTMLDialogElement>(null);
	const titleId = useId();

	useEffect(() => {
		const dialog = ref.current;
		if (!dialog) return;
		if (open && !dialog.open) {
			const opener = document.activeElement as HTMLElement | null;
			// Fallback for environments without showModal (old browsers, jsdom).
			if (typeof dialog.showModal === "function") dialog.showModal();
			else dialog.setAttribute("open", "");
			return () => opener?.focus?.();
		}
		if (!open && dialog.open) {
			if (typeof dialog.close === "function") dialog.close();
			else dialog.removeAttribute("open");
		}
	}, [open]);

	return (
		<dialog
			ref={ref}
			aria-labelledby={titleId}
			onClose={onClose}
			onCancel={onClose}
			className="m-auto w-full max-w-lg rounded-lg border border-border bg-paper p-0 text-ink shadow-lg backdrop:bg-kenya-black/50"
		>
			{open && (
				<div className="flex flex-col gap-4 p-6">
					<div className="flex items-start justify-between gap-4">
						<h2 id={titleId} className="text-h2">
							{title}
						</h2>
						<button
							type="button"
							onClick={onClose}
							className="-m-2 inline-flex min-h-11 min-w-11 items-center justify-center rounded-sm text-muted hover:bg-surface-2"
						>
							<span aria-hidden="true">×</span>
							<span className="sr-only">{t("common.close")}</span>
						</button>
					</div>
					<div className="flex flex-col gap-3 text-body">{children}</div>
				</div>
			)}
		</dialog>
	);
}
