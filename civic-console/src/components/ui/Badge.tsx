import type { ReactNode } from "react";

import { tv, type VariantProps } from "@/lib/variants";

export const badge = tv({
	base: "inline-flex items-center gap-1 rounded-sm px-2 py-0.5 text-micro uppercase tracking-wide",
	variants: {
		variant: {
			default: "bg-surface-2 text-muted",
			ward: "bg-kenya-green/10 text-kenya-green",
			constituency: "bg-accent/10 text-accent",
			county: "bg-warning/10 text-warning",
			national: "bg-kenya-red/10 text-kenya-red",
			verified: "bg-verified/10 text-verified",
			sponsored: "bg-sponsored text-ink",
			danger: "bg-danger/10 text-danger",
		},
	},
	defaultVariants: { variant: "default" },
});

type Props = VariantProps<typeof badge> & { children: ReactNode; className?: string };

/** Atom: Badge (T-W1.2.2.3) — informational label; colour is never the only signal. */
export function Badge({ children, variant, className }: Props) {
	return <span className={badge({ variant, class: className })}>{children}</span>;
}
