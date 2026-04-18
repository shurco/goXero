package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// ContactGroupHandler: https://developer.xero.com/documentation/api/accounting/contactgroups
type ContactGroupHandler struct {
	repos *repository.Repositories
}

func NewContactGroupHandler(r *repository.Repositories) *ContactGroupHandler {
	return &ContactGroupHandler{repos: r}
}

func (h *ContactGroupHandler) List(c fiber.Ctx) error {
	list, err := h.repos.ContactGroups.List(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "ContactGroups", list)
}

func (h *ContactGroupHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	g, err := h.repos.ContactGroups.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "ContactGroups", *g)
}

func (h *ContactGroupHandler) Create(c fiber.Ctx) error {
	g, err := bindBody[models.ContactGroup](c)
	if err != nil {
		return err
	}
	if g.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	if g.Status == "" {
		g.Status = "ACTIVE"
	}
	if err := h.repos.ContactGroups.Create(c.Context(), middleware.OrganisationIDFrom(c), g); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "ContactGroups", *g)
}

func (h *ContactGroupHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.ContactGroups.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.ContactGroupID = id
	if err := h.repos.ContactGroups.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "ContactGroups", *existing)
}

func (h *ContactGroupHandler) Delete(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.ContactGroups.Delete(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

type contactGroupMembersRequest struct {
	Contacts []struct {
		ContactID string `json:"ContactID"`
	} `json:"Contacts"`
}

// AddContacts mirrors `PUT /ContactGroups/{id}/Contacts`.
func (h *ContactGroupHandler) AddContacts(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	req, err := bindBody[contactGroupMembersRequest](c)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(req.Contacts))
	for _, ct := range req.Contacts {
		cid, err := uuid.Parse(ct.ContactID)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid ContactID")
		}
		ids = append(ids, cid)
	}
	if len(ids) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "Contacts is required")
	}
	if err := h.repos.ContactGroups.AddContacts(c.Context(), orgID, id, ids); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.ContactGroups.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "ContactGroups", *fresh)
}

// RemoveContact mirrors `DELETE /ContactGroups/{id}/Contacts/{ContactID}`.
func (h *ContactGroupHandler) RemoveContact(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	contactID, err := parseID(c, "contactId")
	if err != nil {
		return err
	}
	if err := h.repos.ContactGroups.RemoveContact(c.Context(), orgID, id, contactID); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
