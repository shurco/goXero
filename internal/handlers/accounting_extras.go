package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	middleware "github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// This file wires up the Xero "extra" accounting resources added in
// migration 00013 — Prepayments, Overpayments, RepeatingInvoices,
// BatchPayments, LinkedTransactions, Employees, Receipts, ExpenseClaims and
// a Users endpoint. All handlers share the envelope helpers defined in
// helpers.go so the only per-resource logic left here is payload validation
// and the repo call itself.

// ---------------------------------------------------------------------------
// Prepayments
// ---------------------------------------------------------------------------

type PrepaymentHandler struct{ repos *repository.Repositories }

func NewPrepaymentHandler(r *repository.Repositories) *PrepaymentHandler {
	return &PrepaymentHandler{repos: r}
}

func (h *PrepaymentHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Prepayments.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Prepayments", list)
}

func (h *PrepaymentHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	p, err := h.repos.Prepayments.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Prepayments", *p)
}

func (h *PrepaymentHandler) Create(c fiber.Ctx) error {
	p, err := bindBody[models.Prepayment](c)
	if err != nil {
		return err
	}
	if p.Type == "" {
		p.Type = models.PrepaymentTypeReceive
	}
	if p.Total.IsZero() {
		return fiber.NewError(fiber.StatusBadRequest, "Total is required")
	}
	if err := h.repos.Prepayments.Create(c.Context(), middleware.OrganisationIDFrom(c), p); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Prepayments", *p)
}

func (h *PrepaymentHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Prepayments.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Overpayments
// ---------------------------------------------------------------------------

type OverpaymentHandler struct{ repos *repository.Repositories }

func NewOverpaymentHandler(r *repository.Repositories) *OverpaymentHandler {
	return &OverpaymentHandler{repos: r}
}

func (h *OverpaymentHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Overpayments.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Overpayments", list)
}

func (h *OverpaymentHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	o, err := h.repos.Overpayments.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Overpayments", *o)
}

func (h *OverpaymentHandler) Create(c fiber.Ctx) error {
	o, err := bindBody[models.Overpayment](c)
	if err != nil {
		return err
	}
	if o.Type == "" {
		o.Type = models.OverpaymentTypeReceive
	}
	if o.Total.IsZero() {
		return fiber.NewError(fiber.StatusBadRequest, "Total is required")
	}
	if err := h.repos.Overpayments.Create(c.Context(), middleware.OrganisationIDFrom(c), o); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Overpayments", *o)
}

func (h *OverpaymentHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Overpayments.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Repeating invoices
// ---------------------------------------------------------------------------

type RepeatingInvoiceHandler struct{ repos *repository.Repositories }

func NewRepeatingInvoiceHandler(r *repository.Repositories) *RepeatingInvoiceHandler {
	return &RepeatingInvoiceHandler{repos: r}
}

func (h *RepeatingInvoiceHandler) List(c fiber.Ctx) error {
	list, err := h.repos.RepeatingInvoices.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "RepeatingInvoices", list)
}

func (h *RepeatingInvoiceHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	ri, err := h.repos.RepeatingInvoices.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "RepeatingInvoices", *ri)
}

func (h *RepeatingInvoiceHandler) Create(c fiber.Ctx) error {
	ri, err := bindBody[models.RepeatingInvoice](c)
	if err != nil {
		return err
	}
	if ri.Type == "" {
		ri.Type = models.InvoiceTypeAccRec
	}
	if len(ri.LineItems) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "LineItems is required")
	}
	if err := h.repos.RepeatingInvoices.Create(c.Context(), middleware.OrganisationIDFrom(c), ri); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "RepeatingInvoices", *ri)
}

func (h *RepeatingInvoiceHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.RepeatingInvoices.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Batch payments
// ---------------------------------------------------------------------------

type BatchPaymentHandler struct{ repos *repository.Repositories }

func NewBatchPaymentHandler(r *repository.Repositories) *BatchPaymentHandler {
	return &BatchPaymentHandler{repos: r}
}

func (h *BatchPaymentHandler) List(c fiber.Ctx) error {
	list, err := h.repos.BatchPayments.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "BatchPayments", list)
}

func (h *BatchPaymentHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	b, err := h.repos.BatchPayments.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "BatchPayments", *b)
}

func (h *BatchPaymentHandler) Create(c fiber.Ctx) error {
	bp, err := bindBody[models.BatchPayment](c)
	if err != nil {
		return err
	}
	if bp.AccountID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "AccountID is required")
	}
	if len(bp.Payments) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "Payments is required")
	}
	if err := h.repos.BatchPayments.Create(c.Context(), middleware.OrganisationIDFrom(c), bp); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BatchPayments", *bp)
}

// ---------------------------------------------------------------------------
// Linked transactions
// ---------------------------------------------------------------------------

