package handlers

import (
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// providerName is stamped into every Xero-compatible envelope we emit.
const providerName = "goxero"

// errInvalidPayload is the canonical 400 we return when JSON decoding fails.
// Callers shouldn't build their own message — keep the wording consistent so
// frontend tests can pin the response.
var errInvalidPayload = fiber.NewError(fiber.StatusBadRequest, "invalid payload")

// jsonEnvelope wraps a payload in the canonical Xero-like `APIResponse` envelope
// used by every GET-by-id / list endpoint that cares about the `Id`, `Status`,
// `ProviderName`, `DateTimeUTC` metadata Xero SDKs expect.
func jsonEnvelope(c fiber.Ctx, payload any) error {
	return c.JSON(models.APIResponse{
		ID:           uuid.NewString(),
		Status:       "OK",
		ProviderName: providerName,
		DateTimeUTC:  time.Now().UTC(),
		Payload:      payload,
	})
}

// envelopeList is shorthand for `jsonEnvelope(c, fiber.Map{key: items})` —
// the shape used by every "GET /<resource>" endpoint.
func envelopeList[T any](c fiber.Ctx, key string, items []T) error {
	return jsonEnvelope(c, fiber.Map{key: items})
}

// envelopeOne wraps a single resource into the canonical `{key: [item]}` list
// shape — Xero's convention even for GET-by-id.
func envelopeOne[T any](c fiber.Ctx, key string, item T) error {
	return jsonEnvelope(c, fiber.Map{key: []T{item}})
}

// rawList returns `{key: items}` without the outer APIResponse envelope,
// matching Xero's shape for POST/PUT responses and paginated lists.
func rawList[T any](c fiber.Ctx, status int, key string, items []T) error {
	return c.Status(status).JSON(fiber.Map{key: items})
}

// rawOne is rawList's single-item companion.
func rawOne[T any](c fiber.Ctx, status int, key string, item T) error {
	return rawList(c, status, key, []T{item})
}

// noContent writes an empty 204 — every `Delete` handler used to repeat this.
func noContent(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}

// httpError converts a repository/domain error into a Fiber error. Internal
// errors are logged and masked so no implementation detail leaks to the client.
func httpError(err error) *fiber.Error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "not found")
	case errors.Is(err, repository.ErrAlreadyExists):
		return fiber.NewError(fiber.StatusConflict, "already exists")
	case errors.Is(err, repository.ErrForbidden):
		return fiber.NewError(fiber.StatusForbidden, "forbidden")
	}
	slog.Error("internal error", "err", err)
	return fiber.NewError(fiber.StatusInternalServerError, "internal server error")
}

// bindBody parses the JSON request body into T. Returns a 400 Fiber error when
// the payload can't be decoded so handlers don't repeat the same 3 lines.
func bindBody[T any](c fiber.Ctx) (*T, error) {
	v := new(T)
	if err := c.Bind().Body(v); err != nil {
		return nil, errInvalidPayload
	}
	return v, nil
}

// parseYMD parses an ISO "YYYY-MM-DD" date in UTC. Returns the raw parse error
// so callers can decide how to surface it.
func parseYMD(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// parseID parses a UUID from the named route parameter, returning a 400 Fiber
// error when it is missing or malformed.
func parseID(c fiber.Ctx, key string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(key))
	if err != nil {
		return uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "invalid "+key)
	}
	return id, nil
}

// tenantAndID fetches the current organisation id + the ":id" route parameter
// in one call — every CRUD handler used to repeat these three lines.
func tenantAndID(c fiber.Ctx) (uuid.UUID, uuid.UUID, error) {
	orgID := middleware.OrganisationIDFrom(c)
	id, err := parseID(c, "id")
	return orgID, id, err
}

// parseOptionalUUID parses a non-empty UUID string, returning a 400 Fiber error
// for malformed input. Empty input yields (nil, nil).
func parseOptionalUUID(raw, label string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid "+label)
	}
	return &id, nil
}

// paginationFromQuery extracts ?page and ?pageSize with the project's defaults
// and normalises them via models.Pagination.Normalize(). Invalid/negative input
// silently falls back to sane defaults — matches Xero's tolerant behaviour.
func paginationFromQuery(c fiber.Ctx) models.Pagination {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	size, _ := strconv.Atoi(c.Query("pageSize", "50"))
	p := models.Pagination{Page: page, PageSize: size}
	p.Normalize()
	return p
}
