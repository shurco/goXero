package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type AccountHandler struct {
	repos *repository.Repositories
}

func NewAccountHandler(r *repository.Repositories) *AccountHandler {
	return &AccountHandler{repos: r}
}

func (h *AccountHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Accounts.List(c.Context(), middleware.OrganisationIDFrom(c), repository.AccountFilter{
		Status: c.Query("status"),
		Type:   c.Query("type"),
		Search: c.Query("where"),
	})
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Accounts", list)
}

func (h *AccountHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	a, err := h.repos.Accounts.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Accounts", *a)
}

func (h *AccountHandler) Create(c fiber.Ctx) error {
	payload, err := bindBody[models.Account](c)
	if err != nil {
		return err
	}
	if payload.Code == "" || payload.Name == "" || payload.Type == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Code, Name and Type are required")
	}
	if payload.Status == "" {
		payload.Status = "ACTIVE"
	}
	if err := h.repos.Accounts.Create(c.Context(), middleware.OrganisationIDFrom(c), payload); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Accounts", *payload)
}

func (h *AccountHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.Accounts.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.AccountID = id
	if err := h.repos.Accounts.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Accounts", *existing)
}

func (h *AccountHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Accounts.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

type TaxRateHandler struct {
	repos *repository.Repositories
}

func NewTaxRateHandler(r *repository.Repositories) *TaxRateHandler {
	return &TaxRateHandler{repos: r}
}

func (h *TaxRateHandler) List(c fiber.Ctx) error {
	list, err := h.repos.TaxRates.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "TaxRates", list)
}

func (h *TaxRateHandler) Create(c fiber.Ctx) error {
	t, err := bindBody[models.TaxRate](c)
	if err != nil {
		return err
	}
	if t.Name == "" || t.TaxType == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name and TaxType are required")
	}
	if err := h.repos.TaxRates.Create(c.Context(), middleware.OrganisationIDFrom(c), t); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "TaxRates", *t)
}

func (h *TaxRateHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	t, err := h.repos.TaxRates.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "TaxRates", *t)
}

func (h *TaxRateHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.TaxRates.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.TaxRateID = id
	if err := h.repos.TaxRates.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "TaxRates", *existing)
}

func (h *TaxRateHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.TaxRates.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
