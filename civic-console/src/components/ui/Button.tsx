import type { ComponentProps } from "react";

import { tv, type VariantProps } from "@/lib/variants";

export const button = tv({
	base: [
		"inline-flex items-center justify-center gap-2 rounded-sm font-medium",
		"transition-colors duration-150 ease-standard",
		"focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-paper",
		"disabled:pointer-events-none disabled:opacity-50",
	],
	variants: {
		variant: {
			primary: "bg-accent text-on-accent hover:bg-accent-hover",
			secondary: "bg-surface-2 text-ink hover:bg-border",
			ghost: "bg-transparent text-ink hover:bg-surface-2",
			danger: "bg-danger text-on-accent hover:opacity-90",
		},
		// Touch targets: ≥ 44px on mobile (md is the default); sm is for
		// pointer-dense desktop contexts (≥ 32px, Web App Design §4.5).
		size: {
			sm: "min-h-9 px-3 text-small",
			md: "min-h-11 min-w-22 px-4 text-body",
			lg: "min-h-13 px-6 text-h2",
		},
		full: { true: "w-full" },
	},
	defaultVariants: { variant: "primary", size: "md" },
});

type Props = ComponentProps<"button"> & VariantProps<typeof button>;

/** Atom: Button (T-W1.2.2.1). Defaults to type="button" so it never submits a form by accident. */
export function Button({ variant, size, full, className, type = "button", ...props }: Props) {
	return <button type={type} className={button({ variant, size, full, class: className })} {...props} />;
}
