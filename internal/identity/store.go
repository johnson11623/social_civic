package identity

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/identity/identitydb"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// ErrDuplicateNationalID is returned when the national ID hash is already registered.
var ErrDuplicateNationalID = errors.New("identity: national ID already registered")

// NewUser is everything persisted for a registration.
type NewUser struct {
	PublicID       uuid.UUID
	NationalIDHash []byte
	KeyVersion     string
	DisplayName    string
	PreferredLang  string
	WardID         int32
	ConstituencyID int32
	CountyID       int32
	ConsentVersion string
	ConsentAt      time.Time
	IPHash         []byte

	MSISDNCiphertext []byte // AES-256-GCM, see pkg/pii
	MSISDNKeyVersion string
	MSISDNHash       []byte
}

// CreatedUser is the stored user's identifiers.
type CreatedUser struct {
	ID        int64
	PublicID  uuid.UUID
	CreatedAt time.Time
}

// Store persists identity data.
type Store interface {
	// CreateUser inserts the user, their consent, and the event returned by
	// event (built from the new ids) into the outbox, atomically.
	CreateUser(ctx context.Context, u NewUser, event func(CreatedUser) events.Event) (CreatedUser, error)
}

// PostgresStore implements Store on PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps a connection pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// CreateUser implements Store. The unique index on national_id_hash is the
// duplicate check, so concurrent registrations of one ID cannot both succeed.
func (s *PostgresStore) CreateUser(ctx context.Context, u NewUser, event func(CreatedUser) events.Event) (CreatedUser, error) {
	var created CreatedUser
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		row, err := q.InsertUser(ctx, identitydb.InsertUserParams{
			PublicID:             u.PublicID,
			NationalIDHash:       u.NationalIDHash,
			NationalIDKeyVersion: u.KeyVersion,
			DisplayName:          u.DisplayName,
			PreferredLang:        u.PreferredLang,
			WardID:               u.WardID,
			ConstituencyID:       u.ConstituencyID,
			CountyID:             u.CountyID,
			MsisdnCiphertext:     u.MSISDNCiphertext,
			MsisdnKeyVersion:     pgtype.Text{String: u.MSISDNKeyVersion, Valid: u.MSISDNKeyVersion != ""},
			MsisdnHash:           u.MSISDNHash,
		})
		if err != nil {
			return err
		}
		if err := q.InsertConsent(ctx, identitydb.InsertConsentParams{
			UserID:    row.ID,
			Version:   u.ConsentVersion,
			GrantedAt: u.ConsentAt,
			IpHash:    u.IPHash,
		}); err != nil {
			return err
		}
		created = CreatedUser{ID: row.ID, PublicID: row.PublicID, CreatedAt: row.CreatedAt}
		return outbox.Enqueue(ctx, tx, events.TopicUserRegistered, strconv.FormatInt(row.ID, 10), event(created))
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_national_id_hash_key" {
		return CreatedUser{}, ErrDuplicateNationalID
	}
	if err != nil {
		return CreatedUser{}, err
	}
	return created, nil
}

// Login and session errors.
var (
	ErrUserNotFound    = errors.New("identity: user not found")
	ErrNoActiveOTP     = errors.New("identity: no active login code")
	ErrOTPExpired      = errors.New("identity: login code expired")
	ErrOTPMismatch     = errors.New("identity: login code does not match")
	ErrRefreshNotFound = errors.New("identity: refresh token unknown")
	ErrRefreshRevoked  = errors.New("identity: refresh token revoked")
	ErrRefreshReused   = errors.New("identity: rotated refresh token reused")
	ErrRefreshExpired  = errors.New("identity: refresh token expired")
	ErrUserInactive    = errors.New("identity: user is not active")
)

// User states (users.state).
const (
	UserActive    = 1
	UserSuspended = 2
	UserErased    = 3
)

// LoginUser is what login needs to know about a user.
type LoginUser struct {
	ID               int64
	PublicID         uuid.UUID
	State            int16
	PreferredLang    string
	Scope            ScopeClaim
	MSISDNCiphertext []byte
	MSISDNKeyVersion string
}

// SessionUser is the user behind a refresh token.
type SessionUser struct {
	ID       int64
	PublicID uuid.UUID
	Scope    ScopeClaim
}

// Sessions persists refresh tokens.
type Sessions interface {
	// SaveRefreshToken records pair's refresh token as the start of, or next
	// token in, family.
	SaveRefreshToken(ctx context.Context, userID int64, family uuid.UUID, pair TokenPair) error
}

// AuthStore is the persistence behind login and refresh.
type AuthStore interface {
	Sessions
	FindUserByNationalIDHash(ctx context.Context, hash []byte) (LoginUser, error)
	// CreateOTP stores a new login code and invalidates any earlier one.
	CreateOTP(ctx context.Context, userID int64, codeHash []byte, keyVersion string, createdAt, expiresAt time.Time) error
	// VerifyOTP checks the user's active code with match; a mismatch counts an
	// attempt and locks the code after maxAttempts. A match consumes it.
	VerifyOTP(ctx context.Context, userID int64, now time.Time, maxAttempts int, match func(codeHash []byte) bool) error
	// RotateRefreshToken revokes jti and stores the pair returned by issue in
	// the same family. Presenting an already-rotated token revokes every
	// session of the user (F-03 reuse detection) and returns ErrRefreshReused.
	RotateRefreshToken(ctx context.Context, jti uuid.UUID, now time.Time, issue func(SessionUser) (TokenPair, error)) (TokenPair, error)
}

