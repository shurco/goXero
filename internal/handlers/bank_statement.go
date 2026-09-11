package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/bankrules"
	"github.com/shurco/goxero/internal/bankstatement"
	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// BankStatementHandler is the reconcile inbox and the manual statement import
// wizard — the two halves of Xero's Bank Accounts screen that the feed-only
// implementation was missing.
//
// Feed lines and imported lines live in the same table and go through the same
// endpoints, because that is how Xero treats them: once a line is in the inbox
// it does not matter where it came from.
type BankStatementHandler struct {
	repos *repository.Repositories
}

func NewBankStatementHandler(r *repository.Repositories) *BankStatementHandler {
	return &BankStatementHandler{repos: r}
}

// ListStatementLines is the reconcile inbox. Query parameters:
//
//	bankAccountId  — the ledger bank account being reconciled
//	feedAccountId  — a specific Open Banking feed account
//	importId       — the lines created by one manual import
//	status         — NEW | IMPORTED | IGNORED
//	source         — FEED | IMPORT
//	search         — free text over payee/description/reference
//	fromDate/toDate— inclusive date bounds
//	unreconciled   — bool, default true: only lines still awaiting a decision
//	suggestions    — bool: also evaluate bank rules and return suggestions
func (h *BankStatementHandler) ListStatementLines(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	f := repository.StatementLineFilter{
		Status:           c.Query("status"),
		Source:           strings.ToUpper(c.Query("source")),
		Search:           c.Query("search"),
		UnreconciledOnly: boolParam(c, "unreconciled", c.Query("status") == ""),
	}
	var err error
	if f.BankAccountID, err = optionalQueryUUID(c, "bankAccountId"); err != nil {
		return err
	}
	if f.FeedAccountID, err = optionalQueryUUID(c, "feedAccountId"); err != nil {
		return err
	}
	if f.ImportID, err = optionalQueryUUID(c, "importId"); err != nil {
		return err
	}
	if f.From, err = optionalQueryDate(c, "fromDate"); err != nil {
		return err
	}
	if f.To, err = optionalQueryDate(c, "toDate"); err != nil {
		return err
	}

	p := paginationFromQuery(c)
	items, total, err := h.repos.BankStatements.ListLines(c.Context(), orgID, f, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total

	if boolParam(c, "suggestions", false) {
		if err := h.attachSuggestions(c, orgID, items); err != nil {
			return err
		}
	}
	return c.JSON(fiber.Map{"StatementLines": items, "Pagination": p})
}

func (h *BankStatementHandler) GetStatementLine(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	lines := []models.BankStatementLine{*line}
	if err := h.attachSuggestions(c, orgID, lines); err != nil {
		return err
	}
	return rawOne(c, fiber.StatusOK, "StatementLines", lines[0])
}

// Balance returns the pair of numbers at the top of the reconcile screen.
func (h *BankStatementHandler) Balance(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	accountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	if accountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "bankAccountId is required")
	}
	b, err := h.repos.BankStatements.AccountBalance(c.Context(), orgID, *accountID)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Balance": b})
}

// attachSuggestions evaluates the tenant's bank rules against the given lines
// and fills in the advisory coding. Rules are loaded once, in evaluation order.
func (h *BankStatementHandler) attachSuggestions(c fiber.Ctx, orgID uuid.UUID, lines []models.BankStatementLine) error {
	if len(lines) == 0 {
		return nil
	}
	rules, err := h.repos.BankRules.List(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}
	for i := range lines {
		cand := candidateFor(lines[i])
		if s := bankrules.Suggest(rules, cand); s != nil {
			lines[i].Suggestions = []models.BankRuleSuggestion{*s}
			if s.AccountID != "" {
				lines[i].AccountID = uuidPtr(s.AccountID)
			}
		}
	}
	return nil
}

func candidateFor(l models.BankStatementLine) bankrules.Candidate {
	cand := bankrules.Candidate{
		Payee:        firstNonBlank(l.Payee, l.Counterparty),
		Description:  l.Description,
		Reference:    l.Reference,
		ChequeNumber: l.ChequeNumber,
		Amount:       l.Amount,
	}
	if l.BankAccountID != nil {
		cand.BankAccountID = l.BankAccountID.String()
	}
	return cand
}

func uuidPtr(s string) *uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &id
}

// Ignore / Unignore move a line in and out of the inbox. Xero allows both, so
// neither is a terminal state.
func (h *BankStatementHandler) IgnoreStatementLine(c fiber.Ctx) error {
	return h.setLineStatus(c, models.BankFeedLineStatusIgnored)
}

