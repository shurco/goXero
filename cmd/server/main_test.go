package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
)

func newAppWithHandler() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: errorHandler})
}

func do(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

func TestErrorHandler_MapsFiberError(t *testing.T) {
	app := newAppWithHandler()
	app.Get("/teapot", func(c fiber.Ctx) error {
		return fiber.NewError(fiber.StatusTeapot, "coffee only")
	})

	code, body := do(t, app, "/teapot")
	assert.Equal(t, fiber.StatusTeapot, code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, fiber.StatusTeapot, resp.ErrorNumber)
	assert.Equal(t, "coffee only", resp.Message)
	assert.Equal(t, "RequestError", resp.Type)
}

func TestErrorHandler_MasksInternalError(t *testing.T) {
	app := newAppWithHandler()
	app.Get("/boom", func(c fiber.Ctx) error {
		return errors.New("internal DB connection dropped with secret detail")
	})

	code, body := do(t, app, "/boom")
	assert.Equal(t, fiber.StatusInternalServerError, code)
	assert.NotContains(t, string(body), "secret")

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, "internal server error", resp.Message)
	assert.Equal(t, fiber.StatusInternalServerError, resp.ErrorNumber)
}
