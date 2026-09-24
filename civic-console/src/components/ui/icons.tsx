/**
 * Inline stroke icons (Lucide geometry, 24px grid). Inline SVG keeps them
 * out of the JS budget of an icon library; all are decorative (aria-hidden),
 * so the control that uses one must carry the accessible name.
 */
type IconProps = { className?: string };

const base = {
	viewBox: "0 0 24 24",
	fill: "none",
	stroke: "currentColor",
	strokeWidth: 2,
	strokeLinecap: "round",
	strokeLinejoin: "round",
	focusable: false,
} as const;

export function EyeIcon({ className = "h-5 w-5" }: IconProps) {
	return (
		<svg {...base} aria-hidden="true" className={className}>
			<path d="M2.06 12.35a1 1 0 0 1 0-.7 10.75 10.75 0 0 1 19.88 0 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-19.88 0" />
			<circle cx="12" cy="12" r="3" />
		</svg>
	);
}

export function EyeOffIcon({ className = "h-5 w-5" }: IconProps) {
	return (
		<svg {...base} aria-hidden="true" className={className}>
			<path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c4.35 0 8.07 2.66 9.94 6.65a1 1 0 0 1 0 .7 10.8 10.8 0 0 1-1.44 2.49" />
			<path d="M14.08 14.16a3 3 0 0 1-4.24-4.24" />
			<path d="M17.48 17.5A10.75 10.75 0 0 1 2.06 12.35a1 1 0 0 1 0-.7 10.8 10.8 0 0 1 4.45-5.14" />
			<path d="m2 2 20 20" />
		</svg>
	);
}

export function ChevronDownIcon({ className = "h-5 w-5" }: IconProps) {
	return (
		<svg {...base} aria-hidden="true" className={className}>
			<path d="m6 9 6 6 6-6" />
		</svg>
	);
}

export function HeartIcon({ className = "h-5 w-5", filled = false }: IconProps & { filled?: boolean }) {
	return (
		<svg {...base} fill={filled ? "currentColor" : "none"} aria-hidden="true" className={className}>
			<path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.3 1.5 4.05 3 5.5l7 7Z" />
		</svg>
	);
}

export function MessageIcon({ className = "h-5 w-5" }: IconProps) {
	return (
		<svg {...base} aria-hidden="true" className={className}>
			<path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z" />
		</svg>
	);
}

export function PlusIcon({ className = "h-5 w-5" }: IconProps) {
	return (
		<svg {...base} aria-hidden="true" className={className}>
			<path d="M5 12h14" />
			<path d="M12 5v14" />
		</svg>
	);
}

export function TrendingUpIcon({ className = "h-5 w-5" }: IconProps) {
	return (
		<svg {...base} aria-hidden="true" className={className}>
			<path d="M16 7h6v6" />
			<path d="m22 7-8.5 8.5-5-5L2 17" />
		</svg>
	);
}