func (h *BankStatementHandler) UnignoreStatementLine(c fiber.Ctx) error {
	return h.setLineStatus(c, models.BankFeedLineStatusNew)
}

func (h *BankStatementHandler) setLineStatus(c fiber.Ctx, status string) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.BankStatements.SetLineStatus(c.Context(), orgID, id, status); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// BulkLineAction applies ignore/unignore to a set of lines — the checkboxes in
// the cash-coding grid.
func (h *BankStatementHandler) BulkLineAction(c fiber.Ctx) error {
	body, err := bindBody[struct {
		StatementLineIDs []uuid.UUID `json:"StatementLineIDs"`
		Action           string      `json:"Action"` // IGNORE | UNIGNORE
	}](c)
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)
	status := models.BankFeedLineStatusIgnored
	if strings.EqualFold(body.Action, "UNIGNORE") {
		status = models.BankFeedLineStatusNew
	}
	for _, id := range body.StatementLineIDs {
		if err := h.repos.BankStatements.SetLineStatus(c.Context(), orgID, id, status); err != nil {
			return httpError(err)
		}
	}
	return noContent(c)
}

// ApplyRule runs the bank rules over the given lines and returns the coding
// they suggest, without saving anything — the "Apply rule" button in cash
// coding. Passing no IDs applies the rules to the whole unreconciled inbox of
// the account.
func (h *BankStatementHandler) ApplyRule(c fiber.Ctx) error {
	body, err := bindBody[struct {
		BankAccountID    *uuid.UUID  `json:"BankAccountID"`
		StatementLineIDs []uuid.UUID `json:"StatementLineIDs"`
	}](c)
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)
	rules, err := h.repos.BankRules.List(c.Context(), orgID)
	if err != nil {
		return httpError(err)
	}
	lines, err := h.resolveLines(c, orgID, body.BankAccountID, body.StatementLineIDs)
	if err != nil {
		return err
	}
	for i := range lines {
		s := bankrules.Suggest(rules, candidateFor(lines[i]))
		if s != nil {
			lines[i].Suggestions = []models.BankRuleSuggestion{*s}
			lines[i].AccountID = uuidPtr(s.AccountID)
			lines[i].TaxType = s.TaxType
		}
	}
	return c.JSON(fiber.Map{"StatementLines": lines})
}

// resolveLines loads the lines named by IDs, or the unreconciled inbox of an
// account when no IDs are given.
func (h *BankStatementHandler) resolveLines(c fiber.Ctx, orgID uuid.UUID, bankAccountID *uuid.UUID, ids []uuid.UUID) ([]models.BankStatementLine, error) {
	if len(ids) > 0 {
		out := make([]models.BankStatementLine, 0, len(ids))
		for _, id := range ids {
			line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
			if err != nil {
				return nil, httpError(err)
			}
			out = append(out, *line)
		}
		return out, nil
	}
	if bankAccountID == nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "pass BankAccountID or StatementLineIDs")
	}
	lines, err := h.repos.BankStatements.UnreconciledLinesForAccount(c.Context(), orgID, *bankAccountID)
	if err != nil {
		return nil, httpError(err)
	}
	return lines, nil
}

// LineCoding is one row of the cash-coding grid: which line, and what the user
// wants it coded to.
type LineCoding struct {
	StatementLineID string `json:"StatementLineID"`
	AccountCode     string `json:"AccountCode"`
	TaxType         string `json:"TaxType"`
	ContactID       string `json:"ContactID"`
	Reference       string `json:"Reference"`
	Description     string `json:"Description"`
}

