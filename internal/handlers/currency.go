package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// CurrencyHandler: https://developer.xero.com/documentation/api/accounting/currencies
type CurrencyHandler struct {
	repos *repository.Repositories
}

func NewCurrencyHandler(r *repository.Repositories) *CurrencyHandler {
	return &CurrencyHandler{repos: r}
}

func (h *CurrencyHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Currencies.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Currencies", list)
}

func (h *CurrencyHandler) Create(c fiber.Ctx) error {
	cur, err := bindBody[models.Currency](c)
	if err != nil {
		return err
	}
	cur.Code = strings.ToUpper(strings.TrimSpace(cur.Code))
	if len(cur.Code) != 3 {
		return fiber.NewError(fiber.StatusBadRequest, "Code must be a 3-letter ISO-4217 code")
	}
	if cur.Description == "" {
		cur.Description = cur.Code
	}
	if err := h.repos.Currencies.Create(c.Context(), middleware.OrganisationIDFrom(c), cur); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Currencies", *cur)
}
