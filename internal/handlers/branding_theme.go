package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// BrandingThemeHandler: https://developer.xero.com/documentation/api/accounting/brandingthemes
type BrandingThemeHandler struct {
	repos *repository.Repositories
}

func NewBrandingThemeHandler(r *repository.Repositories) *BrandingThemeHandler {
	return &BrandingThemeHandler{repos: r}
}

func (h *BrandingThemeHandler) List(c fiber.Ctx) error {
	list, err := h.repos.BrandingThemes.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "BrandingThemes", list)
}

func (h *BrandingThemeHandler) Create(c fiber.Ctx) error {
	b, err := bindBody[models.BrandingTheme](c)
	if err != nil {
		return err
	}
	if b.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	if err := h.repos.BrandingThemes.Create(c.Context(), middleware.OrganisationIDFrom(c), b); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BrandingThemes", *b)
}
