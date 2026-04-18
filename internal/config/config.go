package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// defaultJWTSecret is the insecure development placeholder. It is rejected when
// APP_ENV is "production" to prevent accidental shipment of an unsafe default.
const defaultJWTSecret = "change-me-in-production"

type Config struct {
	App      AppConfig
	Server   ServerConfig
	Database DatabaseConfig
	Auth     AuthConfig
	BankFeed BankFeedConfig
}

type AppConfig struct {
	Name        string
	Environment string
	LogLevel    string
}

type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxConnections  int32
	MinConnections  int32
	MaxConnLifetime time.Duration
}

type AuthConfig struct {
	JWTSecret        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	TenantHeaderName string
}

// BankFeedConfig holds credentials + defaults for Open Banking aggregators.
// Empty secrets mean the adapter is not registered at boot.
type BankFeedConfig struct {
	RedirectURL            string        // where providers send the browser after consent
	SyncWindow             time.Duration // how far back to pull per /sync call
	GoCardlessBADSecretID  string
	GoCardlessBADSecretKey string
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	port, err := intEnv("SERVER_PORT", 8080)
	if err != nil {
		return nil, err
	}
	dbPort, err := intEnv("DB_PORT", 5432)
	if err != nil {
		return nil, err
	}
	maxConns, err := intEnv("DB_MAX_CONNECTIONS", 25)
	if err != nil {
		return nil, err
	}
	minConns, err := intEnv("DB_MIN_CONNECTIONS", 5)
	if err != nil {
		return nil, err
	}
	readTimeout, err := durationEnv("SERVER_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	writeTimeout, err := durationEnv("SERVER_WRITE_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	dbLifetime, err := durationEnv("DB_MAX_CONN_LIFETIME", time.Hour)
	if err != nil {
		return nil, err
	}
	accessTTL, err := durationEnv("JWT_ACCESS_TTL", time.Hour)
	if err != nil {
		return nil, err
	}
	refreshTTL, err := durationEnv("JWT_REFRESH_TTL", 720*time.Hour)
	if err != nil {
		return nil, err
	}

	syncWindow, err := durationEnv("BANKFEED_SYNC_WINDOW", 90*24*time.Hour)
	if err != nil {
		return nil, err
	}

	env := getEnv("APP_ENV", "development")
	jwtSecret := getEnv("JWT_SECRET", defaultJWTSecret)
	if env == "production" && jwtSecret == defaultJWTSecret {
		return nil, errors.New("JWT_SECRET must be set to a non-default value when APP_ENV=production")
	}

	return &Config{
		App: AppConfig{
			Name:        getEnv("APP_NAME", "goxero"),
			Environment: env,
			LogLevel:    getEnv("LOG_LEVEL", "info"),
		},
		Server: ServerConfig{
			Host:         getEnv("SERVER_HOST", "0.0.0.0"),
			Port:         port,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		},
		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            dbPort,
			User:            getEnv("DB_USER", "goxero"),
			Password:        getEnv("DB_PASSWORD", "goxero"),
			Name:            getEnv("DB_NAME", "goxero"),
			SSLMode:         getEnv("DB_SSL_MODE", "disable"),
			MaxConnections:  int32(maxConns),
			MinConnections:  int32(minConns),
			MaxConnLifetime: dbLifetime,
		},
		Auth: AuthConfig{
			JWTSecret:        jwtSecret,
			AccessTokenTTL:   accessTTL,
			RefreshTokenTTL:  refreshTTL,
			TenantHeaderName: getEnv("TENANT_HEADER", "Xero-Tenant-Id"),
		},
		BankFeed: BankFeedConfig{
			RedirectURL:            getEnv("BANKFEED_REDIRECT_URL", "http://localhost:5173/app/bank-feeds/callback"),
			SyncWindow:             syncWindow,
			GoCardlessBADSecretID:  getEnv("GOCARDLESS_BAD_SECRET_ID", ""),
			GoCardlessBADSecretKey: getEnv("GOCARDLESS_BAD_SECRET_KEY", ""),
		},
	}, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return d, nil
}
