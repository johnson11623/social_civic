import { useId } from "react";

import { cn } from "@/lib/cn";

type Props = {
	label: string;
	hint?: string | undefined;
	checked: boolean;
	onChange: (checked: boolean) => void;
	disabled?: boolean;
};

/**
 * Primitive: Switch — an on/off setting (role=switch, aria-checked), with
 * its label and hint linked; Space/Enter toggle it like any button.
 */
export function Switch({ label, hint, checked, onChange, disabled = false }: Props) {
	const id = useId();
	return (
		<div className="flex items-start justify-between gap-4">
			<div className="flex flex-col">
				<span id={`${id}-label`} className="text-body text-ink">
					{label}
				</span>
				{hint && (
					<span id={`${id}-hint`} className="text-small text-muted">
						{hint}
					</span>
				)}
			</div>
			<button
				type="button"
				role="switch"
				aria-checked={checked}
				aria-labelledby={`${id}-label`}
				aria-describedby={hint ? `${id}-hint` : undefined}
				disabled={disabled}
				onClick={() => onChange(!checked)}
				className={cn(
					"relative inline-flex h-7 w-12 shrink-0 items-center rounded-full border-2 border-transparent transition-colors",
					"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-paper",
					"disabled:cursor-not-allowed disabled:opacity-50",
					checked ? "bg-kenya-green" : "bg-border",
				)}
			>
				<span
					aria-hidden="true"
					className={cn(
						"inline-block h-6 w-6 rounded-full bg-paper shadow-sm transition-transform",
						checked ? "translate-x-5" : "translate-x-0",
					)}
				/>
			</button>
		</div>
	);
}
