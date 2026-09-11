package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/bankcoding"
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
//	search         — free text over payee, description, reference,
//	                 counterparty, cheque number and amount
//	fromDate/toDate— inclusive date bounds
//	unreconciled   — bool, default true: only lines still awaiting a decision
//	suggestions    — bool: also evaluate bank rules and return suggestions
//	previousEntries— bool: also suggest the coding this account used last time
//	                 it saw the same payee (Xero's "Suggest previous entries")
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
	if f.MinAmount, err = optionalQueryDecimal(c, "minAmount"); err != nil {
		return err
	}
	if f.MaxAmount, err = optionalQueryDecimal(c, "maxAmount"); err != nil {
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
	if boolParam(c, "previousEntries", false) {
		if err := h.attachPreviousEntries(c, orgID, items); err != nil {
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
	// A single line is what the Create panel is about to show, so both kinds of
	// suggestion are filled in here whether or not the caller asked.
	lines := []models.BankStatementLine{*line}
	if err := h.attachSuggestions(c, orgID, lines); err != nil {
		return err
	}
	if err := h.attachPreviousEntries(c, orgID, lines); err != nil {
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

// BalanceSeries is the line behind the "Balance graph" on the bank accounts
// page. The card draws one graph per account, so it is the same question as
// Balance asked about every day of the window instead of only today: the
// ledger balance carried forward, one point per calendar day, ending today.
//
// The last point and Balance's LedgerBalance are the same number by
// construction, which is the invariant the card leans on — the graph's right
// edge is the figure printed above it.
func (h *BankStatementHandler) BalanceSeries(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	accountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	if accountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "bankAccountId is required")
	}
	days, err := optionalQueryInt(c, "days", repository.BalanceSeriesDays)
	if err != nil {
		return err
	}
	series, err := h.repos.BankStatements.BalanceSeries(c.Context(), orgID, *accountID, days)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"BalanceSeries": series})
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
		if s := bankrules.Suggest(rules, candidateFor(lines[i])); s != nil {
			setRuleSuggestion(&lines[i], s)
		}
	}
	return nil
}

