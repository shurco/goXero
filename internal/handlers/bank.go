package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// BankTransactionHandler covers spend/receive/transfer money as described at
// https://developer.xero.com/documentation/api/accounting/banktransactions.
type BankTransactionHandler struct {
	repos *repository.Repositories
}

func NewBankTransactionHandler(r *repository.Repositories) *BankTransactionHandler {
	return &BankTransactionHandler{repos: r}
}

type bankTxRequest struct {
	models.BankTransaction
	ContactID     string `json:"ContactID"`
	BankAccountID string `json:"BankAccountID"`
}

func (h *BankTransactionHandler) List(c fiber.Ctx) error {
	p := paginationFromQuery(c)
	bankAccountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	var isReconciled *bool
	if raw := c.Query("isReconciled"); raw != "" {
		v := raw == "true" || raw == "1"
		isReconciled = &v
	}
	list, total, err := h.repos.BankTransactions.List(c.Context(), middleware.OrganisationIDFrom(c), repository.BankTransactionFilter{
		Type:          c.Query("type"),
		Status:        c.Query("status"),
		BankAccountID: bankAccountID,
		IsReconciled:  isReconciled,
		Search:        c.Query("search"),
	}, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"BankTransactions": list, "Pagination": p})
}

func (h *BankTransactionHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	bt, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "BankTransactions", *bt)
}

func (h *BankTransactionHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[bankTxRequest](c)
	if err != nil {
		return err
	}
	bt := req.BankTransaction
	if cid, err := parseOptionalUUID(req.ContactID, "ContactID"); err != nil {
		return err
	} else if cid != nil {
		bt.ContactID = cid
	} else if req.Contact != nil && req.Contact.ContactID != uuid.Nil {
		bt.ContactID = &req.Contact.ContactID
	}
	if aid, err := parseOptionalUUID(req.BankAccountID, "BankAccountID"); err != nil {
		return err
	} else if aid != nil {
		bt.BankAccountID = aid
	} else if req.BankAccount != nil && req.BankAccount.AccountID != uuid.Nil {
		bt.BankAccountID = &req.BankAccount.AccountID
	}
	if bt.BankAccountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "BankAccountID is required")
	}
	if bt.Type == "" {
		bt.Type = models.BankTransactionTypeReceive
	}
	if bt.LineAmountTypes == "" {
		bt.LineAmountTypes = models.LineAmountTypesExclusive
	}
	if bt.Status == "" {
		bt.Status = "AUTHORISED"
	}
	if err := h.repos.BankTransactions.Create(c.Context(), middleware.OrganisationIDFrom(c), &bt); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankTransactions", bt)
}

// Update replaces a bank transaction — header, line items and the GL journal it
// posted. The reconcile screen calls this when a user edits the coding of a
// transaction before approving it; POST and PUT are both accepted because the
// SvelteKit client and the Xero SDKs disagree about which one to use.
func (h *BankTransactionHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	req, err := bindBody[bankTxRequest](c)
	if err != nil {
		return err
	}
	bt := req.BankTransaction
	bt.BankTransactionID = id
	// Fields the client did not send stay as they were: a PATCH-shaped caller
	// should not have to echo the whole record back to change one amount.
	if bt.BankAccountID == nil {
		bt.BankAccountID = existing.BankAccountID
	}
	if bt.ContactID == nil {
		bt.ContactID = existing.ContactID
	}
	if bt.Status == "" {
		bt.Status = existing.Status
	}
	if bt.LineAmountTypes == "" {
		bt.LineAmountTypes = existing.LineAmountTypes
	}
	if bt.Date == nil {
		bt.Date = existing.Date
	}
	if bt.CurrencyCode == "" {
		bt.CurrencyCode = existing.CurrencyCode
	}
	if len(bt.LineItems) == 0 {
		bt.LineItems = existing.LineItems
	}
	if err := h.repos.BankTransactions.Update(c.Context(), orgID, &bt); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "BankTransactions", *fresh)
}

// Reconcile approves (or un-approves) a transaction without touching its
// amounts. This is the endpoint the reconcile screen must call — the previous
// implementation POSTed a copy of the transaction instead, which booked the
// money twice.
func (h *BankTransactionHandler) Reconcile(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		IsReconciled *bool `json:"IsReconciled"`
	}](c)
	if err != nil {
		return err
	}
	reconciled := true
	if body.IsReconciled != nil {
		reconciled = *body.IsReconciled
	}
	if err := h.repos.BankTransactions.Reconcile(c.Context(), orgID, id, reconciled); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "BankTransactions", *fresh)
}

func (h *BankTransactionHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.BankTransactions.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// BankTransferHandler: https://developer.xero.com/documentation/api/accounting/banktransfers
type BankTransferHandler struct {
	repos *repository.Repositories
}

func NewBankTransferHandler(r *repository.Repositories) *BankTransferHandler {
	return &BankTransferHandler{repos: r}
}

func (h *BankTransferHandler) List(c fiber.Ctx) error {
	list, err := h.repos.BankTransfers.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"BankTransfers": list})
}

func (h *BankTransferHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	t, err := h.repos.BankTransfers.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "BankTransfers", *t)
}

func (h *BankTransferHandler) Create(c fiber.Ctx) error {
	t, err := bindBody[models.BankTransfer](c)
	if err != nil {
		return err
	}
	if t.FromBankAccountID == uuid.Nil || t.ToBankAccountID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "FromBankAccountID and ToBankAccountID are required")
	}
	if !t.Amount.IsPositive() {
		return fiber.NewError(fiber.StatusBadRequest, "Amount must be positive")
	}
	if err := h.repos.BankTransfers.Create(c.Context(), middleware.OrganisationIDFrom(c), t); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankTransfers", *t)
}
