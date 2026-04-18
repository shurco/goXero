package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// CreditNoteHandler implements the endpoints documented at
// https://developer.xero.com/documentation/api/accounting/creditnotes.
type CreditNoteHandler struct {
	repos *repository.Repositories
}

func NewCreditNoteHandler(r *repository.Repositories) *CreditNoteHandler {
	return &CreditNoteHandler{repos: r}
}

type creditNoteRequest struct {
	models.CreditNote
	ContactID string `json:"ContactID"`
}

func (h *CreditNoteHandler) List(c fiber.Ctx) error {
	p := paginationFromQuery(c)
	list, total, err := h.repos.CreditNotes.List(c.Context(), middleware.OrganisationIDFrom(c), repository.CreditNoteFilter{
		Type:   c.Query("type"),
		Status: c.Query("status"),
	}, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"CreditNotes": list, "Pagination": p})
}

func (h *CreditNoteHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	cn, err := h.repos.CreditNotes.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "CreditNotes", *cn)
}

func (h *CreditNoteHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[creditNoteRequest](c)
	if err != nil {
		return err
	}
	cn := req.CreditNote
	if cid, err := parseOptionalUUID(req.ContactID, "ContactID"); err != nil {
		return err
	} else if cid != nil {
		cn.ContactID = cid
	} else if req.Contact != nil && req.Contact.ContactID != uuid.Nil {
		cn.ContactID = &req.Contact.ContactID
	}
	if cn.Type == "" {
		cn.Type = models.CreditNoteTypeAccRecCredit
	}
	if cn.Status == "" {
		cn.Status = models.CreditNoteStatusDraft
	}
	if cn.LineAmountTypes == "" {
		cn.LineAmountTypes = models.LineAmountTypesExclusive
	}
	if err := h.repos.CreditNotes.Create(c.Context(), middleware.OrganisationIDFrom(c), &cn); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "CreditNotes", cn)
}

func (h *CreditNoteHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.CreditNotes.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.CreditNoteID = id
	if err := h.repos.CreditNotes.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.CreditNotes.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "CreditNotes", *fresh)
}

func (h *CreditNoteHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.CreditNotes.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

type allocationRequest struct {
	InvoiceID string          `json:"invoiceId"`
	Amount    decimal.Decimal `json:"amount"`
	Date      string          `json:"date"`
}

// Allocate implements `POST /CreditNotes/{id}/Allocations`.
func (h *CreditNoteHandler) Allocate(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	req, err := bindBody[allocationRequest](c)
	if err != nil {
		return err
	}
	invID, err := uuid.Parse(req.InvoiceID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid invoiceId")
	}
	if !req.Amount.IsPositive() {
		return fiber.NewError(fiber.StatusBadRequest, "amount must be positive")
	}
	date := time.Now().UTC()
	if req.Date != "" {
		d, err := parseYMD(req.Date)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid date (expected YYYY-MM-DD)")
		}
		date = d
	}
	if err := h.repos.CreditNotes.Allocate(c.Context(), orgID, id, invID, req.Amount, date); err != nil {
		return httpError(err)
	}
	return c.SendStatus(fiber.StatusCreated)
}
