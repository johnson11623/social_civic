import { describe, expect, it } from "vitest";

import { maskPhone } from "@/components/join/CodeStep";
import { digitsOnly, displayNameError, isNationalId, isOtp, normalizeKenyanMobile } from "@/lib/validation";

describe("normalizeKenyanMobile (same cases as the Go API)", () => {
	it.each([
		["0712345678", "+254712345678"],
		["0712 345 678", "+254712345678"],
		["712345678", "+254712345678"],
		["254712345678", "+254712345678"],
		["+254712345678", "+254712345678"],
		["+254-712-345-678", "+254712345678"],
		["(0712) 345.678", "+254712345678"],
		["0110123456", "+254110123456"],
		["0100 123 456", "+254100123456"],
		["  +254 733 000111", "+254733000111"],
	])("%s → %s", (input, want) => expect(normalizeKenyanMobile(input)).toBe(want));

	it.each([
		"",
		"0712",
		"07123456789",
		"020 222 2222",
		"+255712345678",
		"0812345678",
		"07l2345678",
		"0712345678+",
		"+1 415 555 0100",
	])("rejects %s", (input) => expect(normalizeKenyanMobile(input)).toBeUndefined());
});

describe("other checks", () => {
	it("validates IDs, codes and names", () => {
		expect(isNationalId("12345678")).toBe(true);
		expect(isNationalId("1234567")).toBe(false);
		expect(isNationalId("1234567a")).toBe(false);
		expect(isOtp("123456")).toBe(true);
		expect(isOtp("12345")).toBe(false);
		expect(digitsOnly("12 34-56ab78 9", 8)).toBe("12345678");
		expect(displayNameError("  ")).toBe("required");
		expect(displayNameError("ñ".repeat(101))).toBe("too_long");
		expect(displayNameError("Wanjiku M.")).toBeUndefined();
	});

	it("masks phones", () => {
		expect(maskPhone("+254712345678")).toBe("+254 7•• ••• 678");
		expect(maskPhone("nope")).toBe("•••");
	});
});
