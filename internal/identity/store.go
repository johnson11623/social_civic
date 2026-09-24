package identity

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
