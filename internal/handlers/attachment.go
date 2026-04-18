package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// AttachmentHandler exposes the polymorphic `/{Endpoint}/{Guid}/Attachments`
// routes documented at
// https://developer.xero.com/documentation/api/accounting/attachments.
//
// The route parameter `:subject` is mapped to the canonical Xero subject type
// via attachmentSubjectMap, so clients can call
// `/api/v1/Invoices/{id}/Attachments/filename.pdf`.
type AttachmentHandler struct {
	repos *repository.Repositories
}

func NewAttachmentHandler(r *repository.Repositories) *AttachmentHandler {
	return &AttachmentHandler{repos: r}
}

var attachmentSubjectMap = map[string]string{
	"invoices":          "INVOICE",
	"credit-notes":      "CREDITNOTE",
	"bank-transactions": "BANKTRANSACTION",
	"contacts":          "CONTACT",
	"accounts":          "ACCOUNT",
	"manual-journals":   "MANUALJOURNAL",
	"quotes":            "QUOTE",
	"purchase-orders":   "PURCHASEORDER",
	"receipts":          "RECEIPT",
	"expense-claims":    "EXPENSECLAIM",
}

func resolveSubject(c fiber.Ctx) (string, error) {
	raw := c.Params("subject")
	if v, ok := attachmentSubjectMap[raw]; ok {
		return v, nil
	}
	return "", fiber.NewError(fiber.StatusBadRequest, "unsupported subject")
}

func (h *AttachmentHandler) List(c fiber.Ctx) error {
	subject, err := resolveSubject(c)
	if err != nil {
		return err
	}
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	list, err := h.repos.Attachments.List(c.Context(), orgID, subject, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Attachments", list)
}

func (h *AttachmentHandler) Upload(c fiber.Ctx) error {
	subject, err := resolveSubject(c)
	if err != nil {
		return err
	}
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	filename := c.Params("filename")
	if filename == "" {
		return fiber.NewError(fiber.StatusBadRequest, "filename is required")
	}
	body := c.Body()
	if len(body) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "empty body")
	}
	mime := string(c.Request().Header.ContentType())
	includeOnline := c.Query("IncludeOnline") == "true"
	att, err := h.repos.Attachments.Upload(c.Context(), orgID, subject, id, filename, mime, body, includeOnline)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Attachments", *att)
}

func (h *AttachmentHandler) Fetch(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	aid, err := parseID(c, "attachmentId")
	if err != nil {
		return err
	}
	att, body, err := h.repos.Attachments.Fetch(c.Context(), orgID, aid)
	if err != nil {
		return httpError(err)
	}
	if att.MimeType != "" {
		c.Set(fiber.HeaderContentType, att.MimeType)
	}
	c.Set("Content-Disposition", "inline; filename=\""+att.FileName+"\"")
	return c.Send(body)
}

// HistoryHandler: `/{Endpoint}/{Guid}/History`.
type HistoryHandler struct {
	repos *repository.Repositories
}

func NewHistoryHandler(r *repository.Repositories) *HistoryHandler {
	return &HistoryHandler{repos: r}
}

func (h *HistoryHandler) List(c fiber.Ctx) error {
	subject, err := resolveSubject(c)
	if err != nil {
		return err
	}
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	list, err := h.repos.History.List(c.Context(), orgID, subject, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "HistoryRecords", list)
}

type historyNoteRequest struct {
	HistoryRecords []struct {
		Details string `json:"Details"`
	} `json:"HistoryRecords"`
}

func (h *HistoryHandler) AddNote(c fiber.Ctx) error {
	subject, err := resolveSubject(c)
	if err != nil {
		return err
	}
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	req, err := bindBody[historyNoteRequest](c)
	if err != nil {
		return err
	}
	if len(req.HistoryRecords) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "HistoryRecords is required")
	}
	userID := middleware.UserIDFrom(c)
	var ptr *uuid.UUID
	if userID != uuid.Nil {
		ptr = &userID
	}
	out := make([]models.HistoryRecord, 0, len(req.HistoryRecords))
	for _, r := range req.HistoryRecords {
		rec, err := h.repos.History.AddNote(c.Context(), orgID, subject, id, ptr, r.Details)
		if err != nil {
			return httpError(err)
		}
		out = append(out, *rec)
	}
	return rawList(c, fiber.StatusCreated, "HistoryRecords", out)
}
