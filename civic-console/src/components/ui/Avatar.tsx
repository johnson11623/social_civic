import { tv, type VariantProps } from "@/lib/variants";

const avatar = tv({
	base: "inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-surface-2 font-medium text-ink",
	variants: {
		size: {
			sm: "h-8 w-8 text-micro",
			md: "h-10 w-10 text-small",
			lg: "h-14 w-14 text-h2",
			xl: "h-24 w-24 text-h1",
		},
	},
	defaultVariants: { size: "md" },
});

type Props = VariantProps<typeof avatar> & { name: string; src?: string | undefined };

/** "Wanjiku M." → "WM"; handles extra spaces and non-Latin letters. */
export function initials(name: string): string {
	return name
		.trim()
		.split(/\s+/)
		.map((part) => Array.from(part)[0] ?? "")
		.slice(0, 2)
		.join("")
		.toLocaleUpperCase();
}

/** Atom: Avatar (T-W1.2.2.4) — image with alt text, or initials fallback. */
export function Avatar({ name, src, size }: Props) {
	if (src) {
		return (
			<span className={avatar({ size })}>
				<img src={src} alt={name} loading="lazy" decoding="async" className="h-full w-full object-cover" />
			</span>
		);
	}
	return (
		<span role="img" aria-label={name} className={avatar({ size })}>
			<span aria-hidden="true">{initials(name)}</span>
		</span>
	);
}