// CashCode saves the cash-coding grid: every checked row becomes a bank
// transaction against the chosen account, and the row is removed from the
// inbox. This is Xero's "Save & Reconcile All".
func (h *BankStatementHandler) CashCode(c fiber.Ctx) error {
	body, err := bindBody[struct {
		Rows []LineCoding `json:"Rows"`
	}](c)
	if err != nil {
		return err
	}
	if len(body.Rows) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "no rows to code")
	}
	orgID := middleware.OrganisationIDFrom(c)
	userID := middleware.UserIDFrom(c)
	codedIDs := make([]uuid.UUID, 0, len(body.Rows))
	var coded int
	for i, row := range body.Rows {
		if strings.TrimSpace(row.AccountCode) == "" {
			return fiber.NewError(fiber.StatusBadRequest,
				fmt.Sprintf("row %d: AccountCode is required", i+1))
		}
		lineID, err := uuid.Parse(row.StatementLineID)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("row %d: invalid StatementLineID", i+1))
		}
		line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, lineID)
		if err != nil {
			return httpError(err)
		}
		// Claim before coding: two tabs racing on one line is normal, and only
		// the one that wins the claim may post a transaction for it.
		err = h.withClaimedLine(c.Context(), orgID, lineID, func() (*uuid.UUID, error) {
			bt, cerr := h.createTransactionFromLine(c, orgID, line, lineRequest{
				AccountCode: row.AccountCode,
				TaxType:     row.TaxType,
				ContactID:   row.ContactID,
				Reference:   row.Reference,
				Description: row.Description,
			})
			if cerr != nil {
				return nil, cerr
			}
			return &bt.BankTransactionID, nil
		})
		if err != nil {
			if errors.Is(err, repository.ErrAlreadyExists) {
				continue // already coded by another tab; nothing to do
			}
			return httpError(err)
		}
		codedIDs = append(codedIDs, lineID)
		coded++
	}
	if err := h.repos.BankStatements.MarkLinesCoded(c.Context(), orgID, codedIDs, &userID); err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Coded": coded})
}

// lineRequest carries the user's overrides when a line becomes a transaction.
type lineRequest struct {
	BankAccountID *uuid.UUID
	AccountCode   string
	TaxType       string
	ContactID     string
	Reference     string
	Description   string
	TaxAmount     *decimal.Decimal
}

// CreateFromLine turns one statement line into a new bank transaction — the
// "Create" tab of the Xero reconcile screen.
func (h *BankStatementHandler) CreateFromLine(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		BankAccountID string           `json:"BankAccountID"`
		AccountCode   string           `json:"AccountCode"`
		TaxType       string           `json:"TaxType"`
		ContactID     string           `json:"ContactID"`
		Reference     string           `json:"Reference"`
		Description   string           `json:"Description"`
		TaxAmount     *decimal.Decimal `json:"TaxAmount"`
	}](c)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body.AccountCode) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "AccountCode is required")
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	bankAccountID, err := parseOptionalUUID(body.BankAccountID, "BankAccountID")
	if err != nil {
		return err
	}
	// Claim the line before posting: a second tab that got here first loses the
	// claim and is told the line is already reconciled, instead of both tabs
	// posting a transaction for it.
	var bt *models.BankTransaction
	if err := h.withClaimedLine(c.Context(), orgID, id, func() (*uuid.UUID, error) {
		created, cerr := h.createTransactionFromLine(c, orgID, line, lineRequest{
			BankAccountID: bankAccountID,
			AccountCode:   body.AccountCode,
			TaxType:       body.TaxType,
			ContactID:     body.ContactID,
			Reference:     body.Reference,
			Description:   body.Description,
			TaxAmount:     body.TaxAmount,
		})
		if cerr != nil {
			return nil, cerr
		}
		bt = created
		return &bt.BankTransactionID, nil
	}); err != nil {
		return lostClaimError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankTransactions", *bt)
}

// createTransactionFromLine is the single place a statement line is turned into
// a ledger transaction, shared by Create, CashCode and the bank-rule "apply"
// path so their behaviour cannot drift apart.
func (h *BankStatementHandler) createTransactionFromLine(c fiber.Ctx, orgID uuid.UUID, line *models.BankStatementLine, req lineRequest) (*models.BankTransaction, error) {
	bankAccountID := req.BankAccountID
	if bankAccountID == nil {
		bankAccountID = line.BankAccountID
	}
	if bankAccountID == nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "the statement line has no bank account; pass BankAccountID")
	}
	txType := models.BankTransactionTypeReceive
	amount := line.Amount
	if amount.IsNegative() {
		txType = models.BankTransactionTypeSpend
		amount = amount.Neg()
	}
	posted := line.PostedAt
	description := firstNonBlank(req.Description, line.Description, line.Payee, line.Counterparty)
	ref := firstNonBlank(req.Reference, line.Reference, line.ChequeNumber)

	bt := &models.BankTransaction{
		Type:            txType,
		BankAccountID:   bankAccountID,
		IsReconciled:    true,
		Date:            &posted,
		Reference:       ref,
		CurrencyCode:    line.CurrencyCode,
		Status:          "AUTHORISED",
		LineAmountTypes: models.LineAmountTypesInclusive,
		LineItems: []models.LineItem{{
			Description: description,
			Quantity:    decimal.NewFromInt(1),
			UnitAmount:  amount,
			AccountCode: req.AccountCode,
			TaxType:     req.TaxType,
			TaxAmount:   zeroIfNil(req.TaxAmount),
		}},
	}
	if req.ContactID != "" {
		cid, err := uuid.Parse(strings.TrimSpace(req.ContactID))
		if err != nil {
			return nil, fiber.NewError(fiber.StatusBadRequest, "invalid ContactID")
		}
		bt.ContactID = &cid
	}
	if err := h.repos.BankTransactions.Create(c.Context(), orgID, bt); err != nil {
		return nil, httpError(err)
	}
	return bt, nil
}