// attachPreviousEntries fills in the "suggest previous entries" coding: what
// this bank account did the last time a line with the same payee came through.
//
// The history is read once per bank account rather than once per line, because a
// page of lines is normally for one account and a payee tends to repeat across
// several lines on the same page.
func (h *BankStatementHandler) attachPreviousEntries(c fiber.Ctx, orgID uuid.UUID, lines []models.BankStatementLine) error {
	if len(lines) == 0 {
		return nil
	}
	histories := make(map[uuid.UUID]*bankcoding.History)
	for i := range lines {
		if lines[i].BankAccountID == nil {
			continue
		}
		accountID := *lines[i].BankAccountID
		hist, ok := histories[accountID]
		if !ok {
			entries, err := h.repos.BankStatements.CodingHistory(c.Context(), orgID, accountID, 0)
			if err != nil {
				return httpError(err)
			}
			hist = bankcoding.NewHistory(entries)
			histories[accountID] = hist
		}
		if s := hist.Suggest(lines[i].Payee, lines[i].Reference); s != nil {
			lines[i].PreviousEntry = s
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
		if s := bankrules.Suggest(rules, candidateFor(lines[i])); s != nil {
			setRuleSuggestion(&lines[i], s)
			lines[i].TaxType = s.TaxType
		}
	}
	return c.JSON(fiber.Map{"StatementLines": lines})
}

// setRuleSuggestion records what a matching bank rule proposes. The suggestion
// is advisory: it opens the Create panel pre-filled and the user can override
// everything before saving.
func setRuleSuggestion(line *models.BankStatementLine, s *models.BankRuleSuggestion) {
	line.Suggestions = []models.BankRuleSuggestion{*s}
	if id := uuidPtr(s.AccountID); id != nil {
		line.AccountID = id
	}
}

// resolveLines loads the lines named by IDs, or the unreconciled inbox of an
// account when no IDs are given.
func (h *BankStatementHandler) resolveLines(c fiber.Ctx, orgID uuid.UUID, bankAccountID *uuid.UUID, ids []uuid.UUID) ([]models.BankStatementLine, error) {
	if len(ids) > 0 {
		lines, err := h.repos.BankStatements.GetLines(c.Context(), orgID, ids)
		if err != nil {
			return nil, httpError(err)
		}
		return lines, nil
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
	// Unreconciled posts the transaction without tying it to the line. The
	// Match tab's "New Transaction" uses it: there the new transaction is
	// something to select alongside the others, not the answer on its own, and
	// reconciling it here would answer the question before it was asked.
	Unreconciled bool
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
		// Reconcile defaults to true — Create turns the line into a
		// transaction and is done. Passing false posts the transaction
		// unreconciled and leaves the line in the inbox, which is what the
		// Match tab needs when it adds a transaction to a selection.
		Reconcile *bool `json:"Reconcile"`
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
	reconcile := true
	if body.Reconcile != nil {
		reconcile = *body.Reconcile
	}
	if !reconcile {
		bt, cerr := h.createTransactionFromLine(c, orgID, line, lineRequest{
			BankAccountID: bankAccountID,
			AccountCode:   body.AccountCode,
			TaxType:       body.TaxType,
			ContactID:     body.ContactID,
			Reference:     body.Reference,
			Description:   body.Description,
			TaxAmount:     body.TaxAmount,
			Unreconciled:  true,
		})
		if cerr != nil {
			return cerr
		}
		return rawOne(c, fiber.StatusCreated, "BankTransactions", *bt)
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
		IsReconciled:    !req.Unreconciled,
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
	cid, err := parseOptionalUUID(strings.TrimSpace(req.ContactID), "ContactID")
	if err != nil {
		return nil, err
	}
	bt.ContactID = cid
	if err := h.repos.BankTransactions.Create(c.Context(), orgID, bt); err != nil {
		return nil, httpError(err)
	}
	return bt, nil
}

// MatchCandidates lists the transactions a statement line could be matched to —
// the "Find & match" search. Matching in Xero is on date and amount; the search
// is deliberately generous (same amount within a window) because the user is
// looking for something they know exists.
//
// The three filters on the form are query parameters, and each narrows the
// same list rather than being applied after it:
//
//	search    — free text over the contact name and the reference
//	showSpent — offer the opposite direction as well as the line's own
//	currency  — "Show USD items only": one currency code
//	amount    — "Search by amount": the transaction's total
//
// Direction is a filter rather than a sort because it is not a preference: a
// transaction that moved the other way cannot be the same money as this line,
// so it is out of the list until the user asks for the other side.
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
	f := repository.BankTransactionFilter{
		BankAccountID: line.BankAccountID,
		IsReconciled:  boolPtr(false),
		Search:        search,
		Currency:      strings.TrimSpace(c.Query("currency")),
		// A transaction the user has deleted is not money that can be matched
		// against a statement line, so it is left out of the table entirely
		// rather than counted in "Showing X - Y of Z". DELETE only sets
		// status='DELETED', so nothing else marks the row gone.
		ExcludeStatuses: []string{"DELETED"},
	}
	// "Show Spent Items" unchecked — Xero's default — means only transactions
	// moving the way the line moved.
	if !boolParam(c, "showSpent", false) {
		f.TypePrefix = models.BankTransactionTypeReceive
		if line.Amount.IsNegative() {
			f.TypePrefix = models.BankTransactionTypeSpend
		}
	}
	if raw := strings.TrimSpace(c.Query("amount")); raw != "" {
		amount, aerr := decimal.NewFromString(raw)
		if aerr != nil {
			return fiber.NewError(fiber.StatusBadRequest, "amount must be a number")
		}
		f.Total = &amount
	}
	// Only unreconciled transactions are offered. A transaction that has
	// already been reconciled — whether from this screen or by hand — is not
	// available to be reconciled a second time, and offering it would invite
	// two statement lines to claim the same piece of ledger.
	candidates, total, err := h.repos.BankTransactions.List(c.Context(), orgID, f, p)
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
	// Matching asserts that the line and the transaction are the same money.
	// Two things follow, and both are checked here rather than left to the
	// reconciler's arithmetic: the transaction must be unreconciled, and it
	// must agree with the line on amount and direction. AutoReconcile has
	// always held itself to this; a hand-made match must not be looser.
	if existing.IsReconciled {
		return fiber.NewError(fiber.StatusConflict, "that transaction is already reconciled")
	}
	if !sameDirection(*existing, line) || !existing.Total.Equal(line.Amount.Abs()) {
		return fiber.NewError(fiber.StatusBadRequest, "the amounts do not match")
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

// Adjustments a statement line can carry. Xero's "Adjustments" menu under the
// Match tab offers exactly these two, and they differ only in the description
// they write, because that is the whole of the difference: a bank fee and a
// minor adjustment are both a small amount posted to the statement line's own
// account so that a selection can be made to add up.
const (
	AdjustmentBankFee = "BANK_FEE"
	AdjustmentMinor   = "MINOR_ADJUSTMENT"
)

// Adjustment posts the small extra transaction that lets a selection balance.
//
// Xero's Match tab lets a statement line be reconciled against several
// transactions whose total is the line, and the difference between what the
// user found and what the bank took is exactly what "Bank fee" and "Minor
// adjustment" are for. The transaction this posts is deliberately *not*
// reconciled and not attached to the line: it joins the selection like any
// other candidate, and the selection as a whole is committed by
// ReconcileSelection.
//
// The amount is the caller's — the panel computes the outstanding difference —
// and must move the same way as the line. Which account it lands in is the
// caller's too; when they do not say, it is read from the tenant's own books —
// the account this organisation last coded this payee to on this bank account —
// and only failing that from the first expense account the chart has. See
// defaultAdjustmentAccount.
func (h *BankStatementHandler) Adjustment(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		Kind        string           `json:"Kind"`
		Amount      *decimal.Decimal `json:"Amount"`
		AccountCode string           `json:"AccountCode"`
	}](c)
	if err != nil {
		return err
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	var description string
	switch strings.ToUpper(strings.TrimSpace(body.Kind)) {
	case AdjustmentBankFee:
		description = "Bank fee"
	case AdjustmentMinor:
		description = "Minor adjustment"
	default:
		return fiber.NewError(fiber.StatusBadRequest, `Kind must be "BANK_FEE" or "MINOR_ADJUSTMENT"`)
	}
	amount := decimal.Zero
	if body.Amount != nil {
		amount = *body.Amount
	}
	if amount.IsZero() {
		return fiber.NewError(fiber.StatusBadRequest, "an adjustment of zero would change nothing")
	}
	if amount.IsNegative() != line.Amount.IsNegative() {
		return fiber.NewError(fiber.StatusBadRequest, "an adjustment must move the same way as the statement line")
	}
	if line.BankAccountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "the statement line has no bank account to post to")
	}
	code := strings.TrimSpace(body.AccountCode)
	if code == "" {
		code, err = h.defaultAdjustmentAccount(c, orgID, line, description)
		if err != nil {
			return err
		}
	}
	txType := models.BankTransactionTypeReceive
	if amount.IsNegative() {
		txType = models.BankTransactionTypeSpend
	}
	posted := line.PostedAt
	bt := &models.BankTransaction{
		Type:            txType,
		BankAccountID:   line.BankAccountID,
		IsReconciled:    false,
		Date:            &posted,
		Reference:       line.Reference,
		CurrencyCode:    line.CurrencyCode,
		Status:          "AUTHORISED",
		LineAmountTypes: models.LineAmountTypesInclusive,
		LineItems: []models.LineItem{{
			Description: description,
			Quantity:    decimal.NewFromInt(1),
			UnitAmount:  amount.Abs(),
			AccountCode: code,
		}},
	}
	if err := h.repos.BankTransactions.Create(c.Context(), orgID, bt); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankTransactions", *bt)
}

// defaultAdjustmentAccount picks where an adjustment lands when the caller did
// not choose, and the organisation's own books are what answer.
//
// Which account an adjustment belongs on is not a fact about the chart: one
// organisation books a bank fee to Bank Fees, another to General Expenses, and
// Xero lets either be any code it likes. Xero's Account resource holds no system
// role for one either -- the SystemAccount enum in xero_accounting.yaml ends at
// the wage, CIS and rounding accounts and has no bank-fee value -- so nothing in
// Xero's own data model names such an account. Nothing here names one either.
//
// What does answer it is this organisation's coding history on this bank account
// (repository.CodingHistory), read in two passes:
//
//   - the last time it coded this payee, which is the suggestion the coding panel
//     offers and covers the ordinary case of a remembered counterparty;
//   - failing that, the last time it coded an entry under this adjustment's own
//     description, which is what a bank fee has instead of a counterparty.
//
// A payee the books have never carried and a description they have never used
// leave nothing to answer with, and the caller is told so rather than handed an
// account chosen by its position in the chart -- the selection list does not show
// an account before the line is committed, so a posting that nobody chose would
// be a posting nobody sees. Naming one is always available: the person at the
// reconcile screen has already picked an account for the line, and the adjustment
// menu passes the coding panel's account straight through.
func (h *BankStatementHandler) defaultAdjustmentAccount(c fiber.Ctx, orgID uuid.UUID, line *models.BankStatementLine, description string) (string, error) {
	if line.BankAccountID == nil {
		return "", fiber.NewError(fiber.StatusBadRequest, "the statement line has no bank account to post to")
	}
	entries, err := h.repos.BankStatements.CodingHistory(c.Context(), orgID, *line.BankAccountID, 0)
	if err != nil {
		return "", httpError(err)
	}
	history := bankcoding.NewHistory(entries)
	if s := history.Suggest(line.Payee, line.Reference); s != nil && s.AccountCode != "" {
		return s.AccountCode, nil
	}
	if e := history.SuggestForDescription(description); e != nil {
		return e.AccountCode, nil
	}
	return "", fiber.NewError(fiber.StatusBadRequest,
		"AccountCode is required: these books have never coded that payee, or a \""+description+
			"\", on this bank account, so there is nothing to follow. Choose an account for the line and add the adjustment again.")
}

// ReconcileSelection commits a statement line against the transactions the user
// ticked on the Match tab.
//
// Xero lets a line be reconciled against several transactions at once — three
// invoices that add up to one deposit is the ordinary case — and this is that
// commit. It is deliberately stricter than the sum being close: the signed sum
// of the selection must equal the line exactly, because that is the assertion
// reconciliation makes. Every transaction in the selection is marked
// reconciled; the line records the first one, which is as much as the schema
// holds (bank_statement_lines carries a single bank_transaction_id).
//
// The existing Match endpoint is kept for the one-transaction case, so nothing
// that already calls it changes behaviour.
func (h *BankStatementHandler) ReconcileSelection(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		BankTransactionIDs []uuid.UUID `json:"BankTransactionIDs"`
	}](c)
	if err != nil {
		return err
	}
	if len(body.BankTransactionIDs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "select at least one transaction")
	}
	line, err := h.repos.BankStatements.GetLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	seen := make(map[uuid.UUID]bool, len(body.BankTransactionIDs))
	sum := decimal.Zero
	// The transactions are kept from the validation pass so the response does not
	// re-read every one of them after reconciling.
	selected := make([]models.BankTransaction, 0, len(body.BankTransactionIDs))
	for _, txID := range body.BankTransactionIDs {
		if txID == uuid.Nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid BankTransactionIDs")
		}
		if seen[txID] {
			return fiber.NewError(fiber.StatusBadRequest, "the same transaction cannot be counted twice")
		}
		seen[txID] = true
		tx, err := h.repos.BankTransactions.GetByID(c.Context(), orgID, txID)
		if err != nil {
			return httpError(err)
		}
		// Reconciling against another account's transaction would mark the
		// wrong balance correct. A feed line with no ledger account is exempt.
		if line.BankAccountID != nil && tx.BankAccountID != nil && *tx.BankAccountID != *line.BankAccountID {
			return fiber.NewError(fiber.StatusBadRequest, "the transaction belongs to a different bank account")
		}
		if tx.IsReconciled {
			return fiber.NewError(fiber.StatusConflict, "one of those transactions is already reconciled")
		}
		// Money in counts positive and money out negative, so a selection of
		// one side only adds up the way the statement line already reads.
		amount := tx.Total
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(tx.Type)), "RECEIVE") {
			amount = amount.Neg()
		}
		sum = sum.Add(amount)
		selected = append(selected, *tx)
	}
	if !sum.Equal(line.Amount) {
		return fiber.NewError(fiber.StatusBadRequest,
			"the selected transactions do not add up to the statement line")
	}
	primary := body.BankTransactionIDs[0]
	if err := h.withClaimedLine(c.Context(), orgID, id, func() (*uuid.UUID, error) {
		for i := range selected {
			if err := h.repos.BankTransactions.Reconcile(c.Context(), orgID, selected[i].BankTransactionID, true); err != nil {
				return nil, httpError(err)
			}
			selected[i].IsReconciled = true
		}
		return &primary, nil
	}); err != nil {
		return lostClaimError(err)
	}
	return c.JSON(fiber.Map{"BankTransactions": selected})
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
	matched, scanned, err := h.autoReconcileAccount(c.Context(), orgID, *accountID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"Matched":   matched,
		"Scanned":   scanned,
		"Remaining": scanned - matched,
	})
}

