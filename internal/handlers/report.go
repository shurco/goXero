package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// ReportHandler exposes the core financial reports documented at
// https://developer.xero.com/documentation/api/accounting/reports.
//
// Every endpoint now emits the canonical Xero "Reports" envelope:
//
//	{ "Id": …, "Status": "OK", "ProviderName": "goxero",
//	  "DateTimeUTC": …, "Reports": [{ReportID, ReportName, …, Rows:[…]}] }
//
// Supported query parameters (subset of Xero's, shared across endpoints):
//
//	date, fromDate, toDate  — ISO YYYY-MM-DD. `date` is a report's as-at date;
//	                          where an endpoint also takes `toDate` as the end of
//	                          a period, `date` wins when both are sent.
//	compare                 — bool: add a comparative column (P&L and Balance
//	                          Sheet only). Defaults to the same window one year
//	                          earlier, or to the organisation's financial year
//	                          start for the Balance Sheet — which is exactly
//	                          what IRS Schedule L needs.
//	compareDate             — ISO YYYY-MM-DD: explicit comparative date (BS)
//	compareFromDate/ToDate  — ISO YYYY-MM-DD: explicit comparative window (P&L)
//	accountID               — repeatable UUID: restrict Account Transactions /
//	                          General Ledger Detail to specific accounts
//	contactID               — UUID: restrict an aged report to one contact
//
// Not yet implemented (Xero accepts them; we ignore them rather than pretend):
// `periods`/`timeframe` multi-period columns, `trackingCategoryID` splits,
// `standardLayout` and `paymentsOnly`.
type ReportHandler struct {
	repos *repository.Repositories
}

func NewReportHandler(r *repository.Repositories) *ReportHandler {
	return &ReportHandler{repos: r}
}

func dateParam(c fiber.Ctx, key string, fallback time.Time) (time.Time, error) {
	v := c.Query(key)
	if v == "" {
		return fallback, nil
	}
	d, err := parseYMD(v)
	if err != nil {
		return time.Time{}, fiber.NewError(fiber.StatusBadRequest, "invalid "+key+" (expected YYYY-MM-DD)")
	}
	return d, nil
}

// asAtParam resolves the day a report is drawn at. `date` is Xero's own parameter
// for exactly this and wins whenever the caller sends it; `toDate` is the end of
// the period they asked for and is the fallback, so a caller who names only a
// period gets a report dated at the end of it instead of one silently dated
// today. With neither, the as-at date is `fallback` — today, for every report
// below. Both spellings parse alike, so a malformed one is reported by name.
//
// Only the reports that were ignoring part of their query string read the date
// this way; the reports that already read `toDate` (P&L, Balance Sheet, Cash
// Summary, …) keep `dateParam`, so nothing about them moves.
func asAtParam(c fiber.Ctx, fallback time.Time) (time.Time, error) {
	for _, key := range []string{"date", "toDate"} {
		if c.Query(key) != "" {
			return dateParam(c, key, time.Time{})
		}
	}
	return fallback, nil
}

// optionalDateParam reads a YYYY-MM-DD query parameter when the caller sent one
// and reports nil when they did not, so a caller can tell "no window asked for"
// from "the zero date asked for". A report that renders no rows still has to name
// the window it was read at, and which window is Xero's default is a question
// only the absence of the parameter can answer.
func optionalDateParam(c fiber.Ctx, key string) (*time.Time, error) {
	if c.Query(key) == "" {
		return nil, nil
	}
	d, err := dateParam(c, key, time.Time{})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// orgName is a small helper — pulls the organisation Name so reports can
// render the standard "Trial Balance / Demo Company / As at …" title block.
func (h *ReportHandler) orgName(c fiber.Ctx, orgID uuid.UUID) string {
	org, err := h.repos.Organisations.GetByID(c.Context(), orgID)
	if err != nil || org == nil {
		return ""
	}
	return org.Name
}

func (h *ReportHandler) TrialBalance(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := asAtParam(c, time.Now().UTC())
	if err != nil {
		return err
	}
	// Period defaults to the month containing the as-at date, matching Xero: the
	// Debit/Credit columns cover that period while YTD covers the financial
	// year, which the repository derives from the organisation settings.
	from, err := dateParam(c, "fromDate", time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	rows, err := h.repos.Reports.TrialBalance(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderTrialBalance(h.orgName(c, orgID), from, to, rows)))
}

func (h *ReportHandler) ProfitAndLoss(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	from, err := dateParam(c, "fromDate", time.Date(to.Year(), 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	cmp, err := h.comparativePeriod(c, from, to)
	if err != nil {
		return err
	}
	report, err := h.repos.Reports.ProfitAndLoss(c.Context(), orgID, repository.Period{From: from, To: to}, cmp)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderProfitAndLoss(h.orgName(c, orgID), report)))
}

func (h *ReportHandler) BalanceSheet(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	asOf, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	cmp, err := h.comparativeDate(c, orgID, asOf)
	if err != nil {
		return err
	}
	bs, err := h.repos.Reports.BalanceSheet(c.Context(), orgID, asOf, cmp)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderBalanceSheet(h.orgName(c, orgID), bs)))
}

