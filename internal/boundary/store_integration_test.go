package boundary

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs against a real, migrated PostgreSQL when TEST_DATABASE_URL is set
// (`make test-integration`); otherwise skips.
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make test-integration`")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "TRUNCATE channels CASCADE; DELETE FROM admin_units WHERE level = 1; DELETE FROM admin_units WHERE level = 2; DELETE FROM admin_units WHERE level = 3; DELETE FROM admin_units"); err != nil {
		t.Fatalf("reset admin_units (is the database migrated?): %v", err)
	}
	return pool
}

func TestSeedIntegration(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()

	if _, err := LoadTree(ctx, pool); !errors.Is(err, ErrNotSeeded) {
		t.Fatalf("LoadTree on empty table err = %v, want ErrNotSeeded", err)
	}

	units, err := LoadIEBCCSVFile(seedCSV)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Seed(ctx, pool, IEBC2022, units, ExpectedIEBC2022)
	if err != nil {
		t.Fatal(err)
	}
	for l, n := range ExpectedIEBC2022 {
		if res.Units[l] != n {
			t.Errorf("%s: seeded %d, want %d", l, res.Units[l], n)
		}
	}

	// Idempotent: a second run rewrites nothing.
	var before string
	if err := pool.QueryRow(ctx, "SELECT max(updated_at)::text FROM admin_units").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := Seed(ctx, pool, IEBC2022, units, ExpectedIEBC2022); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var after string
	var total int
	if err := pool.QueryRow(ctx, "SELECT max(updated_at)::text, count(*) FROM admin_units").Scan(&after, &total); err != nil {
		t.Fatal(err)
	}
	if after != before || total != 1788 {
		t.Errorf("re-seed changed rows: updated_at %s → %s, total %d", before, after, total)
	}

	// Round trip: the tree loaded from the database matches the CSV.
	tree, err := LoadTree(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Version != IEBC2022 || tree.Len() != 1788 {
		t.Errorf("loaded tree version=%s len=%d", tree.Version, tree.Len())
	}
	s, err := tree.ResolveWard(17)
	if err != nil || s.Ward.DisplayName != "Ziwa la Ng'ombe" || s.Constituency.Code != 4 || s.County.Code != 1 {
		t.Errorf("ward 17 = %+v, %v", s, err)
	}
	if w, _ := tree.Unit(LevelWard, 17); w.RegisteredVoters != 23296 {
		t.Errorf("ward 17 voters = %d", w.RegisteredVoters)
	}
}

func TestSeedIntegrationRejectsIncompleteData(t *testing.T) {
	pool := integrationPool(t)
	units, err := LoadIEBCCSVFile(seedCSV)
	if err != nil {
		t.Fatal(err)
	}
	// Drop the last ward: counts no longer match, nothing may be written.
	if _, err := Seed(context.Background(), pool, IEBC2022, units[:len(units)-1], ExpectedIEBC2022); err == nil {
		t.Fatal("expected error for incomplete data")
	}
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM admin_units").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("admin_units has %d rows after a rejected seed", n)
	}
}
