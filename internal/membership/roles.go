// Package membership holds roles and moderator assignments (EPIC 1.2).
//
// Every user belongs to four groups: their ward, constituency, county and
// the nation. Those come from the scope on the user row; this package adds
// who governs each group.
package membership

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/membership/membershipdb"
	"github.com/johnson11623/social_civic/internal/platform/authn"
	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/outbox"
)

// Role codes (T-1.2.2.1). Moderator roles are bound to a level; the others
// are platform-wide.
const (
	RoleWardMod       = "ward_mod"
	RoleConstMod      = "const_mod"
	RoleCountyMod     = "county_mod"
	RoleNatMod        = "nat_mod"
	RoleAppealsMember = "appeals_member"
	RoleDPO           = "dpo"
	RoleSysadmin      = "sysadmin"
)

// ModeratorLevels maps each moderator role to the level it governs.
var ModeratorLevels = map[string]int16{RoleWardMod: 1, RoleConstMod: 2, RoleCountyMod: 3, RoleNatMod: 4}

// PlatformRoles are granted without a unit.
var PlatformRoles = map[string]bool{RoleAppealsMember: true, RoleDPO: true, RoleSysadmin: true}

// NationalUnit is the admin unit code of the national group.
const NationalUnit = 1

var (
	ErrAlreadyAssigned = errors.New("membership: already assigned")
	ErrUserNotFound    = errors.New("membership: no such active user")
)

// Assignment is a role in force.
type Assignment struct {
	PublicID    uuid.UUID
	Role        string
	UnitLevel   int16 // 0 for platform roles
	UnitCode    int32
	AppointedAt time.Time
	TermEnd     *time.Time
}

// Roles is a user's roles in force.
type Roles []Assignment

// Has reports whether the user holds the platform role code.
func (rs Roles) Has(code string) bool {
	for _, r := range rs {
		if r.Role == code && r.UnitLevel == 0 {
			return true
		}
	}
	return false
}

// CanModerate reports whether the roles give authority over a post at level
// whose origin is (ward, constituency, county) — T-3.1.2.3. Authority is
// tiered: a moderator acts on posts at their level or below, inside their
// unit. A ward moderator can't touch a post that has reached the county;
// the county moderator can act on any post in the county below or at it.
func (rs Roles) CanModerate(level int16, ward, constituency, county int32) bool {
	for _, r := range rs {
		roleLevel, ok := ModeratorLevels[r.Role]
		if !ok || roleLevel < level {
			continue
		}
		switch roleLevel {
		case 1:
			if r.UnitCode == ward {
				return true
			}
		case 2:
			if r.UnitCode == constituency {
				return true
			}
		case 3:
			if r.UnitCode == county {
				return true
			}
		case 4:
			return true
		}
	}
	return false
}

// ModeratorAppointedData is the payload of moderator.appointed (T-1.2.2.3).
// It carries the term so the audit trail records it (T-1.2.2.5).
type ModeratorAppointedData struct {
	AssignmentID string     `json:"assignment_id"`
	UserID       int64      `json:"user_id"`
	Role         string     `json:"role"`
	UnitLevel    int16      `json:"unit_level"`
	UnitCode     int32      `json:"unit_code"`
	AppointedBy  int64      `json:"appointed_by,omitempty"`
	AppointedAt  time.Time  `json:"appointed_at"`
	TermEnd      *time.Time `json:"term_end,omitempty"`
}

// Store persists role assignments.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Member is an active user and their groups.
type Member struct {
	ID                         int64
	PublicID                   uuid.UUID
	Ward, Constituency, County int32
}

// InGroup reports whether m belongs to the unit at level.
func (m Member) InGroup(level int16, unit int32) bool {
	switch level {
	case 1:
		return m.Ward == unit
	case 2:
		return m.Constituency == unit
	case 3:
		return m.County == unit
	default:
		return unit == NationalUnit
	}
}

// MemberByPublicID returns an active user.
func (s *Store) MemberByPublicID(ctx context.Context, id uuid.UUID) (Member, error) {
	u, err := membershipdb.New(s.pool).GetUserScopeByPublicID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrUserNotFound
	}
	return Member{ID: u.ID, PublicID: u.PublicID, Ward: u.WardID, Constituency: u.ConstituencyID, County: u.CountyID}, err
}

