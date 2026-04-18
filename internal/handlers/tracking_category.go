package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// TrackingCategoryHandler: https://developer.xero.com/documentation/api/accounting/trackingcategories
type TrackingCategoryHandler struct {
	repos *repository.Repositories
}

func NewTrackingCategoryHandler(r *repository.Repositories) *TrackingCategoryHandler {
	return &TrackingCategoryHandler{repos: r}
}

func (h *TrackingCategoryHandler) List(c fiber.Ctx) error {
	list, err := h.repos.TrackingCategories.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "TrackingCategories", list)
}

func (h *TrackingCategoryHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	t, err := h.repos.TrackingCategories.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "TrackingCategories", *t)
}

func (h *TrackingCategoryHandler) Create(c fiber.Ctx) error {
	t, err := bindBody[models.TrackingCategory](c)
	if err != nil {
		return err
	}
	if t.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	if t.Status == "" {
		t.Status = "ACTIVE"
	}
	if err := h.repos.TrackingCategories.Create(c.Context(), middleware.OrganisationIDFrom(c), t); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "TrackingCategories", *t)
}

func (h *TrackingCategoryHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.TrackingCategories.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.TrackingCategoryID = id
	if err := h.repos.TrackingCategories.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "TrackingCategories", *existing)
}

func (h *TrackingCategoryHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.TrackingCategories.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// AddOption implements `PUT /TrackingCategories/{id}/Options`.
func (h *TrackingCategoryHandler) AddOption(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	opt, err := bindBody[models.TrackingOption](c)
	if err != nil {
		return err
	}
	if opt.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	if opt.Status == "" {
		opt.Status = "ACTIVE"
	}
	if err := h.repos.TrackingCategories.AddOption(c.Context(), orgID, id, opt); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Options", *opt)
}