// boolParam reads a Xero-style boolean query parameter. Absent or unparseable
// input yields the fallback.
func boolParam(c fiber.Ctx, key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(c.Query(key)))
	switch v {
	case "":
		return fallback
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	}
	return fallback
}

// comparativePeriod resolves the comparator window for the Profit & Loss.
// `?compare=true` shifts the requested window back one year; explicit
// `compareFromDate`/`compareToDate` win over the shift.
func (h *ReportHandler) comparativePeriod(c fiber.Ctx, from, to time.Time) (*repository.Period, error) {
	explicitFrom := c.Query("compareFromDate") != ""
	explicitTo := c.Query("compareToDate") != ""
	if !boolParam(c, "compare", explicitFrom || explicitTo) && !explicitFrom && !explicitTo {
		return nil, nil
	}
	cmpFrom, err := dateParam(c, "compareFromDate", from.AddDate(-1, 0, 0))
	if err != nil {
		return nil, err
	}
	cmpTo, err := dateParam(c, "compareToDate", to.AddDate(-1, 0, 0))
	if err != nil {
		return nil, err
	}
	return &repository.Period{From: cmpFrom, To: cmpTo}, nil
}

// comparativeDate resolves the comparator date for the Balance Sheet.
// `?compare=true` uses the start of the financial year — the beginning-of-year
// column of IRS Schedule L.
func (h *ReportHandler) comparativeDate(c fiber.Ctx, orgID uuid.UUID, asOf time.Time) (*time.Time, error) {
	explicit := c.Query("compareDate") != ""
	if !boolParam(c, "compare", explicit) && !explicit {
		return nil, nil
	}
	fyStart, err := h.repos.Reports.FinancialYearStart(c.Context(), orgID, asOf)
	if err != nil {
		return nil, httpError(err)
	}
	d, err := dateParam(c, "compareDate", fyStart)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// accountIDsFromQuery collects the repeatable `?accountID=` parameter.
func accountIDsFromQuery(c fiber.Ctx) ([]uuid.UUID, error) {
	var out []uuid.UUID
	// Repeatable parameter (?accountID=a&accountID=b) plus comma-separated
	// values in a single occurrence are both accepted.
	raw := c.Request().URI().QueryArgs().PeekMulti("accountID")
	for _, rawVal := range raw {
		for _, part := range strings.Split(string(rawVal), ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := uuid.Parse(part)
			if err != nil {
				return nil, fiber.NewError(fiber.StatusBadRequest, "invalid accountID")
			}
			out = append(out, id)
		}
	}
	return out, nil
}

// AccountTransactions is the drill-down report behind every other report: the
// individual GL postings for the selected accounts over a period.
func (h *ReportHandler) AccountTransactions(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	fyStart, err := h.repos.Reports.FinancialYearStart(c.Context(), orgID, to)
	if err != nil {
		return httpError(err)
	}
	from, err := dateParam(c, "fromDate", fyStart)
	if err != nil {
		return err
	}
	accountIDs, err := accountIDsFromQuery(c)
	if err != nil {
		return err
	}
	lines, err := h.repos.Reports.AccountTransactions(c.Context(), orgID, accountIDs, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderAccountTransactions(h.orgName(c, orgID), from, to, lines)))
}

