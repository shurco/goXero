package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type PaymentHandler struct {
	repos *repository.Repositories
}

func NewPaymentHandler(r *repository.Repositories) *PaymentHandler {
	return &PaymentHandler{repos: r}
}

type createPaymentRequest struct {
	InvoiceID    string           `json:"invoiceId"`
	AccountID    string           `json:"accountId"`
	Date         string           `json:"date"`
	Amount       decimal.Decimal  `json:"amount"`
	Reference    string           `json:"reference"`
	PaymentType  string           `json:"paymentType"`
	Status       string           `json:"status"`
	CurrencyRate *decimal.Decimal `json:"currencyRate,omitempty"`
}

func (h *PaymentHandler) List(c fiber.Ctx) error {
	p := paginationFromQuery(c)
	list, total, err := h.repos.Payments.List(c.Context(), middleware.OrganisationIDFrom(c), p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{
		"Payments":   list,
		"Pagination": p,
	})
}

func (h *PaymentHandler) Create(c fiber.Ctx) error {
	req, err := bindBody[createPaymentRequest](c)
	if err != nil {
		return err
	}
	if req.Amount.IsZero() {
		return fiber.NewError(fiber.StatusBadRequest, "amount is required")
	}
	payment := models.Payment{
		Amount:       req.Amount,
		Reference:    req.Reference,
		PaymentType:  req.PaymentType,
		Status:       req.Status,
		CurrencyRate: req.CurrencyRate,
	}
	if payment.PaymentType == "" {
		payment.PaymentType = "ACCRECPAYMENT"
	}
	if req.Date != "" {
		d, err := parseYMD(req.Date)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid date (expected YYYY-MM-DD)")
		}
		payment.Date = d
	} else {
		payment.Date = time.Now().UTC()
	}
	invID, err := parseOptionalUUID(req.InvoiceID, "invoiceId")
	if err != nil {
		return err
	}
	payment.InvoiceID = invID
	accID, err := parseOptionalUUID(req.AccountID, "accountId")
	if err != nil {
		return err
	}
	payment.AccountID = accID
	if err := h.repos.Payments.Create(c.Context(), middleware.OrganisationIDFrom(c), &payment); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Payments", payment)
}

func (h *PaymentHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	p, err := h.repos.Payments.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Payments", *p)
}

// Delete soft-voids a payment (Xero treats DELETE /Payments/{id} as Void).
func (h *PaymentHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Payments.Void(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
