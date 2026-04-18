package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/repository"
)

type AuthHandler struct {
	cfg   config.AuthConfig
	repos *repository.Repositories
}

func NewAuthHandler(cfg config.AuthConfig, repos *repository.Repositories) *AuthHandler {
	return &AuthHandler{cfg: cfg, repos: repos}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// refreshRequest carries the raw opaque refresh token back for exchange. The
// field is also accepted from the `logout` endpoint so callers can revoke a
// specific device session without needing a JWT (e.g. on the logout screen
// after the access token has already expired).
type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// authResponse is what login/register/refresh all return. The timestamps are
// ISO-8601 so the SvelteKit client can parse them directly with `new Date()`.
type authResponse struct {
	Token                 string            `json:"token"`
	RefreshToken          string            `json:"refreshToken"`
	ExpiresAt             time.Time         `json:"expiresAt"`
	RefreshTokenExpiresAt time.Time         `json:"refreshTokenExpiresAt"`
	Email                 string            `json:"email"`
	Organisations         []tenantSummary   `json:"organisations"`
	User                  map[string]string `json:"user"`
}

type tenantSummary struct {
	OrganisationID string `json:"organisationId"`
	Name           string `json:"name"`
	ShortCode      string `json:"shortCode,omitempty"`
	BaseCurrency   string `json:"baseCurrency,omitempty"`
}

func (h *AuthHandler) Login(c fiber.Ctx) error {
	req, err := bindBody[loginRequest](c)
	if err != nil {
		return err
	}
	u, err := h.repos.Users.GetByEmail(c.Context(), req.Email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
		}
		return httpError(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	}
	resp, err := h.issueSession(c, u.UserID, u.Email, u.FirstName, u.LastName)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(resp)
}

func (h *AuthHandler) Register(c fiber.Ctx) error {
	req, err := bindBody[registerRequest](c)
	if err != nil {
		return err
	}
	if req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return httpError(err)
	}
	u, err := h.repos.Users.Create(c.Context(), req.Email, string(hash), req.FirstName, req.LastName)
	if err != nil {
		return httpError(err)
	}
	resp, err := h.issueSession(c, u.UserID, u.Email, u.FirstName, u.LastName)
	if err != nil {
		return httpError(err)
	}
	// Freshly registered users have no tenants yet — callers must POST
	// `/api/organisations` to create one. We still clear the slice to keep the
	// JSON shape consistent with login.
	resp.Organisations = []tenantSummary{}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// Refresh exchanges an opaque refresh token for a new access + refresh pair.
// Implements rotation + reuse-detection: presenting a revoked token wipes every
// refresh token for that user (assumed attacker).
func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	req, err := bindBody[refreshRequest](c)
	if err != nil {
		return err
	}
	if req.RefreshToken == "" {
		return fiber.NewError(fiber.StatusBadRequest, "refreshToken is required")
	}

	tok, err := h.repos.RefreshTokens.GetByHash(c.Context(), middleware.HashRefreshToken(req.RefreshToken))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid refresh token")
		}
		return httpError(err)
	}
	if tok.RevokedAt != nil {
		// Reuse of a revoked token — almost certainly theft. Revoke the
		// whole user's refresh family and reject the request.
		if err := h.repos.RefreshTokens.RevokeAllForUser(c.Context(), tok.UserID); err != nil {
			slog.Warn("failed to revoke refresh family after reuse", "userID", tok.UserID, "err", err)
		}
		return fiber.NewError(fiber.StatusUnauthorized, "refresh token revoked")
	}
	if time.Now().After(tok.ExpiresAt) {
		return fiber.NewError(fiber.StatusUnauthorized, "refresh token expired")
	}

	user, err := h.repos.Users.GetByID(c.Context(), tok.UserID)
	if err != nil {
		return httpError(err)
	}

	// Rotate the refresh token.
	rawRefresh, hashRefresh, err := middleware.NewRefreshToken()
	if err != nil {
		return httpError(err)
	}
	newExpires := time.Now().Add(h.cfg.RefreshTokenTTL)
	if _, err := h.repos.RefreshTokens.Rotate(c.Context(), tok.TokenID, tok.UserID, hashRefresh, newExpires, clientUserAgent(c), clientIP(c)); err != nil {
		if errors.Is(err, repository.ErrForbidden) {
			// Lost a rotation race: the token was revoked between our read
			// and our write. Treat as reuse to be safe.
			_ = h.repos.RefreshTokens.RevokeAllForUser(c.Context(), tok.UserID)
			return fiber.NewError(fiber.StatusUnauthorized, "refresh token revoked")
		}
		return httpError(err)
	}

	accessToken, err := middleware.IssueToken(h.cfg, user.UserID, user.Email)
	if err != nil {
		return httpError(err)
	}
	tenants, err := h.tenantSummaries(c.Context(), user.UserID)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(authResponse{
		Token:                 accessToken,
		RefreshToken:          rawRefresh,
		ExpiresAt:             time.Now().Add(h.cfg.AccessTokenTTL),
		RefreshTokenExpiresAt: newExpires,
		Email:                 user.Email,
		Organisations:         tenants,
		User:                  userPayload(user.UserID, user.FirstName, user.LastName),
	})
}

