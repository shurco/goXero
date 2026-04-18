package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// QuoteHandler: https://developer.xero.com/documentation/api/accounting/quotes
type QuoteHandler struct {
	repos *repository.Repositories
}

func NewQuoteHandler(r *repository.Repositories) *QuoteHandler {
	return &QuoteHandler{repos: r}
}

type quoteRequest struct {
	models.Quote
	ContactID string `json:"ContactID"`
}

func (h *QuoteHandler) List(c fiber.Ctx) error {
	p := paginationFromQuery(c)
	list, total, err := h.repos.Quotes.List(c.Context(), middleware.OrganisationIDFrom(c), repository.QuoteFilter{Status: c.Query("status")}, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"Quotes": list, "Pagination": p})
}

func (h *QuoteHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	q, err := h.repos.Quotes.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Quotes", *q)
}

func (h *QuoteHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[quoteRequest](c)
	if err != nil {
		return err
	}
	q := req.Quote
	if cid, err := parseOptionalUUID(req.ContactID, "ContactID"); err != nil {
		return err
	} else if cid != nil {
		q.ContactID = cid
	} else if req.Contact != nil && req.Contact.ContactID != uuid.Nil {
		q.ContactID = &req.Contact.ContactID
	}
	if q.Status == "" {
		q.Status = models.QuoteStatusDraft
	}
	if q.LineAmountTypes == "" {
		q.LineAmountTypes = models.LineAmountTypesExclusive
	}
	if err := h.repos.Quotes.Create(c.Context(), middleware.OrganisationIDFrom(c), &q); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Quotes", q)
}

func (h *QuoteHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.Quotes.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.QuoteID = id
	if err := h.repos.Quotes.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.Quotes.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Quotes", *fresh)
}

func (h *QuoteHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Quotes.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// PurchaseOrderHandler: https://developer.xero.com/documentation/api/accounting/purchaseorders
type PurchaseOrderHandler struct {
	repos *repository.Repositories
}

func NewPurchaseOrderHandler(r *repository.Repositories) *PurchaseOrderHandler {
	return &PurchaseOrderHandler{repos: r}
}

type purchaseOrderRequest struct {
	models.PurchaseOrder
	ContactID string `json:"ContactID"`
}

func (h *PurchaseOrderHandler) List(c fiber.Ctx) error {
	p := paginationFromQuery(c)
	list, total, err := h.repos.PurchaseOrders.List(c.Context(), middleware.OrganisationIDFrom(c), repository.PurchaseOrderFilter{Status: c.Query("status")}, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"PurchaseOrders": list, "Pagination": p})
}

func (h *PurchaseOrderHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	po, err := h.repos.PurchaseOrders.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "PurchaseOrders", *po)
}

func (h *PurchaseOrderHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[purchaseOrderRequest](c)
	if err != nil {
		return err
	}
	po := req.PurchaseOrder
	if cid, err := parseOptionalUUID(req.ContactID, "ContactID"); err != nil {
		return err
	} else if cid != nil {
		po.ContactID = cid
	} else if req.Contact != nil && req.Contact.ContactID != uuid.Nil {
		po.ContactID = &req.Contact.ContactID
	}
	if po.Status == "" {
		po.Status = models.PurchaseOrderStatusDraft
	}
	if po.LineAmountTypes == "" {
		po.LineAmountTypes = models.LineAmountTypesExclusive
	}
	if err := h.repos.PurchaseOrders.Create(c.Context(), middleware.OrganisationIDFrom(c), &po); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "PurchaseOrders", po)
}

func (h *PurchaseOrderHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.PurchaseOrders.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.PurchaseOrderID = id
	if err := h.repos.PurchaseOrders.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.PurchaseOrders.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "PurchaseOrders", *fresh)
}

func (h *PurchaseOrderHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.PurchaseOrders.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