// GeneralLedgerDetail renders one block per account with opening balance,
// postings at a running balance, and the closing balance.
func (h *ReportHandler) GeneralLedgerDetail(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	fyStart, err := h.repos.Reports.FinancialYearStart(c.Context(), orgID, to)
	if err != nil {
		return httpError(err)
	}
	from, err := dateParam(c, "fromDate", fyStart)
	if err != nil {
		return err
	}
	groups, err := h.repos.Reports.GeneralLedgerDetail(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderGeneralLedgerDetail(h.orgName(c, orgID), from, to, groups)))
}

// AgedReceivables is Xero's Aged Receivables Summary: one row per customer with
// an outstanding invoice, in Xero's five ageing columns.
func (h *ReportHandler) AgedReceivables(c fiber.Ctx) error {
	return h.aged(c, models.InvoiceTypeAccRec, "AgedReceivables", "Aged Receivables")
}

// AgedPayables is Xero's Aged Payables Summary: the same table for suppliers,
// plus the Expense Claims block and the overall Total that includes it.
func (h *ReportHandler) AgedPayables(c fiber.Ctx) error {
	return h.aged(c, models.InvoiceTypeAccPay, "AgedPayables", "Aged Payables")
}

// AgedReceivablesByContact and AgedPayablesByContact are the per-contact
// drill-down behind the two summary reports above: the same outstanding
// invoices, one row each, blocked under the contact that owes or is owed.
func (h *ReportHandler) AgedReceivablesByContact(c fiber.Ctx) error {
	return h.agedByContact(c, models.InvoiceTypeAccRec, "AgedReceivablesByContact", "Aged Receivables by Contact")
}

func (h *ReportHandler) AgedPayablesByContact(c fiber.Ctx) error {
	return h.agedByContact(c, models.InvoiceTypeAccPay, "AgedPayablesByContact", "Aged Payables by Contact")
}

// contactIDFromQuery reads the optional ?contactID filter (Xero spells it
// ContactID).
func contactIDFromQuery(c fiber.Ctx) (*uuid.UUID, error) {
	return parseOptionalUUID(firstNonBlank(c.Query("contactID"), c.Query("ContactID")), "contactID")
}

// aged renders one of the two aged summary reports. Both are the same table of
// outstanding invoices by contact; the payables report is Xero's *three-block*
// report, though, and adds the organisation's unpaid expense claims grouped by
// claimant before its overall Total, because those claims are payables too
// (docs/xero-reference/aged-payables-summary.txt: 8,386.76 + 115.95 = 8,502.71).
// The receivables report has one block: Xero has no second block to add there.
//
// A `contactID` filter narrows the report to one contact's invoices and is
// applied to those rows only, so the Expense Claims block is left out of a
// contact-filtered payables report: an expense claim is recorded against a user,
// not against a contact, and none of them is that contact's payable.
func (h *ReportHandler) aged(c fiber.Ctx, invoiceType, reportID, name string) error {
	orgID := middleware.OrganisationIDFrom(c)
	asOf, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	contactID, err := contactIDFromQuery(c)
	if err != nil {
		return err
	}
	rows, err := h.repos.Reports.Aged(c.Context(), orgID, invoiceType, asOf, contactID)
	if err != nil {
		return httpError(err)
	}
	var claims []repository.AgedClaimRow
	if invoiceType == models.InvoiceTypeAccPay && contactID == nil {
		claims, err = h.repos.Reports.AgedExpenseClaims(c.Context(), orgID, asOf)
		if err != nil {
			return httpError(err)
		}
	}
	return c.JSON(renderXeroReport(renderAged(reportID, name, h.orgName(c, orgID), asOf, rows, claims)))
}

func (h *ReportHandler) agedByContact(c fiber.Ctx, invoiceType, reportID, name string) error {
	orgID := middleware.OrganisationIDFrom(c)
	asOf, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	contactID, err := contactIDFromQuery(c)
	if err != nil {
		return err
	}
	rows, err := h.repos.Reports.AgedByContact(c.Context(), orgID, invoiceType, asOf, contactID)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderAgedByContact(reportID, name, h.orgName(c, orgID), asOf, rows)))
}

func (h *ReportHandler) BankSummary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	from, err := dateParam(c, "fromDate", time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	rows, err := h.repos.Reports.BankSummary(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderBankSummary(h.orgName(c, orgID), from, to, rows)))
}

