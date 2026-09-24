import type { ClassValue } from "clsx";

import { cn } from "./cn";

/**
 * T-W1.2.1.2 — component variants: `tv({ base, variants, defaultVariants })`
 * returns a function from variant props (plus a `class` override) to a
 * class string, merged by cn() so token-aware conflicts resolve correctly.
 *
 * In-house rather than tailwind-variants, which bundles its own copy of
 * tailwind-merge (~18 KB gzipped) — too much for the 3G budget for the
 * little of it we use.
 */
type Variants = Record<string, Record<string, ClassValue>>;

/** A variant named by "true"/"false" keys takes a boolean. */
type VariantValue<Options> = keyof Options extends "true" | "false" ? boolean : keyof Options;

type Selection<V extends Variants> = { [K in keyof V]?: VariantValue<V[K]> | undefined };

type Config<V extends Variants> = {
	base?: ClassValue;
	variants?: V;
	defaultVariants?: Selection<V>;
};

type Props<V extends Variants> = Selection<V> & { class?: ClassValue; className?: ClassValue };

export function tv<V extends Variants>(config: Config<V>): (props?: Props<V>) => string {
	return (props = {} as Props<V>) => {
		const classes: ClassValue[] = [config.base];
		for (const key in config.variants) {
			const value = props[key] ?? config.defaultVariants?.[key];
			if (value !== undefined) classes.push(config.variants[key]?.[String(value)]);
		}
		return cn(classes, props.class, props.className);
	};
}

/** The variant props a `tv` component accepts (without the class override). */
export type VariantProps<F> = F extends (props?: infer P) => string ? Omit<P, "class" | "className"> : never;
