package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type BankRuleHandler struct {
	repos *repository.Repositories
}

func NewBankRuleHandler(r *repository.Repositories) *BankRuleHandler {
	return &BankRuleHandler{repos: r}
}

func validRuleType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "SPEND", "RECEIVE", "TRANSFER":
		return true
	default:
		return false
	}
}

// normaliseBankRule validates and fills defaults shared by Create/Update. It
// leaves IsActive untouched so callers may create inactive rules.
func normaliseBankRule(br *models.BankRule) error {
	if strings.TrimSpace(br.Name) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	if !validRuleType(br.RuleType) {
		return fiber.NewError(fiber.StatusBadRequest, "RuleType must be SPEND, RECEIVE, or TRANSFER")
	}
	br.RuleType = strings.ToUpper(strings.TrimSpace(br.RuleType))
	if br.Definition.MatchMode == "" {
		br.Definition.MatchMode = "ALL"
	}
	if br.Definition.RunOn == "" {
		br.Definition.RunOn = "ALL_BANK_ACCOUNTS"
	}
	return nil
}

func (h *BankRuleHandler) List(c fiber.Ctx) error {
	list, err := h.repos.BankRules.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	if list == nil {
		list = []models.BankRule{}
	}
	return envelopeList(c, "BankRules", list)
}

func (h *BankRuleHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	br, err := h.repos.BankRules.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "BankRules", *br)
}

func (h *BankRuleHandler) Create(c fiber.Ctx) error {
	br, err := bindBody[models.BankRule](c)
	if err != nil {
		return err
	}
	if err := normaliseBankRule(br); err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)
	if err := h.repos.BankRules.Create(c.Context(), orgID, br); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.BankRules.GetByID(c.Context(), orgID, br.BankRuleID)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankRules", *fresh)
}

func (h *BankRuleHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	br, err := bindBody[models.BankRule](c)
	if err != nil {
		return err
	}
	br.BankRuleID = id
	if err := normaliseBankRule(br); err != nil {
		return err
	}
	if err := h.repos.BankRules.Update(c.Context(), orgID, br); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.BankRules.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "BankRules", *fresh)
}

func (h *BankRuleHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.BankRules.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