// Roles returns the user's roles in force.
func (s *Store) Roles(ctx context.Context, userID int64) (Roles, error) {
	rows, err := membershipdb.New(s.pool).ActiveRoles(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make(Roles, 0, len(rows))
	for _, r := range rows {
		out = append(out, Assignment{PublicID: r.PublicID, Role: r.RoleCode, UnitLevel: r.UnitLevel.Int16,
			UnitCode: r.UnitCode.Int32, AppointedAt: r.AppointedAt, TermEnd: r.TermEnd})
	}
	return out, nil
}

// Appoint records a role for user and enqueues moderator.appointed,
// atomically. by is 0 for grants from the admin CLI.
func (s *Store) Appoint(ctx context.Context, user Member, role string, unitLevel int16, unitCode int32, by int64, termEnd *time.Time) (Assignment, error) {
	a := Assignment{PublicID: uuid.Must(uuid.NewV7()), Role: role, UnitLevel: unitLevel, UnitCode: unitCode, TermEnd: termEnd}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := membershipdb.New(tx).InsertRoleAssignment(ctx, membershipdb.InsertRoleAssignmentParams{
			PublicID:    a.PublicID,
			UserID:      user.ID,
			RoleCode:    role,
			UnitLevel:   pgtype.Int2{Int16: unitLevel, Valid: unitLevel != 0},
			UnitCode:    pgtype.Int4{Int32: unitCode, Valid: unitLevel != 0},
			AppointedBy: pgtype.Int8{Int64: by, Valid: by != 0},
			TermEnd:     termEnd,
		})
		if err != nil {
			return err
		}
		a.AppointedAt = row.AppointedAt
		return outbox.Enqueue(ctx, tx, events.TopicModeratorAppointed, a.PublicID.String(),
			events.New("membership", events.TopicModeratorAppointed, a.AppointedAt, ModeratorAppointedData{
				AssignmentID: a.PublicID.String(), UserID: user.ID, Role: role, UnitLevel: unitLevel, UnitCode: unitCode,
				AppointedBy: by, AppointedAt: a.AppointedAt, TermEnd: termEnd,
			}))
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Assignment{}, ErrAlreadyAssigned
	}
	return a, err
}

// RevokeRolesStep is the erasure saga step that ends every role the user
// holds (identity.StepMembershipRevoke).
func RevokeRolesStep(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := membershipdb.New(tx).RevokeUserRoles(ctx, userID)
	return err
}

// ---- HTTP -------------------------------------------------------------------------

// Handlers serves /v1/moderators and /v1/users/me/roles behind authn.Middleware.
type Handlers struct {
	Store  *Store
	Logger *slog.Logger
	Now    func() time.Time
}

// AssignmentJSON is a role in API responses.
type AssignmentJSON struct {
	AssignmentID string     `json:"assignment_id"`
	Role         string     `json:"role"`
	Level        int16      `json:"level,omitempty"` // moderator roles
	UnitCode     int32      `json:"unit_code,omitempty"`
	AppointedAt  time.Time  `json:"appointed_at"`
	TermEnd      *time.Time `json:"term_end,omitempty"`
}

func toJSON(a Assignment) AssignmentJSON {
	return AssignmentJSON{AssignmentID: a.PublicID.String(), Role: a.Role, Level: a.UnitLevel, UnitCode: a.UnitCode,
		AppointedAt: a.AppointedAt, TermEnd: a.TermEnd}
}

// Caller resolves the authenticated user and their roles, or writes 401.
func (h *Handlers) Caller(w http.ResponseWriter, r *http.Request) (Member, Roles, bool) {
	p, ok := authn.FromContext(r.Context())
	id, err := uuid.Parse(p.Subject)
	if !ok || err != nil {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return Member{}, nil, false
	}
	m, err := h.Store.MemberByPublicID(r.Context(), id)
	if errors.Is(err, ErrUserNotFound) {
		problem.Write(w, r, http.StatusUnauthorized, "unauthenticated", i18n.MsgUnauthenticated)
		return Member{}, nil, false
	}
	if err != nil {
		h.internal(w, r, "caller", err)
		return Member{}, nil, false
	}
	roles, err := h.Store.Roles(r.Context(), m.ID)
	if err != nil {
		h.internal(w, r, "roles", err)
		return Member{}, nil, false
	}
	return m, roles, true
}

