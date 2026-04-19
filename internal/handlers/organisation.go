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

type updateOrganisationRequest struct {
	Name               string                     `json:"Name"`
	LegalName          string                     `json:"LegalName"`
	OrganisationType   string                     `json:"OrganisationType"`
	CountryCode        string                     `json:"CountryCode"`
	LineOfBusiness     string                     `json:"LineOfBusiness"`
	RegistrationNumber string                     `json:"RegistrationNumber"`
	Description        string                     `json:"Description"`
	Timezone           string                     `json:"Timezone"`
	TaxNumber          string                     `json:"TaxNumber"`
	Profile            models.OrganisationProfile `json:"Profile"`
}

// PUT /api/v1/organisation — update tenant details (settings form).
func (h *OrganisationHandler) Update(c fiber.Ctx) error {
	req, err := bindBody[updateOrganisationRequest](c)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	legal := strings.TrimSpace(req.LegalName)
	if legal == "" {
		return fiber.NewError(fiber.StatusBadRequest, "LegalName is required")
	}

	orgID := middleware.OrganisationIDFrom(c)
	existing, err := h.repos.Organisations.GetByID(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}

	existing.Name = name
	existing.LegalName = legal
	existing.OrganisationType = strings.TrimSpace(req.OrganisationType)
	cc := strings.TrimSpace(req.CountryCode)
	if cc != "" {
		cc = strings.ToUpper(cc)
		if len(cc) > 2 {
			cc = cc[:2]
		}
	}
	existing.CountryCode = cc
	existing.LineOfBusiness = strings.TrimSpace(req.LineOfBusiness)
	existing.RegistrationNumber = strings.TrimSpace(req.RegistrationNumber)
	existing.Description = strings.TrimSpace(req.Description)
	if tz := strings.TrimSpace(req.Timezone); tz != "" {
		existing.Timezone = tz
	}
	existing.TaxNumber = strings.TrimSpace(req.TaxNumber)
	existing.Profile = req.Profile

	if err := h.repos.Organisations.Update(c.Context(), existing); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.Organisations.GetByID(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Organisations", *fresh)
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
	Name                  string `json:"name"`
	LegalName             string `json:"legalName,omitempty"`
	ShortCode             string `json:"shortCode,omitempty"`
	OrganisationType      string `json:"organisationType,omitempty"`
	CountryCode           string `json:"countryCode,omitempty"`
	BaseCurrency          string `json:"baseCurrency,omitempty"`
	Timezone              string `json:"timezone,omitempty"`
	TaxNumber             string `json:"taxNumber,omitempty"`
	LineOfBusiness        string `json:"lineOfBusiness,omitempty"`
	RegistrationNumber    string `json:"registrationNumber,omitempty"`
	FinancialYearEndDay   int    `json:"financialYearEndDay,omitempty"`
	FinancialYearEndMonth int    `json:"financialYearEndMonth,omitempty"`
	HasEmployees          *bool  `json:"hasEmployees,omitempty"`
	PriorAccountingTool   string `json:"priorAccountingTool,omitempty"`
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

	fyDay := req.FinancialYearEndDay
	if fyDay < 1 || fyDay > 31 {
		fyDay = 31
	}
	fyMonth := req.FinancialYearEndMonth
	if fyMonth < 1 || fyMonth > 12 {
		fyMonth = 12
	}

	prof := models.OrganisationProfile{}
	if req.HasEmployees != nil {
		v := *req.HasEmployees
		prof.HasEmployees = &v
	}
	if t := strings.TrimSpace(req.PriorAccountingTool); t != "" {
		prof.PriorAccountingTool = t
	}

	org := &models.Organisation{
		Name:                  name,
		LegalName:             req.LegalName,
		ShortCode:             strings.ToUpper(strings.TrimSpace(req.ShortCode)),
		OrganisationType:      req.OrganisationType,
		CountryCode:           strings.ToUpper(strings.TrimSpace(req.CountryCode)),
		BaseCurrency:          currency,
		Timezone:              req.Timezone,
		TaxNumber:             req.TaxNumber,
		LineOfBusiness:        req.LineOfBusiness,
		RegistrationNumber:    req.RegistrationNumber,
		FinancialYearEndDay:   fyDay,
		FinancialYearEndMonth: fyMonth,
		Profile:               prof,
	}
	if err := h.repos.Organisations.Create(c.Context(), org); err != nil {
		return httpError(err)
	}
	if err := h.repos.Users.LinkOrganisation(c.Context(), org.OrganisationID, middleware.UserIDFrom(c), "ADMIN"); err != nil {
		return httpError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"organisation": org})
}
