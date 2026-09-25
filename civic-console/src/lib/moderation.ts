/**
 * Moderation helpers shared by the queue, review and post views. Plain
 * module (no Effect), so components stay out of the API client's bundle.
 */
import type { Level, ReasonCode, RoleAssignment } from "@/api/api-contract";
import { LEVELS } from "@/lib/levels";

/** The platform's harm-based policy, in display order. */
export const REASONS: readonly ReasonCode[] = ["hate_speech", "incitement", "privacy", "child_safety"];

/** Levels at which the roles give moderation authority, ward first. */
export function moderatedLevels(roles: readonly RoleAssignment[]): Level[] {
	const held = new Set(
		roles.map((r) => r.level).filter((l): l is number => l !== undefined && l >= 1 && l <= 4),
	);
	return LEVELS.filter((_, i) => held.has(i + 1));
}

/** The highest level the roles reach, or undefined for non-moderators. */
export function topLevel(roles: readonly RoleAssignment[]): Level | undefined {
	return moderatedLevels(roles).at(-1);
}

/**
 * T-W2.2.2.4 — a moderator acts on posts at or below their highest level
 * (the server also checks the unit). Used to stop an out-of-authority
 * action before it is sent.
 */
export function withinAuthority(roles: readonly RoleAssignment[], level: Level): boolean {
	const top = topLevel(roles);
	return top !== undefined && LEVELS.indexOf(level) <= LEVELS.indexOf(top);
}

/** Whether an appeal can still be filed at `now`. */
export const appealOpen = (dueAt: string | undefined, now = new Date()) =>
	dueAt !== undefined && new Date(dueAt).getTime() > now.getTime();
