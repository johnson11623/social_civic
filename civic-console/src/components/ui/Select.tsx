import { type ComponentProps, useId } from "react";

import { cn } from "@/lib/cn";

type Option = { value: string; label: string };

type Props = Omit<ComponentProps<"select">, "id" | "children"> & {
	label: string;
	options: readonly Option[];
	placeholder?: string;
	error?: string | undefined;
};

/** Primitive: native Select (keyboard, screen readers and mobile pickers for free). */
export function Select({ label, options, placeholder, error, className, ...props }: Props) {
	const id = useId();
	const errorId = `${id}-error`;
	return (
		<div className="flex flex-col gap-2">
			<label htmlFor={id} className="text-small font-medium text-ink">
				{label}
			</label>
			<select
				id={id}
				aria-invalid={error ? true : undefined}
				aria-describedby={error ? errorId : undefined}
				className={cn(
					"min-h-11 w-full rounded-sm border border-border bg-paper px-3 text-body text-ink",
					"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent",
					"disabled:cursor-not-allowed disabled:bg-surface disabled:text-muted",
					error && "border-danger",
					className,
				)}
				{...props}
			>
				{placeholder !== undefined && <option value="">{placeholder}</option>}
				{options.map((o) => (
					<option key={o.value} value={o.value}>
						{o.label}
					</option>
				))}
			</select>
			{error && (
				<p id={errorId} role="alert" className="text-micro text-danger">
					{error}
				</p>
			)}
		</div>
	);
}
