/**
 * Platform limits shared by the contract and components. A plain module:
 * components import it without pulling Effect (the contract) into the
 * initial bundle.
 */

/** Mirrors the Go API's limit (MaxContentRunes). */
export const MAX_POST_LENGTH = 500;

/** Mirrors the Go API's channel name rule (2–40, lowercase words joined by hyphens). */
export const CHANNEL_NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;
export const MAX_CHANNEL_DESCRIPTION = 140;
