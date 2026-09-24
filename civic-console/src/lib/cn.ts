import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

/**
 * tailwind-merge must know the design tokens: without this it cannot tell a
 * font-size token (text-body) from a colour token (text-ink) and would drop
 * one of them when both are present.
 */
export const twMergeConfig = {
	extend: {
		theme: {
			text: ["display", "h1", "h2", "body", "small", "micro"],
			color: [
				"ink",
				"paper",
				"surface",
				"surface-2",
				"border",
				"muted",
				"accent",
				"accent-hover",
				"kenya-green",
				"kenya-red",
				"kenya-black",
				"warning",
				"danger",
				"sponsored",
				"elevated",
				"verified",
				"on-accent",
			],
			radius: ["sm", "md", "lg"],
			shadow: ["sm", "md", "lg"],
		},
	},
} as const;

const twMerge = extendTailwindMerge(twMergeConfig);

/** T-W1.2.1.1 — conditional classes; later conflicting classes win. */
export function cn(...inputs: ClassValue[]): string {
	return twMerge(clsx(inputs));
}