func (h *Handlers) internal(w http.ResponseWriter, r *http.Request, op string, err error) {
	h.Logger.ErrorContext(r.Context(), "membership failed", "op", op, "err", err)
	problem.Write(w, r, http.StatusInternalServerError, "internal_error", i18n.MsgInternal)
}

// MyRoles serves GET /v1/users/me/roles.
func (h *Handlers) MyRoles(w http.ResponseWriter, r *http.Request) {
	_, roles, ok := h.Caller(w, r)
	if !ok {
		return
	}
	items := make([]AssignmentJSON, 0, len(roles))
	for _, a := range roles {
		items = append(items, toJSON(a))
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"items": items})
}

// AppointRequest is the body of POST /v1/moderators.
type AppointRequest struct {
	UserID   string     `json:"user_id"`
	Role     string     `json:"role"`
	UnitCode int32      `json:"unit_code"` // ward/constituency/county code; ignored for nat_mod
	TermEnd  *time.Time `json:"term_end"`
}

// Appoint serves POST /v1/moderators — sysadmins only (T-1.2.2.2–T-1.2.2.5).
//
// F-08 asks for MFA on sysadmin actions; TOTP enrolment isn't built yet, so
// this relies on the sysadmin role alone for now.
func (h *Handlers) Appoint(w http.ResponseWriter, r *http.Request) {
	admin, roles, ok := h.Caller(w, r)
	if !ok {
		return
	}
	if !roles.Has(RoleSysadmin) {
		problem.Write(w, r, http.StatusForbidden, "insufficient_authority", i18n.MsgInsufficientAuthority)
		return
	}
	var req AppointRequest
	if err := httpjson.DecodeStrict(w, r, &req); err != nil {
		problem.Write(w, r, http.StatusBadRequest, "malformed_json", i18n.MsgMalformedJSON)
		return
	}
	level, ok := ModeratorLevels[req.Role]
	if !ok {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_role", i18n.MsgInvalidRole,
			problem.FieldError{Field: "role", Code: "invalid"})
		return
	}
	unit := req.UnitCode
	if level == 4 {
		unit = NationalUnit
	}
	if req.TermEnd != nil && !req.TermEnd.After(h.Now()) {
		problem.Write(w, r, http.StatusUnprocessableEntity, "invalid_term", i18n.MsgInvalidTerm,
			problem.FieldError{Field: "term_end", Code: "in_past"})
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		problem.Write(w, r, http.StatusNotFound, "user_not_found", i18n.MsgUserNotFound)
		return
	}
	user, err := h.Store.MemberByPublicID(r.Context(), userID)
	if errors.Is(err, ErrUserNotFound) {
		problem.Write(w, r, http.StatusNotFound, "user_not_found", i18n.MsgUserNotFound)
		return
	}
	if err != nil {
		h.internal(w, r, "load user", err)
		return
	}
	if !user.InGroup(level, unit) {
		problem.Write(w, r, http.StatusUnprocessableEntity, "not_in_group", i18n.MsgNotInGroup,
			problem.FieldError{Field: "unit_code", Code: "not_in_group"})
		return
	}
	a, err := h.Store.Appoint(r.Context(), user, req.Role, level, unit, admin.ID, req.TermEnd)
	switch {
	case errors.Is(err, ErrAlreadyAssigned):
		problem.Write(w, r, http.StatusConflict, "already_assigned", i18n.MsgAlreadyAssigned)
	case err != nil:
		h.internal(w, r, "appoint", err)
	default:
		out := toJSON(a)
		httpjson.Write(w, http.StatusCreated, map[string]any{"user_id": user.PublicID.String(), "assignment": out})
	}
}
