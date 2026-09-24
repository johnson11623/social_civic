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

	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Token parse errors.
var (
	ErrTokenExpired = errors.New("identity: token expired")
	ErrTokenInvalid = errors.New("identity: token invalid")
)

// ScopeClaim is the user's administrative scope (T-1.1.2.6), so services can
// authorize feed and posting requests without a database lookup.
type ScopeClaim struct {
	Ward         int `json:"ward"`
	Constituency int `json:"constituency"`
	County       int `json:"county"`
}

// Claims are the platform's JWT claims (F-03: jti, aud, iss, nbf, exp).
type Claims struct {
	jwt.RegisteredClaims
	Type  string      `json:"typ"`             // "access" or "refresh"
	Scope *ScopeClaim `json:"scope,omitempty"` // access tokens only
}

// TokenPair is returned to clients on registration, login and refresh.
type TokenPair struct {
	Access           string
	Refresh          string
	ExpiresIn        int // access token lifetime in seconds
	IssuedAt         time.Time
	RefreshJTI       uuid.UUID
	RefreshExpiresAt time.Time
}

// TokenIssuer signs access and refresh tokens with HS256.
// Asymmetric signing keys from KMS come with T-X.4.
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

// Issue creates an access/refresh pair for the user's public id. The caller
// must persist RefreshJTI (see Sessions) or the refresh token will be rejected.
func (t *TokenIssuer) Issue(subject string, scope ScopeClaim) (TokenPair, error) {
	now := t.now().Truncate(time.Second) // JWT times have second precision
	access, _, err := t.sign(subject, TokenTypeAccess, now, AccessTokenTTL, &scope)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, jti, err := t.sign(subject, TokenTypeRefresh, now, RefreshTokenTTL, nil)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		Access:           access,
		Refresh:          refresh,
		ExpiresIn:        int(AccessTokenTTL.Seconds()),
		IssuedAt:         now,
		RefreshJTI:       jti,
		RefreshExpiresAt: now.Add(RefreshTokenTTL),
	}, nil
}

func (t *TokenIssuer) sign(subject, typ string, now time.Time, ttl time.Duration, scope *ScopeClaim) (string, uuid.UUID, error) {
	jti := uuid.Must(uuid.NewV7())
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti.String(),
			Subject:   subject,
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Type:  typ,
		Scope: scope,
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.key)
	return s, jti, err
}

// Parse verifies signature, issuer, audience, time claims and token type.
func (t *TokenIssuer) Parse(token, wantType string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims,
		func(*jwt.Token) (any, error) { return t.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return nil, ErrTokenExpired
	case err != nil:
		return nil, errors.Join(ErrTokenInvalid, err)
	case claims.Type != wantType:
		return nil, ErrTokenInvalid
	}
	if _, err := uuid.Parse(claims.ID); err != nil {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}