// MatchCandidates lists the transactions a statement line could be matched to —
// the "Find & match" search. Matching in Xero is on date and amount; the search
// is deliberately generous (same amount within a window) because the user is
// looking for something they know exists.
func (h *BankStatementHandler) MatchCandidates(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	search := c.Query("search")
	p := paginationFromQuery(c)
	accountID := line.BankAccountID
	candidates, total, err := h.repos.BankTransactions.List(c.Context(), orgID, repository.BankTransactionFilter{
		BankAccountID: accountID,
		Search:        search,
	}, p)
	if err != nil {
		return httpError(err)
	}
	// Rank the exact amount-and-date matches first: those are overwhelmingly
	// what the user is looking for.
	exact := make([]models.BankTransaction, 0, len(candidates))
	rest := make([]models.BankTransaction, 0, len(candidates))
	for _, bt := range candidates {
		if bt.Date != nil && sameDay(*bt.Date, line.PostedAt) &&
			bt.Total.Equal(line.Amount.Abs()) && sameDirection(bt, line) {
			exact = append(exact, bt)
			continue
		}
		rest = append(rest, bt)
	}
	p.Total = total
	return c.JSON(fiber.Map{
		"BankTransactions": append(exact, rest...),
		"Pagination":       p,
	})
}

// Match links a statement line to an existing bank transaction and reconciles
// it. Both halves are recorded: the line points at the transaction and the
// transaction is marked reconciled, which is what removes it from the inbox.
func (h *BankStatementHandler) Match(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		BankTransactionID uuid.UUID `json:"BankTransactionID"`
	}](c)
	if err != nil {
		return err
	}
	if body.BankTransactionID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "BankTransactionID is required")
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	existing, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, body.BankTransactionID)
	if err != nil {
		return httpError(err)
	}
	// A line may only be matched to a transaction on the same bank account:
	// reconciling against another account's transaction would mark the wrong
	// balance correct. A feed line with no ledger account yet is exempt.
	if line.BankAccountID != nil && existing.BankAccountID != nil && *existing.BankAccountID != *line.BankAccountID {
		return fiber.NewError(fiber.StatusBadRequest, "the transaction belongs to a different bank account")
	}
	if err := h.withClaimedLine(c.Context(), orgID, id, func() (*uuid.UUID, error) {
		if err := h.repos.BankTransactions.Reconcile(c.Context(), orgID, body.BankTransactionID, true); err != nil {
			return nil, httpError(err)
		}
		return &body.BankTransactionID, nil
	}); err != nil {
		return lostClaimError(err)
	}
	bt, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, body.BankTransactionID)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "BankTransactions", *bt)
}