type LinkedTransactionHandler struct{ repos *repository.Repositories }

func NewLinkedTransactionHandler(r *repository.Repositories) *LinkedTransactionHandler {
	return &LinkedTransactionHandler{repos: r}
}

func (h *LinkedTransactionHandler) List(c fiber.Ctx) error {
	list, err := h.repos.LinkedTransactions.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "LinkedTransactions", list)
}

func (h *LinkedTransactionHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	l, err := h.repos.LinkedTransactions.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "LinkedTransactions", *l)
}

func (h *LinkedTransactionHandler) Create(c fiber.Ctx) error {
	l, err := bindBody[models.LinkedTransaction](c)
	if err != nil {
		return err
	}
	if l.SourceTransactionID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "SourceTransactionID is required")
	}
	if err := h.repos.LinkedTransactions.Create(c.Context(), middleware.OrganisationIDFrom(c), l); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "LinkedTransactions", *l)
}

func (h *LinkedTransactionHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.LinkedTransactions.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Employees
// ---------------------------------------------------------------------------

type EmployeeHandler struct{ repos *repository.Repositories }

func NewEmployeeHandler(r *repository.Repositories) *EmployeeHandler {
	return &EmployeeHandler{repos: r}
}

func (h *EmployeeHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Employees.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Employees", list)
}

func (h *EmployeeHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	e, err := h.repos.Employees.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Employees", *e)
}

func (h *EmployeeHandler) Create(c fiber.Ctx) error {
	e, err := bindBody[models.Employee](c)
	if err != nil {
		return err
	}
	if e.FirstName == "" {
		return fiber.NewError(fiber.StatusBadRequest, "FirstName is required")
	}
	if err := h.repos.Employees.Create(c.Context(), middleware.OrganisationIDFrom(c), e); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Employees", *e)
}

func (h *EmployeeHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.Employees.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.EmployeeID = id
	if err := h.repos.Employees.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Employees", *existing)
}

func (h *EmployeeHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Employees.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Receipts
// ---------------------------------------------------------------------------

type ReceiptHandler struct{ repos *repository.Repositories }

func NewReceiptHandler(r *repository.Repositories) *ReceiptHandler {
	return &ReceiptHandler{repos: r}
}

func (h *ReceiptHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Receipts.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Receipts", list)
}

func (h *ReceiptHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	rc, err := h.repos.Receipts.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Receipts", *rc)
}

func (h *ReceiptHandler) Create(c fiber.Ctx) error {
	rc, err := bindBody[models.Receipt](c)
	if err != nil {
		return err
	}
	if len(rc.LineItems) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "LineItems is required")
	}
	if rc.UserID == nil {
		uid := middleware.UserIDFrom(c)
		if uid != uuid.Nil {
			rc.UserID = &uid
		}
	}
	if err := h.repos.Receipts.Create(c.Context(), middleware.OrganisationIDFrom(c), rc); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Receipts", *rc)
}

func (h *ReceiptHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Receipts.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Expense claims
// ---------------------------------------------------------------------------

type ExpenseClaimHandler struct{ repos *repository.Repositories }

func NewExpenseClaimHandler(r *repository.Repositories) *ExpenseClaimHandler {
	return &ExpenseClaimHandler{repos: r}
}

type expenseClaimRequest struct {
	models.ExpenseClaim
	ReceiptIDs []uuid.UUID `json:"ReceiptIDs,omitempty"`
}

func (h *ExpenseClaimHandler) List(c fiber.Ctx) error {
	list, err := h.repos.ExpenseClaims.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "ExpenseClaims", list)
}

func (h *ExpenseClaimHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	e, err := h.repos.ExpenseClaims.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "ExpenseClaims", *e)
}

func (h *ExpenseClaimHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[expenseClaimRequest](c)
	if err != nil {
		return err
	}
	ec := req.ExpenseClaim
	if len(req.ReceiptIDs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "ReceiptIDs is required")
	}
	if ec.UserID == nil {
		uid := middleware.UserIDFrom(c)
		if uid != uuid.Nil {
			ec.UserID = &uid
		}
	}
	if err := h.repos.ExpenseClaims.Create(c.Context(), middleware.OrganisationIDFrom(c), &ec, req.ReceiptIDs); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "ExpenseClaims", ec)
}

func (h *ExpenseClaimHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.ExpenseClaims.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// Users (Xero GET /Users)
// ---------------------------------------------------------------------------

type UsersHandler struct{ repos *repository.Repositories }

func NewUsersHandler(r *repository.Repositories) *UsersHandler { return &UsersHandler{repos: r} }

// List returns every user that has access to the current organisation.
func (h *UsersHandler) List(c fiber.Ctx) error {
	list, err := h.repos.Users.ListForOrganisation(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Users", list)
}
