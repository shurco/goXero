package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type ItemHandler struct {
	repos *repository.Repositories
}

func NewItemHandler(r *repository.Repositories) *ItemHandler {
	return &ItemHandler{repos: r}
}

func (h *ItemHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Items.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Items", list)
}

func (h *ItemHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	item, err := h.repos.Items.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Items", *item)
}

func (h *ItemHandler) Create(c fiber.Ctx) error {
	it, err := bindBody[models.Item](c)
	if err != nil {
		return err
	}
	if it.Code == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Code is required")
	}
	if err := h.repos.Items.Create(c.Context(), middleware.OrganisationIDFrom(c), it); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Items", *it)
}

func (h *ItemHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.Items.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.ItemID = id
	if err := h.repos.Items.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Items", *existing)
}

func (h *ItemHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Items.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
