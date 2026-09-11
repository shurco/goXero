package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/bankrules"
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
	// A rule whose conditions or allocations are malformed would silently never
	// match or never code anything, so it is rejected at the door rather than
	// left in the tenant's rules doing nothing.
	if err := bankrules.Validate(br.Definition); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
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

// Reorder rewrites the evaluation order of the tenant's rules. The client sends
// the whole ordering, so two people dragging rules at the same time cannot
// interleave into a third order neither of them asked for.
func (h *BankRuleHandler) Reorder(c fiber.Ctx) error {
	body, err := bindBody[struct {
		BankRuleIDs []uuid.UUID `json:"BankRuleIDs"`
	}](c)
	if err != nil {
		return err
	}
	if len(body.BankRuleIDs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "BankRuleIDs is required")
	}
	if err := h.repos.BankRules.Reorder(c.Context(), middleware.OrganisationIDFrom(c), body.BankRuleIDs); err != nil {
		return httpError(err)
	}
	list, err := h.repos.BankRules.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	if list == nil {
		list = []models.BankRule{}
	}
	return rawList(c, fiber.StatusOK, "BankRules", list)
}
