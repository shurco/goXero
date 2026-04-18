package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{
		"SERVER_PORT", "DB_PORT", "DB_MAX_CONNECTIONS", "DB_MIN_CONNECTIONS",
		"SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "DB_MAX_CONN_LIFETIME",
		"JWT_ACCESS_TTL", "JWT_REFRESH_TTL", "APP_ENV", "JWT_SECRET",
	} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, 15*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, defaultJWTSecret, cfg.Auth.JWTSecret)
}

func TestLoadRejectsDefaultJWTInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", defaultJWTSecret)

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET")
}

func TestLoadRejectsInvalidInteger(t *testing.T) {
	t.Setenv("SERVER_PORT", "not-a-number")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SERVER_PORT")
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("SERVER_READ_TIMEOUT", "10x")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SERVER_READ_TIMEOUT")
}

func TestDSN(t *testing.T) {
	db := DatabaseConfig{Host: "h", Port: 1, User: "u", Password: "p", Name: "n", SSLMode: "disable"}
	want := "postgres://u:p@h:1/n?sslmode=disable"
	assert.Equal(t, want, db.DSN())
}
