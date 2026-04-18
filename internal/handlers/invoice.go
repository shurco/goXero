package handlers

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type InvoiceHandler struct {
	repos *repository.Repositories
}

func NewInvoiceHandler(r *repository.Repositories) *InvoiceHandler {
	return &InvoiceHandler{repos: r}
}

// validInvoiceStatuses enumerates transitions that the API accepts.
var validInvoiceStatuses = map[string]bool{
	models.InvoiceStatusDraft:      true,
	models.InvoiceStatusSubmitted:  true,
	models.InvoiceStatusAuthorised: true,
	models.InvoiceStatusPaid:       true,
	models.InvoiceStatusVoided:     true,
	models.InvoiceStatusDeleted:    true,
}

func (h *InvoiceHandler) List(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	p := paginationFromQuery(c)

	filter := repository.InvoiceFilter{
		Type:   c.Query("type"),
		Status: c.Query("status"),
		Search: c.Query("search"),
	}
	list, total, err := h.repos.Invoices.List(c.Context(), orgID, filter, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{
		"Invoices":   list,
		"Pagination": p,
	})
}

func (h *InvoiceHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	inv, err := h.repos.Invoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Invoices", *inv)
}

type createInvoiceRequest struct {
	models.Invoice
	ContactID string `json:"ContactID"`
}

func (h *InvoiceHandler) Create(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	req, err := bindBody[createInvoiceRequest](c)
	if err != nil {
		return err
	}
	inv := req.Invoice
	cid, ferr := parseOptionalUUID(req.ContactID, "ContactID")
	if ferr != nil {
		return ferr
	}
	switch {
	case cid != nil:
		inv.ContactID = cid
	case req.Contact != nil:
		inv.ContactID = &req.Contact.ContactID
	}
	if inv.Type == "" {
		inv.Type = models.InvoiceTypeAccRec
	}
	if inv.Status == "" {
		inv.Status = models.InvoiceStatusDraft
	}
	if inv.LineAmountTypes == "" {
		inv.LineAmountTypes = models.LineAmountTypesExclusive
	}
	if err := h.repos.Invoices.Create(c.Context(), orgID, &inv); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Invoices", inv)
}

// Update is the dual-purpose PUT handler. Two payload shapes are accepted:
//   - Status-only: `{"status":"AUTHORISED"}` — invokes UpdateStatus
//     (matches Xero's "transition invoice" calls).
//   - Full Invoice body with LineItems / header fields — invokes Update
//     which replaces line items and recomputes totals.
func (h *InvoiceHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body := c.Body()
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "empty body")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		return errInvalidPayload
	}
	if isStatusOnlyInvoicePayload(probe) {
		return h.UpdateStatus(c)
	}

	existing, err := h.repos.Invoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	var req createInvoiceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return errInvalidPayload
	}
	inv := req.Invoice
	inv.InvoiceID = id
	if cid, err := parseOptionalUUID(req.ContactID, "ContactID"); err != nil {
		return err
	} else if cid != nil {
		inv.ContactID = cid
	} else if req.Contact != nil && req.Contact.ContactID != uuid.Nil {
		inv.ContactID = &req.Contact.ContactID
	} else {
		inv.ContactID = existing.ContactID
	}
	if inv.Type == "" {
		inv.Type = existing.Type
	}
	if inv.LineAmountTypes == "" {
		inv.LineAmountTypes = existing.LineAmountTypes
	}
	if inv.Status == "" {
		inv.Status = existing.Status
	}
	if inv.Date == nil {
		inv.Date = existing.Date
	}
	if inv.DueDate == nil {
		inv.DueDate = existing.DueDate
	}
	if err := h.repos.Invoices.Update(c.Context(), orgID, &inv); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.Invoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Invoices", *fresh)
}

func isStatusOnlyInvoicePayload(m map[string]json.RawMessage) bool {
	if len(m) == 0 {
		return false
	}
	for k := range m {
		switch k {
		case "status", "Status":
			continue
		default:
			return false
		}
	}
	return true
}

// UpdateStatus changes only the Status field. Kept exported so it can be
// reused by PUT and POST routes.
func (h *InvoiceHandler) UpdateStatus(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		Status    string `json:"status"`
		StatusAlt string `json:"Status"`
	}](c)
	if err != nil {
		return err
	}
	status := body.Status
	if status == "" {
		status = body.StatusAlt
	}
	if !validInvoiceStatuses[status] {
		return fiber.NewError(fiber.StatusBadRequest, "invalid status")
	}
	if err := h.repos.Invoices.UpdateStatus(c.Context(), orgID, id, status); err != nil {
		return httpError(err)
	}
	inv, err := h.repos.Invoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Invoices", *inv)
}

// Delete applies Xero's soft-delete semantics: DRAFT/SUBMITTED → DELETED,
// AUTHORISED → VOIDED (with GL reversal). PAID/VOIDED/DELETED return 403.
func (h *InvoiceHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Invoices.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// Payments returns every payment recorded against the invoice.
func (h *InvoiceHandler) Payments(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	list, err := h.repos.Payments.ListForInvoice(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Payments": list})
}

// Email mirrors Xero's `POST /Invoices/{id}/Email` endpoint. Xero doesn't
// actually transmit any payload with this call — the invoice is dispatched to
// the contact's primary email address using the configured branding theme.
// We persist a history record so the audit trail shows the invoice has been
// sent, and (for test/staging environments) return a synthesised summary of
// the would-be email rather than wiring an SMTP provider directly.
func (h *InvoiceHandler) Email(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	inv, err := h.repos.Invoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	to := ""
	if inv.Contact != nil {
		to = inv.Contact.EmailAddress
	}
	// Mark "sent" in history. Safe to ignore errors — history is best-effort.
	_ = h.repos.History.Add(c.Context(), orgID, "invoices", id, models.HistoryRecord{
		Changes: "Sent",
		Details: "Invoice emailed to " + to,
	})
	return c.Status(fiber.StatusNoContent).SendString("")
}

// OnlineInvoice mirrors Xero's `GET /Invoices/{id}/OnlineInvoice` endpoint
// which returns a public payment-portal URL for the customer. We synthesise
// a deterministic URL based on the invoice ID so the frontend can render a
// "Pay online" button without any additional config.
func (h *InvoiceHandler) OnlineInvoice(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	inv, err := h.repos.Invoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	base := c.BaseURL()
	return c.JSON(fiber.Map{
		"OnlineInvoices": []fiber.Map{{
			"OnlineInvoiceUrl": base + "/pay/" + inv.InvoiceID.String(),
		}},
	})
}

func (h *InvoiceHandler) Summary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	s, err := h.repos.Invoices.Summary(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{
		"totalInvoices": s.TotalInvoices,
		"draft":         s.Draft,
		"authorised":    s.Authorised,
		"paid":          s.Paid,
		"overdue":       s.Overdue,
		"totalDue":      s.TotalDue,
		"totalPaid":     s.TotalPaid,
	})
}
