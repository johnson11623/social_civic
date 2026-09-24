import { createTV } from "tailwind-variants";

import { twMergeConfig } from "./cn";

export type { VariantProps } from "tailwind-variants";

/**
 * T-W1.2.1.2 — tailwind-variants with the same token-aware merge as cn(), so
 * `className` overrides on components resolve conflicts correctly.
 */
export const tv = createTV({ twMerge: true, twMergeConfig });
