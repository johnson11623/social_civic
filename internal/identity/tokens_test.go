package identity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testIssuer(t *testing.T, now *time.Time) *TokenIssuer {
	t.Helper()
	ti, err := NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	return ti
}

func TestTokensRoundTripAndTypes(t *testing.T) {
	now := fixedNow
	ti := testIssuer(t, &now)
	pair, err := ti.Issue("user-1", ScopeClaim{Ward: 17, Constituency: 4, County: 1})
	if err != nil {
		t.Fatal(err)
	}
	access, err := ti.Parse(pair.Access, TokenTypeAccess)
	if err != nil || access.Scope == nil || access.Scope.Ward != 17 {
		t.Fatalf("access = %+v, %v", access, err)
	}
	refresh, err := ti.Parse(pair.Refresh, TokenTypeRefresh)
	if err != nil || refresh.ID != pair.RefreshJTI.String() || refresh.Scope != nil {
		t.Fatalf("refresh = %+v, %v", refresh, err)
	}
	if !pair.RefreshExpiresAt.Equal(pair.IssuedAt.Add(RefreshTokenTTL)) {
		t.Errorf("refresh expiry = %s", pair.RefreshExpiresAt)
	}
	// An access token cannot be used as a refresh token, and vice versa.
	if _, err := ti.Parse(pair.Access, TokenTypeRefresh); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("access as refresh: %v", err)
	}
	if _, err := ti.Parse(pair.Refresh, TokenTypeAccess); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("refresh as access: %v", err)
	}
}

func TestTokenExpiry(t *testing.T) {
	now := fixedNow
	ti := testIssuer(t, &now)
	pair, _ := ti.Issue("user-1", ScopeClaim{})
	now = now.Add(AccessTokenTTL + time.Second)
	if _, err := ti.Parse(pair.Access, TokenTypeAccess); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expired access: %v", err)
	}
	if _, err := ti.Parse(pair.Refresh, TokenTypeRefresh); err != nil {
		t.Errorf("refresh should still be valid: %v", err)
	}
	now = fixedNow.Add(RefreshTokenTTL + time.Second)
	if _, err := ti.Parse(pair.Refresh, TokenTypeRefresh); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expired refresh: %v", err)
	}
}

func TestTokensRejectForgeries(t *testing.T) {
	now := fixedNow
	ti := testIssuer(t, &now)
	pair, _ := ti.Issue("user-1", ScopeClaim{})

	other, _ := NewTokenIssuer([]byte("another-signing-key-0123456789abcd"), func() time.Time { return now })
	forged, _ := other.Issue("user-1", ScopeClaim{})

	parts := strings.Split(pair.Access, ".")
	noneAlg := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + parts[1] + "."

	wrongAud, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		RegisteredClaims: jwt.RegisteredClaims{ID: "018f0000-0000-7000-8000-000000000000", Issuer: tokenIssuer, Audience: jwt.ClaimStrings{"someone-else"},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))},
		Type: TokenTypeAccess,
	}).SignedString([]byte("test-signing-key-0123456789abcdef"))

	for name, tok := range map[string]string{
		"other key":      forged.Access,
		"alg none":       noneAlg,
		"tampered":       parts[0] + "." + parts[1] + "x." + parts[2],
		"wrong audience": wrongAud,
		"garbage":        "not-a-jwt",
		"empty":          "",
	} {
		if _, err := ti.Parse(tok, TokenTypeAccess); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("%s: err = %v, want ErrTokenInvalid", name, err)
		}
	}
}

func TestGenerateOTP(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := generateOTP()
		if err != nil || !isOTPFormat(c) {
			t.Fatalf("generateOTP = %q, %v", c, err)
		}
		seen[c] = true
	}
	if len(seen) < 195 {
		t.Errorf("only %d distinct codes in 200; generator looks biased", len(seen))
	}
	for _, s := range []string{"", "12345", "1234567", "12345a", "１２３４５６"} {
		if isOTPFormat(s) {
			t.Errorf("isOTPFormat(%q) = true", s)
		}
	}
}