// Transfer creates a bank transfer between two of the tenant's accounts for a
// statement line — the "Transfer" tab. The line is reconciled as part of it.
func (h *BankStatementHandler) Transfer(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		FromBankAccountID string `json:"FromBankAccountID"`
		ToBankAccountID   string `json:"ToBankAccountID"`
		Reference         string `json:"Reference"`
	}](c)
	if err != nil {
		return err
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	from, err := parseOptionalUUID(body.FromBankAccountID, "FromBankAccountID")
	if err != nil {
		return err
	}
	to, err := parseOptionalUUID(body.ToBankAccountID, "ToBankAccountID")
	if err != nil {
		return err
	}
	if from == nil {
		from = line.BankAccountID
	}
	if from == nil || to == nil {
		return fiber.NewError(fiber.StatusBadRequest, "both FromBankAccountID and ToBankAccountID are required")
	}
	// The user picks the account on the other side of the transfer; which way
	// the money moves follows from the line's sign. A debit sent money out of
	// this account, so this account is the source; a credit brought money in, so
	// the account the user picked is.
	if !line.Amount.IsNegative() {
		from, to = to, from
	}
	transfer := models.BankTransfer{
		FromBankAccountID: *from,
		ToBankAccountID:   *to,
		Amount:            line.Amount.Abs(),
		Date:              line.PostedAt,
		Reference:         firstNonBlank(body.Reference, line.Reference),
	}
	if err := h.withClaimedLine(c.Context(), orgID, id, func() (*uuid.UUID, error) {
		// A transfer posts both legs itself; the line only needs recording.
		return nil, h.repos.BankTransfers.Create(c.Context(), orgID, &transfer)
	}); err != nil {
		return lostClaimError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankTransfers", transfer)
}

// AutoReconcile is Xero's "Ok, let's reconcile": it matches every inbox line to
// an unreconciled transaction with the same date and amount. Anything it is not
// certain about is left alone for the user.
func (h *BankStatementHandler) AutoReconcile(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	accountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	if accountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "bankAccountId is required")
	}
	ctx := c.Context()
	lines, err := h.repos.BankStatements.UnreconciledLinesForAccount(ctx, orgID, *accountID)
	if err != nil {
		return httpError(err)
	}
	candidates, _, err := h.repos.BankTransactions.List(ctx, orgID, repository.BankTransactionFilter{
		BankAccountID: accountID,
		IsReconciled:  boolPtr(false),
	}, models.Pagination{Page: 1, PageSize: 500})
	if err != nil {
		return httpError(err)
	}

	used := map[uuid.UUID]bool{}
	var matched int
	for i := range lines {
		line := &lines[i]
		for _, bt := range candidates {
			if used[bt.BankTransactionID] || !sameDirection(bt, line) {
				continue
			}
			if bt.Date == nil || !sameDay(*bt.Date, line.PostedAt) {
				continue
			}
			if !bt.Total.Equal(line.Amount.Abs()) {
				continue
			}
			// Claim the line first so two people running this at once cannot
			// reconcile the same line twice; a line already being coded by
			// someone else is simply skipped.
			err := h.withClaimedLine(ctx, orgID, line.StatementLineID, func() (*uuid.UUID, error) {
				if rerr := h.repos.BankTransactions.Reconcile(ctx, orgID, bt.BankTransactionID, true); rerr != nil {
					return nil, rerr
				}
				return &bt.BankTransactionID, nil
			})
			if errors.Is(err, repository.ErrAlreadyExists) {
				break
			}
			if err != nil {
				return httpError(err)
			}
			used[bt.BankTransactionID] = true
			matched++
			break
		}
	}
	return c.JSON(fiber.Map{
		"Matched":   matched,
		"Scanned":   len(lines),
		"Remaining": len(lines) - matched,
	})
}

// ---------------------------------------------------------------------------
// Manual statement import wizard
// ---------------------------------------------------------------------------

// ParseStatement is step 1 of the wizard: the uploaded file is parsed and
// staged, and the wizard is handed back everything it needs to render step 2 —
// the detected format, the columns, a preview of the rows and our guess at the
// mapping. Nothing is written to the inbox yet.
//
// Multipart form: `file` plus optional `bankAccountId`, `format`, `mapping`
// (JSON, for a re-upload after the user changed the mapping).
func (h *BankStatementHandler) ParseStatement(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	data, filename, err := uploadData(c)
	if err != nil {
		return err
	}
	bankAccountID, err := parseOptionalUUID(c.FormValue("bankAccountId"), "bankAccountId")
	if err != nil {
		return err
	}
	if bankAccountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "bankAccountId is required")
	}

	// Sniff the contents when the wizard has not pinned a format: banks
	// mislabel their exports often enough that the extension cannot be trusted.
	var st *bankstatement.Statement
	if format := strings.ToUpper(strings.TrimSpace(c.FormValue("format"))); format != "" {
		st, err = bankstatement.ParseAs(format, filename, data)
	} else {
		st, err = bankstatement.Parse(filename, data)
	}
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// The account's currency is the fallback when the file does not say.
	if st.Currency == "" {
		st.Currency = h.accountCurrency(c, orgID, *bankAccountID)
	}

	lines := st.Lines
	var mapping bankstatement.Mapping
	if st.Format == bankstatement.FormatCSV {
		if raw := c.FormValue("mapping"); strings.TrimSpace(raw) != "" {
			if err := jsonUnmarshalString(raw, &mapping); err != nil {
				return fiber.NewError(fiber.StatusBadRequest, "mapping is not valid JSON")
			}
		} else {
			mapping = bankstatement.DetectMapping(st.Columns)
		}
		if lines, err = bankstatement.ApplyCSV(st, mapping); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}

	inputs, duplicates, err := h.prepareLines(c, orgID, *bankAccountID, lines)
	if err != nil {
		return err
	}

	imp := &models.BankStatementImport{
		BankAccountID:  *bankAccountID,
		Filename:       filename,
		Format:         st.Format,
		Status:         models.StatementImportStaged,
		LineCount:      len(inputs),
		DuplicateCount: duplicates,
		CurrencyCode:   st.Currency,
		StatementStart: st.Start,
		StatementEnd:   st.End,
		OpeningBalance: bankstatement.OpeningBalance(lines),
		ClosingBalance: st.Closing,
		Mapping:        mappingAsMap(mapping),
	}
	if err := h.repos.BankStatements.CreateImport(c.Context(), orgID, imp, mustJSON(inputs)); err != nil {
		return httpError(err)
	}

	// The wizard previews the parsed lines, not the raw grid: a user comparing
	// two files should see what we understood, not what the file said.
	return rawOne(c, fiber.StatusCreated, "StatementImports",
		importPreview(imp, st, mapping, inputs, duplicates))
}

