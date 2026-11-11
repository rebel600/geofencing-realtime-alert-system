// Package store is the PostgreSQL/PostGIS persistence layer.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a referenced row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a uniqueness constraint is violated.
var ErrConflict = errors.New("conflict")

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres, retrying while the database starts up, then runs migrations.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	// Works behind PgBouncer-style transaction poolers (e.g. Supabase port 6543),
	// which do not support server-side prepared statements.
	if strings.Contains(databaseURL, "pgbouncer=true") || strings.Contains(databaseURL, ":6543") {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}

	var pool *pgxpool.Pool
	for attempt := 1; ; attempt++ {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			err = pool.Ping(ctx)
		}
		if err == nil {
			break
		}
		if pool != nil {
			pool.Close()
		}
		if attempt >= 30 {
			return nil, fmt.Errorf("connect to database: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// migrate applies embedded *.sql files in lexical order, each once, inside a
// transaction guarded by an advisory lock so concurrent replicas don't race.
func (s *Store) migrate(ctx context.Context) error {
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(727274)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, f := range files {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, f).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, f); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Page is optional limit/offset pagination. Limit 0 means "no limit".
type Page struct {
	Limit  int
	Offset int
}

func (p Page) sql(args *[]any) string {
	var b strings.Builder
	if p.Limit > 0 {
		*args = append(*args, p.Limit)
		fmt.Fprintf(&b, " LIMIT $%d", len(*args))
	}
	if p.Offset > 0 {
		*args = append(*args, p.Offset)
		fmt.Fprintf(&b, " OFFSET $%d", len(*args))
	}
	return b.String()
}

// where builds a "WHERE a AND b" clause from optional filters.
type where struct {
	clauses []string
	args    []any
}

func (w *where) add(clause string, value any) {
	w.args = append(w.args, value)
	w.clauses = append(w.clauses, fmt.Sprintf(clause, len(w.args)))
}

func (w *where) String() string {
	if len(w.clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.clauses, " AND ")
}
