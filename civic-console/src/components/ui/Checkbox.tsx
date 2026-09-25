import { type ComponentProps, useId } from "react";

import { cn } from "@/lib/cn";

type Props = Omit<ComponentProps<"input">, "type" | "id"> & {
	label: string;
	error?: string | undefined;
};

/**
 * Primitive: Checkbox. For consent it must never be pre-checked (DPA 2019
 * s.30, "no dark patterns"), so `defaultChecked` is not accepted.
 */
export function Checkbox({ label, error, className, ...props }: Omit<Props, "defaultChecked">) {
	const id = useId();
	const errorId = `${id}-error`;
	return (
		<div className="flex flex-col gap-2">
			<div className="flex items-start gap-3">
				<input
					id={id}
					type="checkbox"
					aria-invalid={error ? true : undefined}
					aria-describedby={error ? errorId : undefined}
					className={cn("mt-0.5 h-6 w-6 shrink-0 accent-accent", className)}
					{...props}
				/>
				<label htmlFor={id} className="text-body text-ink">
					{label}
				</label>
			</div>
			{error && (
				<p id={errorId} role="alert" className="text-micro text-danger">
					{error}
				</p>
			)}
		</div>
	);
}