// autoReconcileAccount is the body of "Ok, let's reconcile" without the HTTP
// around it, so the two places that need it can share it: the button, and an
// account whose auto-reconcile setting is on, which runs it after an import.
// Every match it makes is stamped as its own, which is what the banner reports.
func (h *BankStatementHandler) autoReconcileAccount(ctx context.Context, orgID, accountID uuid.UUID) (matched, scanned int, err error) {
	lines, err := h.repos.BankStatements.UnreconciledLinesForAccount(ctx, orgID, accountID)
	if err != nil {
		return 0, 0, httpError(err)
	}
	candidates, _, err := h.repos.BankTransactions.List(ctx, orgID, repository.BankTransactionFilter{
		BankAccountID: &accountID,
		IsReconciled:  boolPtr(false),
	}, models.Pagination{Page: 1, PageSize: 500})
	if err != nil {
		return 0, 0, httpError(err)
	}

	used := map[uuid.UUID]bool{}
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
				return matched, len(lines), httpError(err)
			}
			used[bt.BankTransactionID] = true
			if err := h.repos.BankStatements.MarkAutoReconciled(ctx, orgID,
				[]uuid.UUID{line.StatementLineID}); err != nil {
				return matched, len(lines), httpError(err)
			}
			matched++
			break
		}
	}
	return matched, len(lines), nil
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
	// Read one byte past the cap so an oversized file is rejected rather than
	// silently truncated and parsed as if it were complete.
	data, err = io.ReadAll(io.LimitReader(f, maxStatementSize+1))
	if err != nil {
		return nil, "", fiber.NewError(fiber.StatusBadRequest, "could not read the uploaded file")
	}
	if int64(len(data)) > maxStatementSize {
		return nil, "", fiber.NewError(fiber.StatusRequestEntityTooLarge, "statement file exceeds 32 MiB limit")
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
	// With auto-reconcile on for this account, the import finishes the job it
	// can: the lines that agree exactly with a transaction on the account are
	// reconciled as they land. The user asked for this once, on the account,
	// rather than per file.
	matched := 0
	if acc, aerr := h.repos.Accounts.GetByID(c.Context(), orgID, imp.BankAccountID); aerr == nil && acc != nil && acc.AutoReconcile {
		matched, _, err = h.autoReconcileAccount(c.Context(), orgID, imp.BankAccountID)
		if err != nil {
			return err
		}
	}
	return c.JSON(fiber.Map{
		"Import":      imp,
		"Imported":    imported,
		"Skipped":     skipped,
		"AutoMatched": matched,
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

// optionalQueryInt reads a bounded count off the query string. A value that is
// not a number is refused rather than ignored, for the same reason an
// unparseable date is: falling back to the default would answer a different
// question than the one that was asked.
func optionalQueryInt(c fiber.Ctx, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fiber.NewError(fiber.StatusBadRequest, "invalid "+key)
	}
	return n, nil
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

// optionalQueryDecimal reads Xero's amount-range filter. An unparseable number
// is refused rather than ignored: silently dropping it would show the user
// unfiltered lines while the Filter badge claimed a filter was on.
func optionalQueryDecimal(c fiber.Ctx, key string) (*decimal.Decimal, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid "+key+" (expected a number)")
	}
	return &d, nil
}

func boolPtr(v bool) *bool { return &v }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// ---------------------------------------------------------------------------
// Discuss — notes on a statement line
// ---------------------------------------------------------------------------

// ListComments returns a line's discussion thread.
func (h *BankStatementHandler) ListComments(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	list, err := h.repos.BankStatements.Comments(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if list == nil {
		list = []models.BankStatementLineComment{}
	}
	return c.JSON(fiber.Map{"Comments": list})
}

// CreateComment posts one note to a line's discussion.
func (h *BankStatementHandler) CreateComment(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		Body string `json:"Body"`
	}](c)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(body.Body)
	if text == "" {
		return fiber.NewError(fiber.StatusBadRequest, "write something before posting")
	}
	uid := middleware.UserIDFrom(c)
	userID := &uid
	// The name is copied onto the note rather than joined at read time, so a
	// discussion stays readable after the author leaves the organisation.
	name := ""
	if u, err := h.repos.Users.GetByID(c.Context(), uid); err == nil && u != nil {
		name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = u.Email
		}
	}
	comment, err := h.repos.BankStatements.AddComment(c.Context(), orgID, id, userID, name, text)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Comments", *comment)
}