const (
	// maxStatementSize caps an uploaded statement. Xero's own limit is 40MB;
	// 32MB is comfortably more than any real bank export.
	maxStatementSize   = 32 << 20
	importPreviewLimit = 200
)

// uploadData reads the statement file the wizard posts, under the size cap that
// keeps a hostile upload from being buffered whole into memory.
func uploadData(c fiber.Ctx) (data []byte, filename string, err error) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return nil, "", fiber.NewError(fiber.StatusBadRequest, "attach the statement file as `file`")
	}
	f, err := fileHeader.Open()
	if err != nil {
		return nil, "", fiber.NewError(fiber.StatusBadRequest, "could not read the uploaded file")
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, maxStatementSize))
	if err != nil {
		return nil, "", fiber.NewError(fiber.StatusBadRequest, "could not read the uploaded file")
	}
	return data, fileHeader.Filename, nil
}

// importPreview is the step-2 payload the wizard renders. The upload and the
// remap both return it, so correcting the mapping cannot change the shape of the
// table the user is looking at.
func importPreview(imp *models.BankStatementImport, st *bankstatement.Statement, mapping bankstatement.Mapping, inputs []repository.StatementLineInput, duplicates int) fiber.Map {
	shown := inputs[:min(len(inputs), importPreviewLimit)]
	preview := make([]map[string]any, 0, len(shown))
	for _, in := range shown {
		preview = append(preview, map[string]any{
			"Date":         in.PostedAt.Format("2006-01-02"),
			"Amount":       in.Amount,
			"Payee":        in.Payee,
			"Description":  in.Description,
			"Reference":    in.Reference,
			"ChequeNumber": in.ChequeNumber,
			"Balance":      in.Balance,
			"Duplicate":    in.Duplicate,
		})
	}
	return fiber.Map{
		"Import":     imp,
		"Columns":    st.Columns,
		"HasHeader":  st.HasHeader,
		"Delimiter":  st.Delimiter,
		"Mapping":    mapping,
		"Detected":   bankstatement.DetectedColumns(st),
		"Preview":    preview,
		"Duplicates": duplicates,
	}
}

// RemapImport re-runs the mapping on a staged CSV import — step 2 of the
// wizard, when the user corrects the columns we guessed. The file itself is
// still on the client, so it is re-uploaded; only the staged payload changes.
func (h *BankStatementHandler) RemapImport(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	imp, err := h.repos.BankStatements.GetImport(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if imp.Status != models.StatementImportStaged {
		return fiber.NewError(fiber.StatusConflict, "this import has already been committed")
	}

	data, filename, err := uploadData(c)
	if err != nil {
		return err
	}

	var mapping bankstatement.Mapping
	if err := jsonUnmarshalString(c.FormValue("mapping"), &mapping); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "mapping is not valid JSON")
	}
	st, err := bankstatement.ParseAs(firstNonBlank(imp.Format, bankstatement.FormatCSV), filename, data)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if st.Format != bankstatement.FormatCSV {
		return fiber.NewError(fiber.StatusBadRequest, "only CSV imports need a column mapping")
	}
	lines, err := bankstatement.ApplyCSV(st, mapping)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	inputs, duplicates, err := h.prepareLines(c, orgID, imp.BankAccountID, lines)
	if err != nil {
		return err
	}
	imp.Mapping = mappingAsMap(mapping)
	imp.LineCount = len(inputs)
	imp.DuplicateCount = duplicates
	imp.StatementStart, imp.StatementEnd = st.Start, st.End
	imp.OpeningBalance = bankstatement.OpeningBalance(lines)
	imp.ClosingBalance = st.Closing
	if err := h.repos.BankStatements.UpdateImport(c.Context(), orgID, imp, mustJSON(inputs)); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "StatementImports",
		importPreview(imp, st, mapping, inputs, duplicates))
}

