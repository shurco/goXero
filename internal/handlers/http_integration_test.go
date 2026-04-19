package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/repository"
	"github.com/shurco/goxero/internal/router"
	"github.com/shurco/goxero/internal/testutil"
)

var (
	seedDemoOrgID  = uuid.MustParse("6823b27b-c48f-4099-bb27-4202a4f496a2")
	seedDemoUserID = uuid.MustParse("e906a37e-41c0-4b9d-b374-a34052b3b7d1")
)

func testCfg() *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:        "test-secret-for-http-integration-tests-ok",
			AccessTokenTTL:   time.Hour,
			RefreshTokenTTL:  24 * time.Hour,
			TenantHeaderName: "Xero-Tenant-Id",
		},
	}
}

type appHarness struct {
	app   *fiber.App
	cfg   *config.Config
	repos *repository.Repositories
	token string
}

func newHarness(t *testing.T) *appHarness {
	t.Helper()
	pool := testutil.NewPool(t)
	repos := repository.New(pool)
	cfg := testCfg()

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if fe, ok := err.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		},
	})
	router.Register(app, cfg, repos)

	tok, err := middleware.IssueToken(cfg.Auth, seedDemoUserID, "demo@example.com")
	require.NoError(t, err)

	return &appHarness{app: app, cfg: cfg, repos: repos, token: tok}
}

func (h *appHarness) do(t *testing.T, method, path string, body any, tenant bool) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	if tenant {
		req.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	}
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, data
}

// --- Health & auth (no Postgres required) ---

func TestHTTP_Health(t *testing.T) {
	pool := testutil.NewPool(t)
	repos := repository.New(pool)
	cfg := testCfg()
	app := fiber.New()
	router.Register(app, cfg, repos)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHTTP_AuthRegisterLoginMe(t *testing.T) {
	h := newHarness(t)

	email := "harness-" + uuid.NewString()[:6] + "@example.com"
	status, body := h.do(t, http.MethodPost, "/api/auth/register",
		map[string]string{"email": email, "password": "Pa$$w0rd!"},
		false)
	require.Equal(t, http.StatusCreated, status, string(body))

	var reg struct {
		Token string `json:"token"`
		Email string `json:"email"`
	}
	require.NoError(t, json.Unmarshal(body, &reg))
	assert.Equal(t, email, reg.Email)
	require.NotEmpty(t, reg.Token)

	status, body = h.do(t, http.MethodPost, "/api/auth/login",
		map[string]string{"email": email, "password": "Pa$$w0rd!"},
		false)
	require.Equal(t, http.StatusOK, status, string(body))

	var login struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &login))
	require.NotEmpty(t, login.Token)

	// /api/auth/me must respect the registered user's token
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHTTP_AuthRegisterRequiresCredentials(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do(t, http.MethodPost, "/api/auth/register", map[string]string{}, false)
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestHTTP_AuthLoginUnknownUser(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do(t, http.MethodPost, "/api/auth/login",
		map[string]string{"email": "nobody-" + uuid.NewString() + "@example.com", "password": "x"}, false)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestHTTP_AuthLoginWrongPassword(t *testing.T) {
	h := newHarness(t)
	email := "wrongpass-" + uuid.NewString()[:6] + "@example.com"
	status, _ := h.do(t, http.MethodPost, "/api/auth/register",
		map[string]string{"email": email, "password": "good-password"}, false)
	require.Equal(t, http.StatusCreated, status)

	status, _ = h.do(t, http.MethodPost, "/api/auth/login",
		map[string]string{"email": email, "password": "bad-password"}, false)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestHTTP_AuthOrganisationsEndpoint(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(t, http.MethodGet, "/api/organisations", nil, false)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), "organisations")
}

// --- Xero endpoints (tenant required) ---

func TestHTTP_OrganisationGet(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(t, http.MethodGet, "/api/v1/organisation", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), "Demo Company")
}

func TestHTTP_OrganisationPut(t *testing.T) {
	h := newHarness(t)
	payload := map[string]any{
		"Name":               "Demo Company",
		"LegalName":          "Demo Company (Global)",
		"OrganisationType":   "COMPANY",
		"CountryCode":        "US",
		"LineOfBusiness":     "Software",
		"RegistrationNumber": "REG-001",
		"Description":        "Integration test update",
		"Timezone":           "UTC",
		"TaxNumber":          "101-2-303",
		"Profile": map[string]any{
			"ShowExtraOnInvoices": false,
			"SameAsPostal":        true,
			"Email":               "org@example.com",
			"Postal": map[string]string{
				"AddressLine1": "23 Main Street",
				"City":         "Central City",
				"PostalCode":   "90210",
				"Country":      "US",
			},
		},
	}
	status, body := h.do(t, http.MethodPut, "/api/v1/organisation", payload, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), "Integration test update")
	assert.Contains(t, string(body), "org@example.com")
}

