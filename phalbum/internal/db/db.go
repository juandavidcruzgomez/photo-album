// Package db holds small Postgres helpers shared by the packages that own
// tables.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schemaLock is the advisory lock key held while applying schemas, so
// programs starting at the same time don't race on DDL.
const schemaLock = 7_270_001

// ApplySchema runs an idempotent schema script under the schema lock.
func ApplySchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", schemaLock); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, schema)
		return err
	})
	if err != nil {
		return fmt.Errorf("applying schema: %w", err)
	}
	return nil
}
