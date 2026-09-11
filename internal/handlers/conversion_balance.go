package handlers

import (
	"github.com/gofiber/fiber/v3"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// ConversionBalanceHandler serves the opening balances an organisation brings in
// when it converts to goXero: the balances themselves, the date they are stated
// as at, and the lock that protects them.
type ConversionBalanceHandler struct {
	repos *repository.Repositories
}

func NewConversionBalanceHandler(r *repository.Repositories) *ConversionBalanceHandler {
	return &ConversionBalanceHandler{repos: r}
}

func (h *ConversionBalanceHandler) Get(c fiber.Ctx) error {
	cb, err := h.repos.ConversionBalances.Get(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"ConversionBalance": cb})
}

// Update replaces the conversion balances.
//
// A locked conversion may be changed only by an organisation administrator. The
// lock exists to stop accidental edits, and Xero's answer to it is a role that
// is allowed past it -- "Only users with Adviser roles will be able to make any
// changes" on the screen itself. So a member with any other role can neither
// edit the balances nor lift the lock.
func (h *ConversionBalanceHandler) Update(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	cb, err := bindBody[models.ConversionBalance](c)
	if err != nil {
		return err
	}
	current, err := h.repos.ConversionBalances.Get(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}
	if current.Locked {
		role, err := h.repos.Users.OrganisationRole(c.Context(), middleware.UserIDFrom(c), orgID)
		if err != nil {
			return httpError(err)
		}
		if role != models.OrganisationRoleAdmin {
			return fiber.NewError(fiber.StatusForbidden,
				"the conversion balances are locked; an administrator can unlock them")
		}
	}
	if err := h.repos.ConversionBalances.Save(c.Context(), orgID, cb); err != nil {
		return httpError(err)
	}
	fresh, err := h.repos.ConversionBalances.Get(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"ConversionBalance": fresh})
}
