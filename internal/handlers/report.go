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
//	date, fromDate, toDate  — ISO YYYY-MM-DD
//	compare                 — bool: add a comparative column (P&L and Balance
//	                          Sheet only). Defaults to the same window one year
//	                          earlier, or to the organisation's financial year
//	                          start for the Balance Sheet — which is exactly
//	                          what IRS Schedule L needs.
//	compareDate             — ISO YYYY-MM-DD: explicit comparative date (BS)
//	compareFromDate/ToDate  — ISO YYYY-MM-DD: explicit comparative window (P&L)
//	accountID               — repeatable UUID: restrict Account Transactions /
//	                          General Ledger Detail to specific accounts
//	periods                 — int, number of periods to compare (1…12)
//	timeframe               — MONTH|QUARTER|YEAR (default MONTH)
//	trackingCategoryID      — UUID (accepted for forward-compat; currently
//	                          ignored — tracking splits land in a later phase)
//	standardLayout          — bool, currently accepted for forward-compat
//	paymentsOnly            — bool (P&L only, forward-compat)
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
	to, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	// Period defaults to the month containing `date`, matching Xero: the
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

func (h *ReportHandler) AgedReceivables(c fiber.Ctx) error {
	return h.aged(c, models.InvoiceTypeAccRec, "AgedReceivables", "Aged Receivables")
}

func (h *ReportHandler) AgedPayables(c fiber.Ctx) error {
	return h.aged(c, models.InvoiceTypeAccPay, "AgedPayables", "Aged Payables")
}

func (h *ReportHandler) aged(c fiber.Ctx, invoiceType, reportID, name string) error {
	orgID := middleware.OrganisationIDFrom(c)
	asOf, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	rows, err := h.repos.Reports.Aged(c.Context(), orgID, invoiceType, asOf)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderAged(reportID, name, h.orgName(c, orgID), asOf, rows)))
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

// CashSummary uses the same underlying BankSummary aggregation but is labelled
// differently so Xero clients identify it as Cash Summary.
func (h *ReportHandler) CashSummary(c fiber.Ctx) error {
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
	return c.JSON(renderXeroReport(renderCashSummary(h.orgName(c, orgID), from, to, rows)))
}

// ExecutiveSummary derives KPIs (income, cash, AR/AP, net profit).
func (h *ReportHandler) ExecutiveSummary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	from := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
	kpis, err := h.repos.Reports.ExecutiveSummary(c.Context(), orgID, from, to)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderExecutiveSummary(h.orgName(c, orgID), to, kpis)))
}

// BudgetSummary is a forward-compat stub — Xero returns an empty report
// when no budget rows exist, which is what we do until budgets ship.
func (h *ReportHandler) BudgetSummary(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	to, err := dateParam(c, "date", time.Now().UTC())
	if err != nil {
		return err
	}
	return c.JSON(renderXeroReport(renderBudgetSummary(h.orgName(c, orgID), to)))
}

// ReportsList enumerates the report endpoints currently implemented so the
// frontend can render the Reports index without hardcoding slugs.
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
			{"ReportID": "InvoiceSummary", "ReportName": "Invoice Summary", "Path": "/reports/invoice-summary"},
		},
	})
}

// BAS returns a simple sales-tax summary over the requested period — useful for
// GST/BAS/VAT returns. Positive `TaxCollected` = tax charged on sales, negative
// `TaxPaid` = tax claimable on purchases.
func (h *ReportHandler) BAS(c fiber.Ctx) error {
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
	r := models.Report{
		ReportID:   "BASReport",
		ReportName: "Sales Tax Report",
		ReportType: "BASReport",
		ReportTitles: []string{
			"Sales Tax Report",
			h.orgName(c, orgID),
			"From " + xeroDate(from) + " To " + xeroDate(to),
		},
		ReportDate: xeroDate(to),
		Rows: []models.ReportRow{
			headerRow("Tax Rate", "Net Sales", "Net Purchases", "Tax Collected", "Tax Paid", "Net Tax"),
		},
	}
	section := models.ReportRow{RowType: models.ReportRowTypeSection}
	for _, row := range rows {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				txt(row.TaxType),
				money(row.NetSales),
				money(row.NetPurchases),
				money(row.TaxCollected),
				money(row.TaxPaid),
				money(row.TaxCollected.Sub(row.TaxPaid)),
			},
		})
	}
	if len(section.Rows) > 0 {
		r.Rows = append(r.Rows, section)
	}
	return c.JSON(renderXeroReport(r))
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