// SaveRefreshToken implements Sessions.
func (s *PostgresStore) SaveRefreshToken(ctx context.Context, userID int64, family uuid.UUID, pair TokenPair) error {
	return identitydb.New(s.pool).InsertRefreshToken(ctx, identitydb.InsertRefreshTokenParams{
		Jti: pair.RefreshJTI, UserID: userID, FamilyID: family,
		IssuedAt: pair.IssuedAt, ExpiresAt: pair.RefreshExpiresAt,
	})
}

// FindUserByNationalIDHash implements AuthStore.
func (s *PostgresStore) FindUserByNationalIDHash(ctx context.Context, hash []byte) (LoginUser, error) {
	row, err := identitydb.New(s.pool).GetUserByNationalIDHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return LoginUser{}, ErrUserNotFound
	}
	if err != nil {
		return LoginUser{}, err
	}
	return LoginUser{
		ID: row.ID, PublicID: row.PublicID, State: row.State, PreferredLang: row.PreferredLang,
		Scope:            ScopeClaim{Ward: int(row.WardID), Constituency: int(row.ConstituencyID), County: int(row.CountyID)},
		MSISDNCiphertext: row.MsisdnCiphertext, MSISDNKeyVersion: row.MsisdnKeyVersion.String,
	}, nil
}

// CreateOTP implements AuthStore.
func (s *PostgresStore) CreateOTP(ctx context.Context, userID int64, codeHash []byte, keyVersion string, createdAt, expiresAt time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		if err := q.InvalidateActiveOTPs(ctx, userID); err != nil {
			return err
		}
		_, err := q.InsertOTPChallenge(ctx, identitydb.InsertOTPChallengeParams{
			UserID: userID, CodeHash: codeHash, KeyVersion: keyVersion, CreatedAt: createdAt, ExpiresAt: expiresAt,
		})
		return err
	})
}

// VerifyOTP implements AuthStore. The row lock serializes concurrent attempts,
// so a code can be consumed once and attempts cannot be raced past the limit.
func (s *PostgresStore) VerifyOTP(ctx context.Context, userID int64, now time.Time, maxAttempts int, match func([]byte) bool) error {
	var result error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		otp, err := q.GetActiveOTPForUpdate(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			result = ErrNoActiveOTP
			return nil
		}
		if err != nil {
			return err
		}
		if !now.Before(otp.ExpiresAt) {
			result = ErrOTPExpired
			return q.InvalidateActiveOTPs(ctx, userID)
		}
		if !match(otp.CodeHash) {
			result = ErrOTPMismatch
			_, err := q.RecordFailedOTPAttempt(ctx, identitydb.RecordFailedOTPAttemptParams{ID: otp.ID, MaxAttempts: int32(maxAttempts)})
			return err
		}
		n, err := q.ConsumeOTP(ctx, otp.ID)
		if err != nil {
			return err
		}
		if n != 1 {
			result = ErrNoActiveOTP
		}
		return nil
	})
	if err != nil {
		return err
	}
	return result
}

// RotateRefreshToken implements AuthStore.
func (s *PostgresStore) RotateRefreshToken(ctx context.Context, jti uuid.UUID, now time.Time, issue func(SessionUser) (TokenPair, error)) (TokenPair, error) {
	var pair TokenPair
	var result error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := identitydb.New(tx)
		tok, err := q.GetRefreshTokenForUpdate(ctx, jti)
		if errors.Is(err, pgx.ErrNoRows) {
			result = ErrRefreshNotFound
			return nil
		}
		if err != nil {
			return err
		}
		if tok.RevokedAt != nil {
			if tok.RevokeReason.String == "rotated" {
				// A rotated token came back: it was stolen or replayed. Kill every session.
				result = ErrRefreshReused
				_, err := q.RevokeAllUserRefreshTokens(ctx, identitydb.RevokeAllUserRefreshTokensParams{UserID: tok.UserID, Reason: "reuse_detected"})
				return err
			}
			result = ErrRefreshRevoked
			return nil
		}
		if !now.Before(tok.ExpiresAt) {
			result = ErrRefreshExpired
			return nil
		}
		u, err := q.GetUserByID(ctx, tok.UserID)
		if err != nil {
			return err
		}
		if u.State != UserActive {
			result = ErrUserInactive
			return nil
		}
		pair, err = issue(SessionUser{
			ID: u.ID, PublicID: u.PublicID,
			Scope: ScopeClaim{Ward: int(u.WardID), Constituency: int(u.ConstituencyID), County: int(u.CountyID)},
		})
		if err != nil {
			return err
		}
		if err := q.RotateRefreshToken(ctx, identitydb.RotateRefreshTokenParams{Jti: jti, ReplacedBy: pgtype.UUID{Bytes: pair.RefreshJTI, Valid: true}}); err != nil {
			return err
		}
		return q.InsertRefreshToken(ctx, identitydb.InsertRefreshTokenParams{
			Jti: pair.RefreshJTI, UserID: u.ID, FamilyID: tok.FamilyID,
			IssuedAt: pair.IssuedAt, ExpiresAt: pair.RefreshExpiresAt,
		})
	})
	if err != nil {
		return TokenPair{}, err
	}
	if result != nil {
		return TokenPair{}, result
	}
	return pair, nil
}
