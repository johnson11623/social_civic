import { type ComponentProps, useId } from "react";

import { cn } from "@/lib/cn";
import { ChevronDownIcon } from "./icons";

type Option = { value: string; label: string };

type Props = Omit<ComponentProps<"select">, "id" | "children"> & {
	label: string;
	options: readonly Option[];
	placeholder?: string;
	error?: string | undefined;
};

/**
 * Primitive: native Select (keyboard, screen readers and mobile pickers for
 * free). The browser's own arrow is replaced by a chevron inset like the
 * Input's trailing controls, so arrows sit at the same spot on every browser.
 */
export function Select({ label, options, placeholder, error, className, ...props }: Props) {
	const id = useId();
	const errorId = `${id}-error`;
	return (
		<div className="flex flex-col gap-2">
			<label htmlFor={id} className="text-small font-medium text-ink">
				{label}
			</label>
			<div className="relative">
				<select
					id={id}
					aria-invalid={error ? true : undefined}
					aria-describedby={error ? errorId : undefined}
					className={cn(
						"peer min-h-11 w-full cursor-pointer appearance-none rounded-sm border border-border bg-paper py-2 pl-3 pr-12 text-body text-ink",
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
				<span className="pointer-events-none absolute inset-y-0 right-0 flex w-11 items-center justify-center text-muted peer-disabled:opacity-50">
					<ChevronDownIcon />
				</span>
			</div>
			{error && (
				<p id={errorId} role="alert" className="text-micro text-danger">
					{error}
				</p>
			)}
		</div>
	);
}