func TestHTTP_Accounts_CRUD(t *testing.T) {
	h := newHarness(t)

	// List
	status, body := h.do(t, http.MethodGet, "/api/v1/accounts", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// Create (invalid)
	status, _ = h.do(t, http.MethodPost, "/api/v1/accounts",
		map[string]string{"Code": ""}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// Create (valid)
	code := "HT-" + uuid.NewString()[:6]
	status, body = h.do(t, http.MethodPost, "/api/v1/accounts",
		map[string]any{"Code": code, "Name": "HT Sales", "Type": "REVENUE"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	// Extract the created AccountID
	var env struct {
		Accounts []struct {
			AccountID string `json:"AccountID"`
			Code      string `json:"Code"`
		} `json:"Accounts"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.Len(t, env.Accounts, 1)
	id := env.Accounts[0].AccountID

	// Get by id
	status, _ = h.do(t, http.MethodGet, "/api/v1/accounts/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	// Update
	status, _ = h.do(t, http.MethodPut, "/api/v1/accounts/"+id,
		map[string]any{"Name": "HT Sales Updated"}, true)
	assert.Equal(t, http.StatusOK, status)

	// Archive
	status, _ = h.do(t, http.MethodDelete, "/api/v1/accounts/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)

	// Invalid id → 400
	status, _ = h.do(t, http.MethodGet, "/api/v1/accounts/not-a-uuid", nil, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// Missing id → 404
	status, _ = h.do(t, http.MethodGet, "/api/v1/accounts/"+uuid.NewString(), nil, true)
	assert.Equal(t, http.StatusNotFound, status)
}

func TestHTTP_TaxRates(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do(t, http.MethodPost, "/api/v1/tax-rates", map[string]string{}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	status, body := h.do(t, http.MethodPost, "/api/v1/tax-rates",
		map[string]any{"Name": "T-" + uuid.NewString()[:4], "TaxType": "OUTPUT",
			"DisplayTaxRate": "10", "EffectiveRate": "10"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, _ = h.do(t, http.MethodGet, "/api/v1/tax-rates", nil, true)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTP_Contacts(t *testing.T) {
	h := newHarness(t)

	status, _ := h.do(t, http.MethodPost, "/api/v1/contacts", map[string]string{}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	status, body := h.do(t, http.MethodPost, "/api/v1/contacts",
		map[string]any{"Name": "HT Contact " + uuid.NewString()[:6], "IsCustomer": true}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.Len(t, env.Contacts, 1)
	id := env.Contacts[0].ContactID

	status, _ = h.do(t, http.MethodGet, "/api/v1/contacts", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/contacts/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/contacts/"+id,
		map[string]any{"Name": "HT Contact (renamed)", "ContactStatus": "ACTIVE"}, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/contacts/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)

	// List with filters
	status, _ = h.do(t, http.MethodGet,
		"/api/v1/contacts?status=ACTIVE&isCustomer=true&isSupplier=false&search=HT&page=1&pageSize=10",
		nil, true)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTP_Items(t *testing.T) {
	h := newHarness(t)

	status, _ := h.do(t, http.MethodPost, "/api/v1/items", map[string]string{}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	status, body := h.do(t, http.MethodPost, "/api/v1/items",
		map[string]any{
			"Code":         "IT-" + uuid.NewString()[:4],
			"Name":         "HT item",
			"IsSold":       true,
			"SalesDetails": map[string]any{"UnitPrice": "10", "AccountCode": "200", "TaxType": "NONE"},
		}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Items []struct {
			ItemID string `json:"ItemID"`
		} `json:"Items"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.Len(t, env.Items, 1)
	id := env.Items[0].ItemID

	status, _ = h.do(t, http.MethodGet, "/api/v1/items", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/items/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/items/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_InvoicesAndPayments(t *testing.T) {
	h := newHarness(t)

	inv := map[string]any{
		"Type":            "ACCREC",
		"Status":          "DRAFT",
		"LineAmountTypes": "Exclusive",
		"Date":            "2026-01-01T00:00:00Z",
		"DueDate":         "2026-01-15T00:00:00Z",
		"InvoiceNumber":   "HT-" + uuid.NewString()[:4],
		"LineItems": []map[string]any{
			{"Description": "Unit", "Quantity": "1", "UnitAmount": "100", "AccountCode": "200"},
		},
	}
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", inv, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.Len(t, env.Invoices, 1)
	id := env.Invoices[0].InvoiceID

	status, _ = h.do(t, http.MethodGet, "/api/v1/invoices?type=ACCREC&status=DRAFT&search=HT&page=1&pageSize=5", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/invoices/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	// Bad status
	status, _ = h.do(t, http.MethodPut, "/api/v1/invoices/"+id,
		map[string]string{"status": "nonsense"}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// Good transition
	status, _ = h.do(t, http.MethodPut, "/api/v1/invoices/"+id,
		map[string]string{"status": "AUTHORISED"}, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/reports/invoice-summary", nil, true)
	assert.Equal(t, http.StatusOK, status)

	// Payment requires amount
	status, _ = h.do(t, http.MethodPost, "/api/v1/payments", map[string]string{}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// Invalid invoiceId
	status, _ = h.do(t, http.MethodPost, "/api/v1/payments",
		map[string]any{"invoiceId": "zzz", "amount": "10"}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// Valid payment
	status, _ = h.do(t, http.MethodPost, "/api/v1/payments",
		map[string]any{
			"invoiceId": id,
			"amount":    "10",
			"date":      "2026-01-20",
		}, true)
	require.Equal(t, http.StatusCreated, status)

	// Bad date format
	status, _ = h.do(t, http.MethodPost, "/api/v1/payments",
		map[string]any{"invoiceId": id, "amount": "1", "date": "20/01/2026"}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/payments?page=1&pageSize=20", nil, true)
	assert.Equal(t, http.StatusOK, status)
}

// --- Tenant-guard errors ---

func TestHTTP_TenantMissing(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do(t, http.MethodGet, "/api/v1/accounts", nil, false)
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestHTTP_TenantForbiddenForOutsider(t *testing.T) {
	h := newHarness(t)
	outsiderTok, err := middleware.IssueToken(h.cfg.Auth, uuid.New(), "outsider@example.com")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+outsiderTok)
	req.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHTTP_UnauthenticatedXero(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organisation", nil)
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHTTP_InvalidJSONBody(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
