package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type OrganisationHandler struct {
	repos *repository.Repositories
}

func NewOrganisationHandler(r *repository.Repositories) *OrganisationHandler {
	return &OrganisationHandler{repos: r}
}

// GET /api/v1/organisation
func (h *OrganisationHandler) Get(c fiber.Ctx) error {
	return envelopeOne(c, "Organisations", *middleware.OrganisationFrom(c))
}

// GET /api/organisations — lists all orgs the user can access.
func (h *OrganisationHandler) List(c fiber.Ctx) error {
	orgs, err := h.repos.Organisations.ListForUser(c.Context(), middleware.UserIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	if orgs == nil {
		orgs = []models.Organisation{}
	}
	return c.JSON(fiber.Map{"organisations": orgs})
}

type createOrganisationRequest struct {
	Name               string `json:"name"`
	LegalName          string `json:"legalName,omitempty"`
	ShortCode          string `json:"shortCode,omitempty"`
	OrganisationType   string `json:"organisationType,omitempty"`
	CountryCode        string `json:"countryCode,omitempty"`
	BaseCurrency       string `json:"baseCurrency,omitempty"`
	Timezone           string `json:"timezone,omitempty"`
	TaxNumber          string `json:"taxNumber,omitempty"`
	LineOfBusiness     string `json:"lineOfBusiness,omitempty"`
	RegistrationNumber string `json:"registrationNumber,omitempty"`
}

// POST /api/organisations — create a new organisation owned by the current user.
// The creator is automatically linked with role "ADMIN".
func (h *OrganisationHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[createOrganisationRequest](c)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}

	currency := strings.ToUpper(strings.TrimSpace(req.BaseCurrency))
	if currency == "" {
		currency = "USD"
	}

	org := &models.Organisation{
		Name:               name,
		LegalName:          req.LegalName,
		ShortCode:          strings.ToUpper(strings.TrimSpace(req.ShortCode)),
		OrganisationType:   req.OrganisationType,
		CountryCode:        strings.ToUpper(strings.TrimSpace(req.CountryCode)),
		BaseCurrency:       currency,
		Timezone:           req.Timezone,
		TaxNumber:          req.TaxNumber,
		LineOfBusiness:     req.LineOfBusiness,
		RegistrationNumber: req.RegistrationNumber,
	}
	if err := h.repos.Organisations.Create(c.Context(), org); err != nil {
		return httpError(err)
	}
	if err := h.repos.Users.LinkOrganisation(c.Context(), org.OrganisationID, middleware.UserIDFrom(c), "ADMIN"); err != nil {
		return httpError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"organisation": org})
}