// CommitImport is step 3: the user has seen the preview and pressed Import.
// Rows flagged as duplicates are skipped unless `includeDuplicates` is set —
// Xero warns about them and lets the user proceed anyway.
func (h *BankStatementHandler) CommitImport(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		IncludeDuplicates bool `json:"IncludeDuplicates"`
	}](c)
	if err != nil {
		return err
	}
	payload, err := h.repos.BankStatements.Payload(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	var inputs []repository.StatementLineInput
	if err := jsonUnmarshalBytes(payload, &inputs); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "staged import is unreadable")
	}
	// The duplicate flag is cleared here rather than the rows being dropped: the
	// repository counts what it skips, and "imported 0 of 2" is what the wizard
	// has to show the user.
	if body.IncludeDuplicates {
		for i := range inputs {
			inputs[i].Duplicate = false
		}
	}
	imported, skipped, err := h.repos.BankStatements.CommitImport(c.Context(), orgID, id, inputs)
	if err != nil {
		return httpError(err)
	}
	imp, err := h.repos.BankStatements.GetImport(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{
		"Import":   imp,
		"Imported": imported,
		"Skipped":  skipped,
	})
}

func (h *BankStatementHandler) ListImports(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	accountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	p := paginationFromQuery(c)
	imports, total, err := h.repos.BankStatements.ListImports(c.Context(), orgID, accountID, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"StatementImports": imports, "Pagination": p})
}

func (h *BankStatementHandler) GetImport(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	imp, err := h.repos.BankStatements.GetImport(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	payload, err := h.repos.BankStatements.Payload(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	lines := []repository.StatementLineInput{}
	if err := jsonUnmarshalBytes(payload, &lines); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "StatementImports", fiber.Map{
		"Import": imp, "Lines": lines,
	})
}

// UndoImport removes a manual import and the statement lines it created. An
// import whose lines have already been reconciled into transactions is refused:
// undoing it would silently orphan posted ledger entries.
func (h *BankStatementHandler) UndoImport(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	deleted, err := h.repos.BankStatements.DeleteImport(c.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, repository.ErrForbidden) {
			return fiber.NewError(fiber.StatusConflict,
				"some lines from this import have already been reconciled; unreconcile them first")
		}
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Deleted": deleted})
}

// ---------------------------------------------------------------------------
// Reconcile periods
// ---------------------------------------------------------------------------

func (h *BankStatementHandler) ListPeriods(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	accountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	periods, err := h.repos.BankStatements.ListPeriods(c.Context(), orgID, accountID)
	if err != nil {
		return httpError(err)
	}
	if periods == nil {
		periods = []models.BankReconcilePeriod{}
	}
	return envelopeList(c, "ReconcilePeriods", periods)
}

func (h *BankStatementHandler) CreatePeriod(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	body, err := bindBody[struct {
		BankAccountID    string           `json:"BankAccountID"`
		StartDate        string           `json:"StartDate"`
		EndDate          string           `json:"EndDate"`
		StatementBalance *decimal.Decimal `json:"StatementBalance"`
	}](c)
	if err != nil {
		return err
	}
	accountID, err := parseOptionalUUID(body.BankAccountID, "BankAccountID")
	if err != nil {
		return err
	}
	if accountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "BankAccountID is required")
	}
	start, err := parseYMD(body.StartDate)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "StartDate must be YYYY-MM-DD")
	}
	end, err := parseYMD(body.EndDate)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "EndDate must be YYYY-MM-DD")
	}
	if end.Before(start) {
		return fiber.NewError(fiber.StatusBadRequest, "EndDate must not be before StartDate")
	}
	period := &models.BankReconcilePeriod{
		BankAccountID: *accountID,
		StartDate:     start,
		EndDate:       end,
	}
	if body.StatementBalance != nil {
		period.StatementBalance = *body.StatementBalance
	} else if bal, err := h.repos.BankStatements.AccountBalance(c.Context(), orgID, *accountID); err == nil {
		period.StatementBalance = bal.StatementBalance
	}
	userID := middleware.UserIDFrom(c)
	if err := h.repos.BankStatements.CreatePeriod(c.Context(), orgID, period, &userID); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			return fiber.NewError(fiber.StatusConflict, "a reconcile period already covers those dates")
		}
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "ReconcilePeriods", *period)
}

