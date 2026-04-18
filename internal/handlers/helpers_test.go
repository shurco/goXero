package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

func TestParseOptionalUUIDEmpty(t *testing.T) {
	id, err := parseOptionalUUID("", "x")
	require.NoError(t, err)
	assert.Nil(t, id)
}

func TestParseOptionalUUIDValid(t *testing.T) {
	want := uuid.New()
	got, err := parseOptionalUUID(want.String(), "x")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
}

func TestParseOptionalUUIDInvalid(t *testing.T) {
	_, err := parseOptionalUUID("not-a-uuid", "contactId")
	var fe *fiber.Error
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, fiber.StatusBadRequest, fe.Code)
}

func TestHTTPErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{repository.ErrNotFound, fiber.StatusNotFound},
		{repository.ErrAlreadyExists, fiber.StatusConflict},
		{repository.ErrForbidden, fiber.StatusForbidden},
		{errors.New("boom"), fiber.StatusInternalServerError},
	}
	for _, tc := range cases {
		fe := httpError(tc.err)
		assert.Equal(t, tc.code, fe.Code, "err=%v", tc.err)
		if tc.code == fiber.StatusInternalServerError {
			assert.Equal(t, "internal server error", fe.Message)
		}
	}
}

func TestParseYMDRoundTrip(t *testing.T) {
	d, err := parseYMD("2024-05-01")
	require.NoError(t, err)
	assert.Equal(t, 2024, d.Year())
	assert.EqualValues(t, 5, d.Month())
	assert.Equal(t, 1, d.Day())

	_, err = parseYMD("not-a-date")
	assert.Error(t, err)
}

func TestPaginationFromQueryDefaultsAndOverflow(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		p := paginationFromQuery(c)
		return c.JSON(p)
	})
	cases := []struct {
		query    string
		wantPage int
		wantSize int
	}{
		{"", 1, 50},
		{"?page=3&pageSize=100", 3, 100},
		{"?page=-5&pageSize=0", 1, 50},
		{"?page=abc&pageSize=xyz", 1, 50},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/"+tc.query, nil)
		resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
		require.NoError(t, err)
		var p models.Pagination
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
		assert.Equal(t, tc.wantPage, p.Page, "query=%q", tc.query)
		assert.Equal(t, tc.wantSize, p.PageSize, "query=%q", tc.query)
	}
}

type envelopePayload struct {
	Status       string `json:"Status"`
	ProviderName string `json:"ProviderName"`
	Payload      struct {
		Invoices []struct {
			ID string `json:"Id"`
		} `json:"Invoices"`
	} `json:"Payload"`
}

func TestEnvelopeOneWrapsPayload(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return envelopeOne(c, "Invoices", struct {
			ID string `json:"Id"`
		}{ID: "abc"})
	})
	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(t, err)

	var e envelopePayload
	require.NoError(t, json.Unmarshal(buf.Bytes(), &e))
	assert.Equal(t, "OK", e.Status)
	assert.Equal(t, providerName, e.ProviderName)
	require.Len(t, e.Payload.Invoices, 1)
	assert.Equal(t, "abc", e.Payload.Invoices[0].ID)
}

func TestRawOneSkipsOuterEnvelope(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		return rawOne(c, fiber.StatusCreated, "Invoices", map[string]string{"Id": "abc"})
	})
	req := httptest.NewRequest("POST", "/", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var out struct {
		Invoices []map[string]string `json:"Invoices"`
		Status   string              `json:"Status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Invoices, 1)
	assert.Equal(t, "abc", out.Invoices[0]["Id"])
	// The raw envelope does not set "Status": "OK".
	assert.Empty(t, out.Status)
}

func TestBindBodyDecodesJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		p, err := bindBody[payload](c)
		if err != nil {
			return err
		}
		return c.JSON(p)
	})
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	req = httptest.NewRequest("POST", "/", bytes.NewBufferString(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestTenantAndIDHappyPath(t *testing.T) {
	app := fiber.New()
	orgID := uuid.New()
	pid := uuid.New()
	app.Get("/:id", func(c fiber.Ctx) error {
		c.Locals("organisation", &models.Organisation{OrganisationID: orgID})
		org, id, err := tenantAndID(c)
		require.NoError(t, err)
		assert.Equal(t, orgID, org)
		assert.Equal(t, pid, id)
		return c.SendStatus(fiber.StatusOK)
	})
	req := httptest.NewRequest("GET", "/"+pid.String(), nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestTenantAndIDInvalidID(t *testing.T) {
	app := fiber.New()
	app.Get("/:id", func(c fiber.Ctx) error {
		_, _, err := tenantAndID(c)
		return err
	})
	req := httptest.NewRequest("GET", "/not-a-uuid", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestNoContentReturns204(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return noContent(c) })
	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: -1})
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
}

func TestAttachmentSubjectMapCompleteness(t *testing.T) {
	// The frontend `AttachmentSubject` union lists these slugs — each must
	// resolve to a backend subject_type so polymorphic attachments keep
	// working when the SPA uploads/lists them.
	wanted := []string{
		"invoices", "credit-notes", "bank-transactions", "contacts",
		"accounts", "manual-journals", "quotes", "purchase-orders",
		"receipts", "expense-claims",
	}
	for _, slug := range wanted {
		_, ok := attachmentSubjectMap[slug]
		assert.True(t, ok, "missing subject slug %q", slug)
	}
}
