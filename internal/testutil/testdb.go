package testutil

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // register "pgx" driver for database/sql (pgtestdb / goose)
	"github.com/peterldowns/pgtestdb"
	"github.com/peterldowns/pgtestdb/migrators/goosemigrator"
	"github.com/stretchr/testify/require"
)

// DSNInfo splits a pgtestdb DSN (postgres://user:pass@host:port/db?sslmode=…)
// into the individual fields used by config.DatabaseConfig. Panics on invalid
// URL — it is test-only plumbing.
type DSNInfo struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// ParseDSN extracts host/port/credentials from a standard postgres URL.
func ParseDSN(t *testing.T, dsn string) DSNInfo {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	pass, _ := u.User.Password()
	return DSNInfo{
		Host:     u.Hostname(),
		Port:     port,
		User:     u.User.Username(),
		Password: pass,
		Database: u.Path[1:],
		SSLMode:  u.Query().Get("sslmode"),
	}
}

// ModuleRoot returns the repository root (directory containing go.mod), assuming
// this file lives in internal/testutil/.
func ModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// PGTestDBConfig builds admin connection settings for github.com/peterldowns/pgtestdb.
// Defaults match compose.dev.yml service pgtestdb (localhost:5433, postgres/password).
//
// Environment overrides: PGTESTDB_HOST, PGTESTDB_PORT, PGTESTDB_USER, PGTESTDB_PASSWORD.
func PGTestDBConfig() pgtestdb.Config {
	return pgtestdb.Config{
		DriverName: "pgx",
		Host:       getenv("PGTESTDB_HOST", "localhost"),
		Port:       getenv("PGTESTDB_PORT", "5433"),
		User:       getenv("PGTESTDB_USER", "postgres"),
		Password:   getenv("PGTESTDB_PASSWORD", "password"),
		Database:   "postgres",
		Options:    "sslmode=disable",
	}
}

// NewPool provisions an isolated database (schema from migrations/ via goose),
// returns a pgx pool, and registers t.Cleanup to close the pool.
//
// Skips the test when PGTESTDB_SKIP=1 or when the admin Postgres endpoint is
// unreachable (e.g. docker compose pgtestdb not started).
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("PGTESTDB_SKIP") == "1" {
		t.Skip("PGTESTDB_SKIP=1 — skipping integration tests that need Postgres")
	}

	conf := PGTestDBConfig()
	base, err := conf.Connect()
	if err != nil {
		t.Skipf("pgtestdb: cannot reach %s:%s — %v (start: docker compose -f compose.dev.yml up -d pgtestdb)",
			conf.Host, conf.Port, err)
	}
	require.NoError(t, base.Close())

	root := ModuleRoot(t)
	migrator := goosemigrator.New(
		"migrations",
		goosemigrator.WithFS(os.DirFS(root)),
	)

	dbc := pgtestdb.Custom(t, conf, migrator)

	pool, err := pgxpool.New(context.Background(), dbc.URL())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// NewDSN provisions the same isolated database as NewPool but returns only the
// connection URL — useful to test packages that accept a DSN (e.g. database).
func NewDSN(t *testing.T) string {
	t.Helper()
	if os.Getenv("PGTESTDB_SKIP") == "1" {
		t.Skip("PGTESTDB_SKIP=1 — skipping integration tests that need Postgres")
	}
	conf := PGTestDBConfig()
	base, err := conf.Connect()
	if err != nil {
		t.Skipf("pgtestdb: cannot reach %s:%s — %v (start: docker compose -f compose.dev.yml up -d pgtestdb)",
			conf.Host, conf.Port, err)
	}
	require.NoError(t, base.Close())

	root := ModuleRoot(t)
	migrator := goosemigrator.New("migrations", goosemigrator.WithFS(os.DirFS(root)))
	dbc := pgtestdb.Custom(t, conf, migrator)
	return dbc.URL()
}
