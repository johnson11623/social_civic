/**
 * Platform limits shared by the contract and components. A plain module:
 * components import it without pulling Effect (the contract) into the
 * initial bundle.
 */

/** Mirrors the Go API's limit (MaxContentRunes). */
export const MAX_POST_LENGTH = 500;