func (h *BankStatementHandler) DeletePeriod(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.BankStatements.DeletePeriod(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// prepareLines converts parsed file lines into rows ready to stage, marking the
// ones that look like a duplicate of something already in the account. The
// duplicate check is a fingerprint over account+date+amount+payee+reference,
// computed against the account's existing lines in one query.
func (h *BankStatementHandler) prepareLines(c fiber.Ctx, orgID, bankAccountID uuid.UUID, lines []bankstatement.Line) ([]repository.StatementLineInput, int, error) {
	if len(lines) == 0 {
		return nil, 0, fiber.NewError(fiber.StatusBadRequest, "the file contains no transactions")
	}
	from, to := lines[0].Date, lines[0].Date
	for _, l := range lines {
		if l.Date.Before(from) {
			from = l.Date
		}
		if l.Date.After(to) {
			to = l.Date
		}
	}
	existing, err := h.repos.BankStatements.ExistingFingerprints(c.Context(), orgID, bankAccountID, from, to)
	if err != nil {
		return nil, 0, httpError(err)
	}
	currency := h.accountCurrency(c, orgID, bankAccountID)

	// A file can legitimately contain the same line twice (two identical
	// coffees), so only a collision with what is *already stored* is a
	// duplicate; the second copy within the file is kept and flagged too, since
	// re-importing the same file must not double up.
	seen := map[string]bool{}
	inputs := make([]repository.StatementLineInput, 0, len(lines))
	duplicates := 0
	for _, l := range lines {
		fp := repository.StatementFingerprint(l.Date, l.Amount, l.Payee, l.Reference)
		dup := existing[fp] || seen[fp]
		if dup {
			duplicates++
		}
		seen[fp] = true
		inputs = append(inputs, repository.StatementLineInput{
			PostedAt:     l.Date,
			Amount:       l.Amount,
			Balance:      l.Balance,
			CurrencyCode: currency,
			Payee:        l.Payee,
			Description:  l.Description,
			Reference:    l.Reference,
			ChequeNumber: l.ChequeNumber,
			ProviderTxID: l.ProviderTxID,
			Fingerprint:  fp,
			Duplicate:    dup,
		})
	}
	return inputs, duplicates, nil
}

// withClaimedLine runs fn against a statement line taken out of the inbox, then
// commits the line to the transaction fn produces — a nil transaction for a
// transfer, which posts both legs itself. The line is released back to the inbox
// only when fn fails: once a transaction exists, putting the line back would let
// a retry post it a second time.
func (h *BankStatementHandler) withClaimedLine(ctx context.Context, orgID, lineID uuid.UUID, fn func() (*uuid.UUID, error)) error {
	if err := h.repos.BankStatements.ClaimLine(ctx, orgID, lineID); err != nil {
		return err
	}
	txID, err := fn()
	if err != nil {
		_ = h.repos.BankStatements.ReleaseLine(ctx, orgID, lineID)
		return err
	}
	return h.repos.BankStatements.AttachLineTransaction(ctx, orgID, lineID, txID)
}

// lostClaimError explains a refused claim in the user's terms: the line left the
// inbox because it has already been reconciled.
func lostClaimError(err error) error {
	if errors.Is(err, repository.ErrAlreadyExists) {
		return fiber.NewError(fiber.StatusConflict, "statement line is already reconciled")
	}
	return httpError(err)
}

// sameDirection reports whether a transaction moves money the same way as the
// statement line. Bank transaction totals are unsigned here, so the direction
// comes from the transaction type: matching a deposit to a withdrawal of the
// same size would reconcile the wrong thing.
func sameDirection(bt models.BankTransaction, line *models.BankStatementLine) bool {
	spend := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(bt.Type)), "SPEND")
	return spend == line.Amount.IsNegative()
}

func (h *BankStatementHandler) accountCurrency(c fiber.Ctx, orgID, accountID uuid.UUID) string {
	acc, err := h.repos.Accounts.GetByID(c.Context(), orgID, accountID)
	if err != nil || acc == nil {
		return ""
	}
	return acc.CurrencyCode
}

func optionalQueryUUID(c fiber.Ctx, key string) (*uuid.UUID, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid "+key)
	}
	return &id, nil
}

func optionalQueryDate(c fiber.Ctx, key string) (*time.Time, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	d, err := parseYMD(raw)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid "+key+" (expected YYYY-MM-DD)")
	}
	return &d, nil
}

func boolPtr(v bool) *bool { return &v }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}
