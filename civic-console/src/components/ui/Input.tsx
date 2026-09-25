import { type ComponentProps, type ReactNode, useId } from "react";

import { cn } from "@/lib/cn";

type Props = Omit<ComponentProps<"input">, "id"> & {
	/** Always visible (Web App Design §6.2: never placeholder-only labels). */
	label: string;
	error?: string | undefined;
	hint?: string | undefined;
	id?: string | undefined;
	/**
	 * Control shown inside the field on the right (e.g. show/hide). Rendered
	 * in a 44×44 slot; the input reserves room so text never runs under it.
	 */
	trailing?: ReactNode;
};

/** Atom: Input (T-W1.2.2.2) — linked label, hint and announced error. */
export function Input({ label, error, hint, id, trailing, className, ...props }: Props) {
	const generated = useId();
	const inputId = id ?? generated;
	const hintId = `${inputId}-hint`;
	const errorId = `${inputId}-error`;
	const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(" ") || undefined;

	return (
		<div className="flex flex-col gap-2">
			<label htmlFor={inputId} className="text-small font-medium text-ink">
				{label}
			</label>
			<div className="relative">
				<input
					id={inputId}
					aria-invalid={error ? true : undefined}
					aria-describedby={describedBy}
					className={cn(
						"min-h-11 w-full rounded-sm border border-border bg-paper px-3 py-2 text-body text-ink",
						"placeholder:text-muted",
						"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
						"disabled:cursor-not-allowed disabled:bg-surface disabled:text-muted",
						error && "border-danger",
						trailing !== undefined && "pr-12",
						className,
					)}
					{...props}
				/>
				{trailing !== undefined && (
					<div className="absolute inset-y-0 right-0 flex items-center pr-0.5">{trailing}</div>
				)}
			</div>
			{hint && (
				<p id={hintId} className="text-micro text-muted">
					{hint}
				</p>
			)}
			{error && (
				<p id={errorId} className="text-micro text-danger" role="alert">
					{error}
				</p>
			)}
		</div>
	);
}
