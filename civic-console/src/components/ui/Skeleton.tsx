import { cn } from "@/lib/cn";

/**
 * Primitive: Skeleton — a placeholder shaped like the content it stands in
 * for, so nothing shifts when data arrives. Decorative: the container that
 * shows skeletons carries aria-busy and a status label. The pulse stops
 * under prefers-reduced-motion (global override).
 */
export function Skeleton({ className }: { className?: string }) {
	return <span aria-hidden="true" className={cn("block animate-pulse rounded-sm bg-surface-2", className)} />;
}
