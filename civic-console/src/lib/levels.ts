import type { Level } from "@/api/api-contract";

/** Ward → National, in elevation order. */
export const LEVELS: readonly Level[] = ["ward", "constituency", "county", "national"];

export const isLevel = (v: unknown): v is Level =>
	typeof v === "string" && (LEVELS as readonly string[]).includes(v);