// CashSummary explains the period's cash movement: the accounts the money moved
// to and from, grouped the way Xero's Cash Summary groups them (income, less
// expenses, other cash movements, tax movements), closed with the bank balances
// either side of the window. It is a different aggregation from the Bank
// Summary, not a relabelled copy of it — the Bank Summary states where the cash
// ended up, this report states where it went.
func (h *ReportHandler) CashSummary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	// The Cash Summary is a year-to-date statement — its column is headed by
	// the year and its comparatives are yearly averages — so the window
	// defaults to the financial year the report is dated in, the way the other
	// year-to-date reports default. An explicit ?fromDate still wins.
	fyStart, err := h.repos.Reports.FinancialYearStart(c.Context(), orgID, to)
	if err != nil {
		return httpError(err)
	}
	from, err := dateParam(c, "fromDate", fyStart)
	if err != nil {
		return err
	}
	cs, err := h.repos.Reports.CashSummary(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderCashSummary(h.orgName(c, orgID), from, to, cs)))
}

// ExecutiveSummary derives KPIs (income, cash, AR/AP, net profit). The flow
// KPIs cover [from, to] — Xero's "this month" by default, or an explicit
// ?fromDate; the balance KPIs (AR, AP, net assets, closing bank) are as at the
// report's end date, which is exactly what Xero means by "as at end of period":
// ?date when the caller sends it, otherwise the ?toDate of the period they asked
// for, otherwise today.
func (h *ReportHandler) ExecutiveSummary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := asAtParam(c, time.Now().UTC())
	if err != nil {
		return err
	}
	from, err := dateParam(c, "fromDate", time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	kpis, err := h.repos.Reports.ExecutiveSummary(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderExecutiveSummary(h.orgName(c, orgID), from, to, kpis)))
}

// BudgetSummary answers with Xero's empty Budget Summary, and says so in the
// report: `migrations/` has no budget table (no `budgets`, `budget_lines` or
// equivalent), so there is no budget figure anywhere in the schema to aggregate.
// Returning an empty Xero report is what the real API does for an organisation
// with no budget set, so a client parsing it behaves identically. What the
// report must not do is invent a figure: an actuals-derived number would silently
// misreport plan against actual, and the `Total 0.00` row it used to print was a
// total of nothing. renderBudgetSummary states the gap in the report's titles
// instead. Implement it the day a budget table lands; nothing else in the report
// layer blocks it.
func (h *ReportHandler) BudgetSummary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := asAtParam(c, time.Now().UTC())
	if err != nil {
		return err
	}
	// The window is read for the same reason every other report reads it: a
	// caller who asks for a period must not be handed a report that names a
	// different one. This report has no budget to aggregate over a window, so an
	// explicit ?fromDate can only show up in its title — which is where the
	// caller will look for it, and where Xero puts the period too.
	from, err := optionalDateParam(c, "fromDate")
	if err != nil {
		return err
	}
	return c.JSON(renderXeroReport(renderBudgetSummary(h.orgName(c, orgID), from, to)))
}

