package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

type ContactHandler struct {
	repos *repository.Repositories
}

func NewContactHandler(r *repository.Repositories) *ContactHandler {
	return &ContactHandler{repos: r}
}

func (h *ContactHandler) List(c fiber.Ctx) error {
	p := paginationFromQuery(c)
	filter := repository.ContactFilter{
		Status: c.Query("status"),
		Search: c.Query("search"),
	}
	if v := c.Query("isCustomer"); v != "" {
		b := v == "true"
		filter.IsCustomer = &b
	}
	if v := c.Query("isSupplier"); v != "" {
		b := v == "true"
		filter.IsSupplier = &b
	}
	list, total, err := h.repos.Contacts.List(c.Context(), middleware.OrganisationIDFrom(c), filter, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{
		"Contacts":   list,
		"Pagination": p,
	})
}

func (h *ContactHandler) Get(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	contact, err := h.repos.Contacts.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Contacts", *contact)
}

func (h *ContactHandler) Create(c fiber.Ctx) error {
	contact, err := bindBody[models.Contact](c)
	if err != nil {
		return err
	}
	if contact.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Name is required")
	}
	if contact.ContactStatus == "" {
		contact.ContactStatus = "ACTIVE"
	}
	if err := h.repos.Contacts.Create(c.Context(), middleware.OrganisationIDFrom(c), contact); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Contacts", *contact)
}

func (h *ContactHandler) Update(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	existing, err := h.repos.Contacts.GetByID(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if err := c.Bind().Body(existing); err != nil {
		return errInvalidPayload
	}
	existing.ContactID = id
	if err := h.repos.Contacts.Update(c.Context(), orgID, existing); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Contacts", *existing)
}

func (h *ContactHandler) Archive(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.Contacts.Archive(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}
