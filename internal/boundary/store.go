package boundary

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/boundary/boundarydb"
)

// ErrNotSeeded is returned by LoadTree when admin_units is empty.
var ErrNotSeeded = errors.New("boundary: admin_units is empty; run `make db-seed`")

// SeedResult summarizes a seed run.
type SeedResult struct {
	Units map[Level]int
}

// Seed upserts units into admin_units in one transaction, parents first, and
// commits only if the table then holds exactly the expected count per level.
// Re-running with the same data is a no-op.
func Seed(ctx context.Context, pool *pgxpool.Pool, version string, units []Unit, want map[Level]int) (SeedResult, error) {
	if err := CheckCounts(units, want); err != nil {
		return SeedResult{}, err
	}
	// Validates the tree shape before any write.
	if _, err := NewTree(version, units); err != nil {
		return SeedResult{}, err
	}

	var res SeedResult
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := boundarydb.New(tx)
		for _, level := range []Level{LevelNational, LevelCounty, LevelConstituency, LevelWard} {
			for _, u := range units {
				if u.Level != level {
					continue
				}
				p := boundarydb.UpsertAdminUnitParams{
					Level:         int16(u.Level),
					Code:          int32(u.Code),
					IebcCode:      u.IEBCCode,
					Name:          u.Name,
					DisplayName:   u.DisplayName,
					SourceVersion: version,
				}
				if u.Level != LevelNational {
					p.ParentLevel = pgtype.Int2{Int16: int16(u.Level + 1), Valid: true}
					p.ParentCode = pgtype.Int4{Int32: int32(u.ParentCode), Valid: true}
				}
				if u.Level == LevelWard {
					p.RegisteredVoters = pgtype.Int4{Int32: int32(u.RegisteredVoters), Valid: true}
				}
				if err := q.UpsertAdminUnit(ctx, p); err != nil {
					return fmt.Errorf("upsert %s %d: %w", u.Level, u.Code, err)
				}
			}
		}
		counts, err := q.CountAdminUnitsByLevel(ctx)
		if err != nil {
			return err
		}
		res.Units = map[Level]int{}
		for _, c := range counts {
			res.Units[Level(c.Level)] = int(c.Units)
		}
		for l, n := range want {
			if res.Units[l] != n {
				return fmt.Errorf("after seeding, %s has %d units, want %d (stale rows from another boundary set?)", l, res.Units[l], n)
			}
		}
		return nil
	})
	if err != nil {
		return SeedResult{}, fmt.Errorf("boundary: seed: %w", err)
	}
	return res, nil
}

// LoadTree reads admin_units into an in-memory tree.
func LoadTree(ctx context.Context, pool *pgxpool.Pool) (*Tree, error) {
	rows, err := boundarydb.New(pool).ListAdminUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("boundary: load: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotSeeded
	}
	units := make([]Unit, 0, len(rows))
	for _, r := range rows {
		units = append(units, Unit{
			Level:            Level(r.Level),
			Code:             int(r.Code),
			IEBCCode:         r.IebcCode,
			Name:             r.Name,
			DisplayName:      r.DisplayName,
			ParentCode:       int(r.ParentCode.Int32),
			RegisteredVoters: int(r.RegisteredVoters.Int32),
		})
	}
	return NewTree(rows[0].SourceVersion, units)
}