// ReportsList enumerates the report endpoints currently implemented so the
// frontend can render the Reports index without hardcoding slugs.
//
// Every entry here answers with the Xero Reports envelope, and that is what
// makes the index a list of *reports*. `/reports/invoice-summary` is
// deliberately absent: that route serves the invoices screens' own KPI object
// ({totalInvoices, draft, authorised, paid, overdue, totalDue, totalPaid}),
// not a report, so advertising it here would promise a shape the route does not
// serve. TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope enumerates the
// registered routes and fails if the index and the routes ever disagree.
func (h *ReportHandler) ReportsList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"Reports": []fiber.Map{
			{"ReportID": "TrialBalance", "ReportName": "Trial Balance", "Path": "/reports/trial-balance"},
			{"ReportID": "ProfitAndLoss", "ReportName": "Profit and Loss", "Path": "/reports/profit-and-loss"},
			{"ReportID": "BalanceSheet", "ReportName": "Balance Sheet", "Path": "/reports/balance-sheet"},
			{"ReportID": "CashSummary", "ReportName": "Cash Summary", "Path": "/reports/cash-summary"},
			{"ReportID": "BankSummary", "ReportName": "Bank Summary", "Path": "/reports/bank-summary"},
			{"ReportID": "AgedReceivables", "ReportName": "Aged Receivables", "Path": "/reports/aged-receivables"},
			{"ReportID": "AgedPayables", "ReportName": "Aged Payables", "Path": "/reports/aged-payables"},
			{"ReportID": "ExecutiveSummary", "ReportName": "Executive Summary", "Path": "/reports/executive-summary"},
			{"ReportID": "BudgetSummary", "ReportName": "Budget Summary", "Path": "/reports/budget-summary"},
			{"ReportID": "BASReport", "ReportName": "BAS / Sales Tax Report", "Path": "/reports/bas"},
			{"ReportID": "SalesTaxReport", "ReportName": "Sales Tax Report", "Path": "/reports/sales-tax"},
			{"ReportID": "JournalReport", "ReportName": "Journal Report", "Path": "/reports/journal-report"},
			{"ReportID": "AccountTransactions", "ReportName": "Account Transactions", "Path": "/reports/account-transactions"},
			{"ReportID": "GeneralLedgerDetail", "ReportName": "General Ledger Detail", "Path": "/reports/general-ledger-detail"},
			{"ReportID": "AgedReceivablesByContact", "ReportName": "Aged Receivables by Contact", "Path": "/reports/aged-receivables-by-contact"},
			{"ReportID": "AgedPayablesByContact", "ReportName": "Aged Payables by Contact", "Path": "/reports/aged-payables-by-contact"},
			{"ReportID": "Form1120", "ReportName": "Form 1120", "Path": "/reports/form-1120"},
		},
	})
}

// BAS returns the sales-tax workpaper over the requested period, titled as the
// BAS. It is the same workpaper /reports/sales-tax returns — Xero's BAS is the
// Australian return built on the same tax rates — and the two routes differ
// only in what they call themselves, so a client can tell which it asked for.
func (h *ReportHandler) BAS(c fiber.Ctx) error {
	return h.salesTax(c, "BASReport", "BAS / Sales Tax Report")
}

// SalesTax is the plain sales-tax view of the same workpaper.
func (h *ReportHandler) SalesTax(c fiber.Ctx) error {
	return h.salesTax(c, "SalesTaxReport", "Sales Tax Report")
}

// salesTax builds one row per tax rate the organisation's own coded journals
// carry, plus a Total row. Nothing about the set of rates is assumed: a rate
// the organisation has never posted under is not a row, and a rate it has
// posted under is, whatever it is called.
func (h *ReportHandler) salesTax(c fiber.Ctx, reportID, reportName string) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	from, err := dateParam(c, "fromDate", time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	rows, err := h.repos.Reports.SalesTaxByRate(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderSalesTax(h.orgName(c, orgID), reportID, reportName, from, to, rows)))
}

// JournalReport exposes the raw GL feed used by every report — useful for
// reconciliation and audit trails. It mirrors Xero's `GET /Journals` endpoint
// (already exposed at `/api/v1/journals`) but wraps it as a Report.
func (h *ReportHandler) JournalReport(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "toDate", time.Now().UTC())
	if err != nil {
		return err
	}
	from, err := dateParam(c, "fromDate", time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	lines, err := h.repos.Reports.JournalFeed(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	r := models.Report{
		ReportID:   "JournalReport",
		ReportName: "Journal Report",
		ReportType: "JournalReport",
		ReportTitles: []string{
			"Journal Report",
			h.orgName(c, orgID),
			"From " + xeroDate(from) + " To " + xeroDate(to),
		},
		ReportDate: xeroDate(to),
		Rows: []models.ReportRow{
			headerRow("Date", "Source", "Reference", "Account", "Debit", "Credit"),
		},
	}
	section := models.ReportRow{RowType: models.ReportRowTypeSection}
	for _, l := range lines {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				txt(l.Date.Format("2006-01-02")),
				txt(l.Source),
				txt(l.Reference),
				accountCell(l.AccountID, l.AccountCode, l.AccountName),
				money(l.Debit),
				money(l.Credit),
			},
		})
	}
	if len(section.Rows) > 0 {
		r.Rows = append(r.Rows, section)
	}
	return c.JSON(renderXeroReport(r))
}
