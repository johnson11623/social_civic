package identity

import (
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors (SHA-1 secret "12345678901234567890"),
// last 6 digits of the 8-digit values.
func TestTOTP_RFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037",
	} {
		if got := totpCode(secret, totpStepAt(time.Unix(unix, 0))); got != want {
			t.Errorf("t=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyTOTP_SkewAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	step := totpStepAt(now)
	code := totpCode(secret, step)

	got, ok := VerifyTOTP(secret, code, now, 0)
	if !ok || got != step {
		t.Fatalf("current code refused")
	}
	if _, ok := VerifyTOTP(secret, code, now, step); ok {
		t.Error("replayed code accepted")
	}
	if _, ok := VerifyTOTP(secret, totpCode(secret, step-1), now, 0); !ok {
		t.Error("previous step (drift) refused")
	}
	if _, ok := VerifyTOTP(secret, totpCode(secret, step-2), now, 0); ok {
		t.Error("code two steps old accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := VerifyTOTP(secret, bad, now, 0); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	if uri := TOTPURI(secret, "Wanjiku M."); uri[:15] != "otpauth://totp/" {
		t.Errorf("uri = %s", uri)
	}
}
