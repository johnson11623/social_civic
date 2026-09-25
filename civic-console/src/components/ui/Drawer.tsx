import { type ReactNode, useEffect, useId, useRef } from "react";

import { useT } from "@/lib/i18n/I18nProvider";

type Props = {
	open: boolean;
	onClose: () => void;
	title: string;
	children: ReactNode;
};

/**
 * Primitive: Drawer — a panel from the left on the native <dialog> (focus
 * trapped, Escape closes, page behind inert), like Modal.
 */
export function Drawer({ open, onClose, title, children }: Props) {
	const { t } = useT();
	const ref = useRef<HTMLDialogElement>(null);
	const titleId = useId();

	useEffect(() => {
		const dialog = ref.current;
		if (!dialog) return;
		if (open && !dialog.open) {
			const opener = document.activeElement as HTMLElement | null;
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
			className="m-0 h-dvh max-h-none w-80 max-w-full border-0 border-r border-border bg-surface p-0 text-ink shadow-lg backdrop:bg-kenya-black/50"
		>
			{open && (
				<div className="flex h-full flex-col">
					<div className="flex items-center justify-end px-4 pt-3">
						<h2 id={titleId} className="sr-only">
							{title}
						</h2>
						<button
							type="button"
							onClick={onClose}
							className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-sm text-muted hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent"
						>
							<span aria-hidden="true">×</span>
							<span className="sr-only">{t("common.close")}</span>
						</button>
					</div>
					<div className="flex-1 overflow-y-auto">{children}</div>
				</div>
			)}
		</dialog>
	);
}
