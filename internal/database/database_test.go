package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/testutil"
)

func TestNewPool_InvalidConfig(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host: "::::", Port: -1, User: "", Password: "", Name: "", SSLMode: "bogus",
	}
	_, err := NewPool(context.Background(), cfg)
	require.Error(t, err)
}

func TestNewPool_PingFailureOnUnreachableHost(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host: "127.0.0.1", Port: 1, User: "x", Password: "x", Name: "x",
		SSLMode: "disable", MaxConnections: 1, MinConnections: 1,
		MaxConnLifetime: time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_, err := NewPool(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping database")
}

func TestNewPool_Live(t *testing.T) {
	dsn := testutil.NewDSN(t)
	info := testutil.ParseDSN(t, dsn)

	cfg := config.DatabaseConfig{
		Host:            info.Host,
		Port:            info.Port,
		User:            info.User,
		Password:        info.Password,
		Name:            info.Database,
		SSLMode:         info.SSLMode,
		MaxConnections:  4,
		MinConnections:  1,
		MaxConnLifetime: 10 * time.Minute,
	}
	pool, err := NewPool(context.Background(), cfg)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(context.Background()))
}
