package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/repository"
	"github.com/shurco/goxero/internal/testutil"
)

// seed UUIDs from migrations/00009_seed_demo.sql.
var (
	seedOrgID  = uuid.MustParse("6823b27b-c48f-4099-bb27-4202a4f496a2")
	seedUserID = uuid.MustParse("e906a37e-41c0-4b9d-b374-a34052b3b7d1")
)

// doRequest builds a request and returns status + body string.
func doRequest(t *testing.T, app *fiber.App, method, target string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func newJWTApp(t *testing.T) (*fiber.App, string) {
	cfg := testAuthCfg(t)
	app := fiber.New()
	app.Get("/secret", JWTAuth(cfg), func(c fiber.Ctx) error {
		uid := UserIDFrom(c)
		return c.JSON(fiber.Map{"user": uid.String()})
	})
	tok, err := IssueToken(cfg, uuid.New(), "u@example.com")
	require.NoError(t, err)
	return app, tok
}

func TestJWTAuth_MissingHeader(t *testing.T) {
	app, _ := newJWTApp(t)
	code, _ := doRequest(t, app, http.MethodGet, "/secret", nil)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestJWTAuth_BadScheme(t *testing.T) {
	app, tok := newJWTApp(t)
	code, _ := doRequest(t, app, http.MethodGet, "/secret",
		map[string]string{"Authorization": "Token " + tok})
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	app, _ := newJWTApp(t)
	code, body := doRequest(t, app, http.MethodGet, "/secret",
		map[string]string{"Authorization": "Bearer junk"})
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.NotContains(t, body, "signature", "JWT internals must not leak")
}

func TestJWTAuth_ValidToken(t *testing.T) {
	app, tok := newJWTApp(t)
	code, body := doRequest(t, app, http.MethodGet, "/secret",
		map[string]string{"Authorization": "Bearer " + tok})
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `"user"`)
}

// --- Tenant middleware (requires Postgres via pgtestdb) ---

func newTenantApp(t *testing.T) (*fiber.App, *repository.Repositories) {
	cfg := testAuthCfg(t)
	pool := testutil.NewPool(t)
	repos := repository.New(pool)

	app := fiber.New()
	// Simulate the JWTAuth result by pre-seeding the userID local.
	app.Use(func(c fiber.Ctx) error {
		if raw := c.Get("X-Test-User"); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				c.Locals(string(CtxUserID), id)
			}
		}
		return c.Next()
	})
	app.Get("/tenant-protected", Tenant(cfg, repos), func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"org": OrganisationIDFrom(c).String()})
	})
	return app, repos
}

func TestTenant_MissingID(t *testing.T) {
	app, _ := newTenantApp(t)
	code, _ := doRequest(t, app, http.MethodGet, "/tenant-protected",
		map[string]string{"X-Test-User": seedUserID.String()})
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestTenant_InvalidUUID(t *testing.T) {
	app, _ := newTenantApp(t)
	code, _ := doRequest(t, app, http.MethodGet, "/tenant-protected",
		map[string]string{
			"X-Test-User":    seedUserID.String(),
			"Xero-Tenant-Id": "not-a-uuid",
		})
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestTenant_UnauthenticatedUser(t *testing.T) {
	app, _ := newTenantApp(t)
	code, _ := doRequest(t, app, http.MethodGet, "/tenant-protected",
		map[string]string{"Xero-Tenant-Id": seedOrgID.String()})
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestTenant_ForbiddenForNonMember(t *testing.T) {
	app, _ := newTenantApp(t)
	outsider := uuid.New()
	code, body := doRequest(t, app, http.MethodGet, "/tenant-protected",
		map[string]string{
			"X-Test-User":    outsider.String(),
			"Xero-Tenant-Id": seedOrgID.String(),
		})
	assert.Equal(t, http.StatusForbidden, code)
	assert.Contains(t, body, "no access")
}

func TestTenant_OrganisationNotFound(t *testing.T) {
	// Exercises the "org exists in membership but not in organisations" branch
	// of Tenant middleware. We simulate it by directly inserting a mapping
	// inside a transaction that has temporarily disabled FK triggers — the only
	// way to reach this defensive branch short of a race condition.
	app, repos := newTenantApp(t)

	ghost := uuid.New()
	ctx := context.Background()
	_, err := repos.Pool.Exec(ctx,
		`INSERT INTO organisations (organisation_id, name, base_currency)
		 VALUES ($1, 'ghost', 'USD')`, ghost)
	require.NoError(t, err)
	require.NoError(t, repos.Users.LinkOrganisation(ctx, ghost, seedUserID, "admin"))
	// Drop only the org row; CASCADE would wipe the membership mapping too,
	// so we detach the FK in this test database first.
	_, err = repos.Pool.Exec(ctx,
		`ALTER TABLE organisation_users DROP CONSTRAINT organisation_users_organisation_id_fkey`)
	require.NoError(t, err)
	_, err = repos.Pool.Exec(ctx, `DELETE FROM organisations WHERE organisation_id=$1`, ghost)
	require.NoError(t, err)

	code, _ := doRequest(t, app, http.MethodGet, "/tenant-protected",
		map[string]string{
			"X-Test-User":    seedUserID.String(),
			"Xero-Tenant-Id": ghost.String(),
		})
	assert.Equal(t, http.StatusNotFound, code)
}

func TestTenant_ResolvesOrganisationViaQuery(t *testing.T) {
	app, _ := newTenantApp(t)
	code, body := doRequest(t, app, http.MethodGet,
		"/tenant-protected?organisationId="+seedOrgID.String(),
		map[string]string{"X-Test-User": seedUserID.String()})
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, seedOrgID.String())
}

func TestTenant_ResolvesOrganisationViaHeader(t *testing.T) {
	app, _ := newTenantApp(t)
	code, body := doRequest(t, app, http.MethodGet, "/tenant-protected",
		map[string]string{
			"X-Test-User":    seedUserID.String(),
			"Xero-Tenant-Id": seedOrgID.String(),
		})
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, seedOrgID.String())
}

func TestHelpers_DefaultsWhenMissing(t *testing.T) {
	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"userID": UserIDFrom(c).String(),
			"orgID":  OrganisationIDFrom(c).String(),
			"org":    OrganisationFrom(c) == nil,
		})
	})
	code, body := doRequest(t, app, http.MethodGet, "/probe", nil)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, uuid.Nil.String())
	assert.Contains(t, body, `"org":true`)
}

// Sanity to avoid unused-import warnings when the rest of the file is skipped.
var _ = time.Second
