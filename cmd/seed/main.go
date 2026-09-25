// Command seed loads reference data into the database.
//
//	seed -database postgres://... [-boundary db/seed/iebc_2022_wards.csv]
//
// It is idempotent: re-running with the same data changes nothing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/post"
)

func main() {
	dbURL := flag.String("database", os.Getenv("DATABASE_URL"), "PostgreSQL URL (default $DATABASE_URL)")
	boundaryCSV := flag.String("boundary", "db/seed/iebc_2022_wards.csv", "IEBC boundary CSV")
	flag.Parse()

	if err := run(*dbURL, *boundaryCSV); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(dbURL, boundaryCSV string) error {
	if dbURL == "" {
		return fmt.Errorf("-database or DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	units, err := boundary.LoadIEBCCSVFile(boundaryCSV)
	if err != nil {
		return err
	}
	start := time.Now()
	res, err := boundary.Seed(ctx, pool, boundary.IEBC2022, units, boundary.ExpectedIEBC2022)
	if err != nil {
		return err
	}
	fmt.Printf("boundary %s seeded in %s: %d counties, %d constituencies, %d wards\n",
		boundary.IEBC2022, time.Since(start).Round(time.Millisecond),
		res.Units[boundary.LevelCounty], res.Units[boundary.LevelConstituency], res.Units[boundary.LevelWard])

	created, err := post.EnsureGeneralChannels(ctx, pool)
	if err != nil {
		return fmt.Errorf("general channels: %w", err)
	}
	fmt.Printf("#general channels: %d created (one per ward)\n", created)
	return nil
}
