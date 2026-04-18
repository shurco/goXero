package middleware

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type contextKey string

const (
	CtxUserID       contextKey = "userID"
	CtxUserEmail    contextKey = "userEmail"
	CtxOrganisation contextKey = "organisation"
)

type Claims struct {
	UserID uuid.UUID `json:"uid"`
	Email  string    `json:"email"`
	jwt.RegisteredClaims
}

func IssueToken(cfg config.AuthConfig, userID uuid.UUID, email string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "goxero",
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(cfg.AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(cfg.JWTSecret))
}

// NewRefreshToken returns a fresh opaque refresh token as (raw, hash) where
// `raw` is a 32-byte base64url-encoded string handed to the client exactly
// once, and `hash` is the SHA-256 hex digest that is safe to persist.
// Using an opaque token (instead of a second JWT) lets us revoke / rotate on
// the server without chasing every signed artefact the client may still hold.
func NewRefreshToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hash = HashRefreshToken(raw)
	return raw, hash, nil
}

// HashRefreshToken returns the SHA-256 hex digest used to look refresh tokens
// up in the database. Deterministic so callers can compare safely without the
// raw secret ever touching the DB log.
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func ParseToken(cfg config.AuthConfig, raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// JWTAuth validates the Bearer token and stores the user on the Fiber context.
// Parsing errors are masked: the client never sees JWT library internals.
func JWTAuth(cfg config.AuthConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing Authorization header")
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid Authorization header")
		}
		claims, err := ParseToken(cfg, parts[1])
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}
		c.Locals(string(CtxUserID), claims.UserID)
		c.Locals(string(CtxUserEmail), claims.Email)
		return c.Next()
	}
}

// OptionalJWTAuth is a best-effort variant of JWTAuth: if a valid Bearer token
// is present it populates the request context exactly like `JWTAuth`, but a
// missing / expired / malformed token is silently ignored so the handler can
// still run anonymously. Used by `/api/auth/logout` where the caller may be
// hitting the endpoint after the access token has already expired.
func OptionalJWTAuth(cfg config.AuthConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return c.Next()
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return c.Next()
		}
		claims, err := ParseToken(cfg, parts[1])
		if err != nil {
			return c.Next()
		}
		c.Locals(string(CtxUserID), claims.UserID)
		c.Locals(string(CtxUserEmail), claims.Email)
		return c.Next()
	}
}

// Tenant resolves the organisation from the tenant header (preferred, matches
// Xero's `Xero-Tenant-Id`) or from `?organisationId=`. It ALWAYS verifies that
// the authenticated user is a member of the organisation — without this guard
// any valid JWT could read another tenant's data.
func Tenant(cfg config.AuthConfig, repos *repository.Repositories) fiber.Handler {
	return func(c fiber.Ctx) error {
		raw := c.Get(cfg.TenantHeaderName)
		if raw == "" {
			raw = c.Query("organisationId")
		}
		if raw == "" {
			return fiber.NewError(fiber.StatusBadRequest, "missing tenant id ("+cfg.TenantHeaderName+" header or organisationId query)")
		}
		orgID, err := uuid.Parse(raw)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid tenant id")
		}

		userID := UserIDFrom(c)
		if userID == uuid.Nil {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
		}
		ok, err := repos.Users.HasOrganisationAccess(c.Context(), userID, orgID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "failed to verify tenant")
		}
		if !ok {
			return fiber.NewError(fiber.StatusForbidden, "no access to organisation")
		}

		org, err := repos.Organisations.GetByID(c.Context(), orgID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return fiber.NewError(fiber.StatusNotFound, "organisation not found")
			}
			return fiber.NewError(fiber.StatusInternalServerError, "failed to load tenant")
		}
		c.Locals(string(CtxOrganisation), org)
		return c.Next()
	}
}

func UserIDFrom(c fiber.Ctx) uuid.UUID {
	v, _ := c.Locals(string(CtxUserID)).(uuid.UUID)
	return v
}

func OrganisationFrom(c fiber.Ctx) *models.Organisation {
	v, _ := c.Locals(string(CtxOrganisation)).(*models.Organisation)
	return v
}

func OrganisationIDFrom(c fiber.Ctx) uuid.UUID {
	if o := OrganisationFrom(c); o != nil {
		return o.OrganisationID
	}
	return uuid.Nil
}
