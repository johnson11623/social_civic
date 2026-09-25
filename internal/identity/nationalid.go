package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strconv"

	"github.com/google/uuid"
)

// Keyring entries used by identity.
const (
	// PepperKeyName holds the HMAC pepper for national IDs, phone numbers,
	// IPs and login codes.
	PepperKeyName = "national-id-pepper"
	// PIIKeyName holds the 32-byte AES-256-GCM key for personal data at rest.
	PIIKeyName = "pii-encryption-key"
)

// ErrInvalidNationalID is returned for IDs that are not exactly 8 digits.
var ErrInvalidNationalID = errors.New("identity: national ID must be 8 digits")

// ValidateNationalID checks the format before any hashing, so malformed IDs
// never reach the keyring or the database.
func ValidateNationalID(id string) error {
	if len(id) != 8 {
		return ErrInvalidNationalID
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return ErrInvalidNationalID
		}
	}
	return nil
}

// hmacSHA256 is the deterministic keyed hash used for national IDs and IPs.
// The raw input is never stored.
func hmacSHA256(input string, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(input))
	return mac.Sum(nil)
}

// hashNationalID hashes a validated national ID.
func hashNationalID(id string, pepper []byte) []byte {
	return hmacSHA256("nid:"+id, pepper)
}

// hashIP hashes a client IP for the consent record.
func hashIP(ip string, pepper []byte) []byte {
	if ip == "" {
		return nil
	}
	return hmacSHA256("ip:"+ip, pepper)
}

// hashMSISDN hashes a normalized phone number for lookups.
func hashMSISDN(e164 string, pepper []byte) []byte {
	return hmacSHA256("msisdn:"+e164, pepper)
}

// msisdnAAD binds an encrypted phone number to its user.
func msisdnAAD(publicID uuid.UUID) []byte {
	return []byte("users.msisdn:" + publicID.String())
}

// hashOTP hashes a login code for a user.
func hashOTP(userID int64, code string, pepper []byte) []byte {
	return hmacSHA256("otp:"+strconv.FormatInt(userID, 10)+":"+code, pepper)
}
