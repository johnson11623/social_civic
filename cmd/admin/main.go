// Command admin grants platform roles (sysadmin, dpo, appeals_member) from
// the operator's shell. It bootstraps the first sysadmin, who can then
// appoint moderators through the API.
//
//	admin -database postgres://... -user <public id> -role sysadmin
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/johnson11623/social_civic/internal/membership"
)

func main() {
	dbURL := flag.String("database", os.Getenv("DATABASE_URL"), "PostgreSQL URL (default $DATABASE_URL)")
	user := flag.String("user", "", "user public id")
	role := flag.String("role", membership.RoleSysadmin, "platform role: sysadmin, dpo or appeals_member")
	flag.Parse()
	if err := run(*dbURL, *user, *role); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run(dbURL, user, role string) error {
	if dbURL == "" {
		return fmt.Errorf("-database or DATABASE_URL is required")
	}
	if !membership.PlatformRoles[role] {
		return fmt.Errorf("-role must be one of sysadmin, dpo, appeals_member (moderators are appointed via POST /v1/moderators)")
	}
	id, err := uuid.Parse(user)
	if err != nil {
		return fmt.Errorf("-user must be a user public id: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	store := membership.NewStore(pool)
	m, err := store.MemberByPublicID(ctx, id)
	if err != nil {
		return err
	}
	a, err := store.Appoint(ctx, m, role, 0, 0, 0, nil)
	if err != nil {
		return err
	}
	fmt.Printf("granted %s to %s (assignment %s)\n", role, id, a.PublicID)
	return nil
}
