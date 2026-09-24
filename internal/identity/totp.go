package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1; authenticator apps expect it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"time"
)

// TOTP (RFC 6238) for step-up MFA (F-08): 30-second steps, 6 digits,
// HMAC-SHA1 — what every authenticator app supports.
const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	// A code is accepted one step either side of now, for clock drift.
	totpSkew = 1
	// TOTPSecretBytes is the secret size (RFC 4226 recommends 160 bits).
	TOTPSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random secret.
func NewTOTPSecret() ([]byte, error) {
	s := make([]byte, TOTPSecretBytes)
	_, err := rand.Read(s)
	return s, err
}

// TOTPSecretText is the base32 form people type into an authenticator.
func TOTPSecretText(secret []byte) string { return totpEncoding.EncodeToString(secret) }

// TOTPURI is the otpauth:// URI an authenticator app reads from a QR code.
func TOTPURI(secret []byte, account string) string {
	q := url.Values{}
	q.Set("secret", TOTPSecretText(secret))
	q.Set("issuer", "Civic Platform")
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpStep.Seconds())))
	return "otpauth://totp/" + url.PathEscape("Civic Platform:"+account) + "?" + q.Encode()
}

// totpStepAt is the RFC 6238 time step counter for t.
func totpStepAt(t time.Time) int64 { return t.Unix() / int64(totpStep.Seconds()) }

// totpCode computes the code for a time step (RFC 4226 HOTP).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}

// VerifyTOTP checks code against now ± skew and returns the matching step.
// Steps at or before lastStep are refused, so a code works only once.
func VerifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if len(code) != totpDigits {
		return 0, false
	}
	current := totpStepAt(now)
	for d := -totpSkew; d <= totpSkew; d++ {
		step := current + int64(d)
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