// Logout revokes the presented refresh token (single device). If the caller
// supplied `?everywhere=true` and is authenticated, every refresh token for
// that user is revoked — useful for "sign out on all devices" UX.
func (h *AuthHandler) Logout(c fiber.Ctx) error {
	req, _ := bindBody[refreshRequest](c)

	everywhere := c.Query("everywhere") == "true"
	userID := middleware.UserIDFrom(c)

	if everywhere && userID != uuid.Nil {
		if err := h.repos.RefreshTokens.RevokeAllForUser(c.Context(), userID); err != nil {
			return httpError(err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}

	if req != nil && req.RefreshToken != "" {
		tok, err := h.repos.RefreshTokens.GetByHash(c.Context(), middleware.HashRefreshToken(req.RefreshToken))
		if err == nil {
			// Only revoke if the token still belongs to the caller (when
			// logged in) or unconditionally on anonymous logout.
			if userID == uuid.Nil || tok.UserID == userID {
				_ = h.repos.RefreshTokens.RevokeByID(c.Context(), tok.TokenID, tok.UserID)
			}
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AuthHandler) Me(c fiber.Ctx) error {
	u, err := h.repos.Users.GetByID(c.Context(), middleware.UserIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	tenants, err := h.tenantSummaries(c.Context(), u.UserID)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{
		"user": fiber.Map{
			"userId":    u.UserID,
			"email":     u.Email,
			"firstName": u.FirstName,
			"lastName":  u.LastName,
		},
		"organisations": tenants,
	})
}

// tenantSummaries loads all organisations the user belongs to in a single query.
func (h *AuthHandler) tenantSummaries(ctx context.Context, userID uuid.UUID) ([]tenantSummary, error) {
	orgs, err := h.repos.Organisations.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]tenantSummary, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, tenantSummary{
			OrganisationID: o.OrganisationID.String(),
			Name:           o.Name,
			ShortCode:      o.ShortCode,
			BaseCurrency:   o.BaseCurrency,
		})
	}
	return out, nil
}

// issueSession builds an access+refresh pair for a successful authentication.
// It persists the refresh token's hash so future rotations can validate + revoke.
func (h *AuthHandler) issueSession(c fiber.Ctx, userID uuid.UUID, email, firstName, lastName string) (authResponse, error) {
	accessToken, err := middleware.IssueToken(h.cfg, userID, email)
	if err != nil {
		return authResponse{}, err
	}
	rawRefresh, hashRefresh, err := middleware.NewRefreshToken()
	if err != nil {
		return authResponse{}, err
	}
	refreshExpires := time.Now().Add(h.cfg.RefreshTokenTTL)
	if _, err := h.repos.RefreshTokens.Create(c.Context(), userID, hashRefresh, refreshExpires, clientUserAgent(c), clientIP(c)); err != nil {
		return authResponse{}, err
	}
	tenants, err := h.tenantSummaries(c.Context(), userID)
	if err != nil {
		return authResponse{}, err
	}
	return authResponse{
		Token:                 accessToken,
		RefreshToken:          rawRefresh,
		ExpiresAt:             time.Now().Add(h.cfg.AccessTokenTTL),
		RefreshTokenExpiresAt: refreshExpires,
		Email:                 email,
		Organisations:         tenants,
		User:                  userPayload(userID, firstName, lastName),
	}, nil
}

func userPayload(id uuid.UUID, firstName, lastName string) map[string]string {
	return map[string]string{
		"userId":    id.String(),
		"firstName": firstName,
		"lastName":  lastName,
	}
}

func clientUserAgent(c fiber.Ctx) string {
	ua := c.Get("User-Agent")
	if len(ua) > 500 {
		ua = ua[:500]
	}
	return ua
}

func clientIP(c fiber.Ctx) string {
	return c.IP()
}