// ---------------------------------------------------------------------------
// Options — delete a line, and the auto-reconcile banner
// ---------------------------------------------------------------------------

// DeleteStatementLine removes a line the user does not want in the inbox. A
// reconciled line is refused: the ledger entry it explains would be left
// orphaned.
func (h *BankStatementHandler) DeleteStatementLine(c fiber.Ctx) error {
	orgID, id, err := tenantAndID(c)
	if err != nil {
		return err
	}
	if err := h.repos.BankStatements.DeleteLine(c.Context(), orgID, id); err != nil {
		if errors.Is(err, repository.ErrForbidden) {
			return fiber.NewError(fiber.StatusConflict,
				"this line has already been reconciled; unreconcile it first")
		}
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Deleted": 1})
}

// AutoReconcileStatus is the number over the inbox — Xero's "24 of 73
// statement lines imported in the last 30 days were auto-reconciled" — together
// with the setting the banner is where you turn on.
func (h *BankStatementHandler) AutoReconcileStatus(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	accountID, err := optionalQueryUUID(c, "bankAccountId")
	if err != nil {
		return err
	}
	if accountID == nil {
		return fiber.NewError(fiber.StatusBadRequest, "bankAccountId is required")
	}
	days := 30
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 3650 {
			return fiber.NewError(fiber.StatusBadRequest, "days must be between 1 and 3650")
		}
		days = n
	}
	rep, err := h.repos.BankStatements.AutoReconcileReport(c.Context(), orgID, *accountID, days)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"AutoReconcile": rep})
}

// SetAutoReconcile turns automatic reconciliation on or off for one account.
// With it on, an import reconciles what it can without being asked, which is
// what Xero's setting does.
func (h *BankStatementHandler) SetAutoReconcile(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	body, err := bindBody[struct {
		BankAccountID string `json:"BankAccountID"`
		Enabled       bool   `json:"Enabled"`
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
	if err := h.repos.Accounts.SetAutoReconcile(c.Context(), orgID, *accountID, body.Enabled); err != nil {
		return httpError(err)
	}
	if body.Enabled {
		h.autoReconcileAccount(c, orgID, *accountID)
	}
	rep, err := h.repos.BankStatements.AutoReconcileReport(c.Context(), orgID, *accountID, 30)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"AutoReconcile": rep})
}
