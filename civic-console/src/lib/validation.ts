/**
 * Client-side checks mirroring the platform API, so users get instant,
 * localized feedback. The server re-validates everything.
 */

/** Consent version users agree to at registration (matches the Go API). */
export const CONSENT_VERSION = "2026-01";

export const isNationalId = (value: string) => /^\d{8}$/.test(value);

export const isOtp = (value: string) => /^\d{6}$/.test(value);

/** Keep only digits, up to `max` (for numeric inputs that ignore spaces). */
export const digitsOnly = (value: string, max: number) => value.replace(/\D/g, "").slice(0, max);

/**
 * Kenyan mobile → E.164 (+2547XXXXXXXX / +2541XXXXXXXX), accepting the ways
 * people write it: 0712 345 678, 712345678, 254712345678, +254-712-345-678.
 * Same rules as the Go API's NormalizeKenyanMobile.
 */
export function normalizeKenyanMobile(value: string): string | undefined {
	const trimmed = value.trim();
	if (!/^\+?[\d\s\-().]*$/.test(trimmed) || trimmed.indexOf("+") > 0) return undefined;
	let d = trimmed.replace(/\D/g, "");
	if (d.length === 12 && d.startsWith("254")) d = d.slice(3);
	else if (d.length === 10 && d.startsWith("0")) d = d.slice(1);
	else if (d.length !== 9) return undefined;
	return d[0] === "7" || d[0] === "1" ? `+254${d}` : undefined;
}

export function displayNameError(value: string): "required" | "too_long" | undefined {
	const n = Array.from(value.trim()).length;
	if (n === 0) return "required";
	if (n > 100) return "too_long";
	return undefined;
}
