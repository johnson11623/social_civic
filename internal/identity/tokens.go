package identity

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	tokenIssuer   = "identity-service"
	tokenAudience = "civic-platform"

	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 30 * 24 * time.Hour
)

// Claims are the platform's JWT claims (F-03: jti, aud, iss, nbf, exp).
type Claims struct {
	jwt.RegisteredClaims
	Type string `json:"typ"` // "access" or "refresh"
}

// TokenPair is returned to clients on registration and login.
type TokenPair struct {
	Access    string
	Refresh   string
	ExpiresIn int // access token lifetime in seconds
}

// TokenIssuer signs access and refresh tokens with HS256.
//
// Refresh token persistence, rotation and reuse detection come with
// T-1.1.2.4/T-1.1.2.5; asymmetric signing keys from KMS with T-X.4.
type TokenIssuer struct {
	key []byte
	now func() time.Time
}

// NewTokenIssuer requires a signing key of at least 32 bytes.
func NewTokenIssuer(key []byte, now func() time.Time) (*TokenIssuer, error) {
	if len(key) < 32 {
		return nil, errors.New("identity: JWT signing key must be at least 32 bytes")
	}
	if now == nil {
		now = time.Now
	}
	return &TokenIssuer{key: append([]byte(nil), key...), now: now}, nil
}

// Issue creates an access/refresh pair for the user's public id.
func (t *TokenIssuer) Issue(subject string) (TokenPair, error) {
	now := t.now()
	access, err := t.sign(subject, "access", now, AccessTokenTTL)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, err := t.sign(subject, "refresh", now, RefreshTokenTTL)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{Access: access, Refresh: refresh, ExpiresIn: int(AccessTokenTTL.Seconds())}, nil
}

func (t *TokenIssuer) sign(subject, typ string, now time.Time, ttl time.Duration) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Subject:   subject,
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Type: typ,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.key)
}

// Parse verifies a token's signature, issuer, audience and time claims.
func (t *TokenIssuer) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return t.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return nil, err
	}
	return claims, nil
}
