package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// ManualJournalHandler: https://developer.xero.com/documentation/api/accounting/manualjournals
type ManualJournalHandler struct {
	repos *repository.Repositories
}

func NewManualJournalHandler(r *repository.Repositories) *ManualJournalHandler {
	return &ManualJournalHandler{repos: r}
}

func (h *ManualJournalHandler) List(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	p := paginationFromQuery(c)
	list, total, err := h.repos.ManualJournals.List(c.Context(), orgID, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"ManualJournals": list, "Pagination": p})
}

func (h *ManualJournalHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	mj, err := h.repos.ManualJournals.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "ManualJournals", *mj)
}

func (h *ManualJournalHandler) Create(c fiber.Ctx) error {
	mj, err := bindBody[models.ManualJournal](c)
	if err != nil {
		return err
	}
	if mj.Narration == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Narration is required")
	}
	if len(mj.JournalLines) < 2 {
		return fiber.NewError(fiber.StatusBadRequest, "At least two journal lines are required")
	}
	if mj.Status == "" {
		mj.Status = "DRAFT"
	}
	if mj.LineAmountTypes == "" {
		mj.LineAmountTypes = models.LineAmountTypesExclusive
	}
	if err := h.repos.ManualJournals.Create(c.Context(), middleware.OrganisationIDFrom(c), mj); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "ManualJournals", *mj)
}

func (h *ManualJournalHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.ManualJournals.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// JournalHandler serves the read-only GL journal feed
// (https://developer.xero.com/documentation/api/accounting/journals).
type JournalHandler struct {
	repos *repository.Repositories
}

func NewJournalHandler(r *repository.Repositories) *JournalHandler {
	return &JournalHandler{repos: r}
}

func (h *JournalHandler) List(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	p := paginationFromQuery(c)
	filter := repository.JournalFilter{}
	if v := c.Query("from"); v != "" {
		d, err := parseYMD(v)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid from (expected YYYY-MM-DD)")
		}
		filter.From = &d
	}
	if v := c.Query("to"); v != "" {
		d, err := parseYMD(v)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid to (expected YYYY-MM-DD)")
		}
		filter.To = &d
	}
	list, total, err := h.repos.Journals.List(c.Context(), orgID, filter, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"Journals": list, "Pagination": p})
}
