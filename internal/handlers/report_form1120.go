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

// Form 1120 workpaper — the report an accountant fills the annual US
// corporation income tax return from.
//
// It is not a copy of the form. It arranges the figures goXero already holds
// into the form's own Page 1 line structure, adds the beginning/end-of-year
// balance sheet (Schedule L) and the book-to-return reconciliation (Schedule
// M-1), and finishes with the accounts that no 1120 line claims — the section
// that tells the reader whether the workpaper is finished.
//
// The line structure, the account → line mapping and the arithmetic live in
// internal/repository/form1120.go; this file only renders them.

// form1120Sections is the print order and the column set of each section. The
// repository owns the section identifiers, the wording is presentation.
var form1120Sections = []struct {
	ID     string
	Title  string
	Header []string
}{
	{repository.Form1120SectionIncome, "Income", []string{"Line", "Amount"}},
	{repository.Form1120SectionDeductions, "Deductions", []string{"Line", "Amount"}},
	{repository.Form1120SectionTax, "Tax and taxable income", []string{"Line", "Amount"}},
	{repository.Form1120SectionScheduleL, "Schedule L — Balance sheet per books",
		[]string{"Line", "Beginning of year", "End of year"}},
	{repository.Form1120SectionScheduleM1, "Schedule M-1 — Reconciliation of income per books with income per return",
		[]string{"Line", "Amount"}},
}

// Form1120 renders the workpaper for the requested period.
//
// Query parameters follow the other reports: `toDate` (ISO, default today) and
// `fromDate` (ISO, default the organisation's financial year start for that
// date). The financial year end the organisation is configured with is what
// decides where the return's year begins, exactly as FinancialYearStart does
// for the other reports.
func (h *ReportHandler) Form1120(c fiber.Ctx) error {
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
	if to.Before(from) {
		return fiber.NewError(fiber.StatusBadRequest, "toDate must not be before fromDate")
	}
	report, err := h.repos.Reports.Form1120(c.Context(), orgID, repository.Period{From: from, To: to})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(renderXeroReport(renderForm1120(h.orgName(c, orgID), report)))
}

// renderForm1120 emits the workpaper in the canonical Xero "Reports" shape: one
// outer section per part of the workpaper (income, deductions, tax and taxable
// income, Schedule L, Schedule M-1, unmapped accounts), one nested section per
// form line, and inside that the accounts behind the line followed by the
// figure that goes on the form.
//
// Every account carries its AccountID as a cell attribute, so a consumer can
// join each figure back to the chart of accounts — which is what makes both the
// drill-down and the "no 1120 line" section actionable.
func renderForm1120(orgName string, rep *repository.Form1120Report) models.Report {
	r := models.Report{
		ReportID:   "Form1120",
		ReportName: "Form 1120 Workpaper",
		ReportType: "Form1120",
		ReportTitles: []string{
			"Form 1120 Workpaper",
			orgName,
			dateRangeLabel(rep.From, rep.To),
		},
		ReportDate: xeroDate(rep.To),
		Rows:       []models.ReportRow{headerRow("Line", "Amount")},
	}
	for _, section := range form1120Sections {
		rows := models.ReportRow{RowType: models.ReportRowTypeSection, Title: section.Title}
		// Schedule L prints two date columns, so it re-states its own header
		// rather than inheriting the "Line / Amount" pair above it.
		if section.ID == repository.Form1120SectionScheduleL {
			rows.Rows = append(rows.Rows, headerRow(section.Header...))
		}
		for _, line := range rep.Lines {
			if line.Section != section.ID {
				continue
			}
			rows.Rows = append(rows.Rows, form1120LineRow(line))
		}
		r.Rows = append(r.Rows, rows)
	}
	r.Rows = append(r.Rows, form1120UnmappedRow(rep))
	return r
}

// form1120LineRow renders one line of the form: a section titled the way the
// form prints the line, the accounts that make it up, any note the line needs,
// and the figure itself as the section's summary row.
func form1120LineRow(line repository.Form1120Line) models.ReportRow {
	title := strings.TrimSpace(line.ID + " " + line.Label)
	section := models.ReportRow{RowType: models.ReportRowTypeSection, Title: title}
	for _, account := range line.Accounts {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells:   form1120AccountCells(account, line.Basis),
		})
	}
	if line.Note != "" {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells:   []models.ReportCell{txt(line.Note)},
		})
	}
	cells := []models.ReportCell{txt(title), money(line.Amount)}
	if line.Basis == repository.Form1120BasisClosing {
		cells = []models.ReportCell{txt(title), money(line.Beginning), money(line.Amount)}
	}
	section.Rows = append(section.Rows, models.ReportRow{
		RowType: models.ReportRowTypeSummary,
		Cells:   cells,
	})
	return section
}

// form1120AccountCells renders one account behind a line, in the column set of
// the line it belongs to.
func form1120AccountCells(account repository.Form1120Account, basis string) []models.ReportCell {
	label := form1120AccountLabel(account)
	if basis == repository.Form1120BasisClosing {
		return []models.ReportCell{label, money(account.Beginning), money(account.Closing)}
	}
	return []models.ReportCell{label, money(account.Period)}
}

// form1120AccountLabel links a real account back to the chart of accounts; the
// two derived figures the workpaper adds (the unclosed retained earnings and
// the Profit & Loss bottom line) have no account and print as plain text.
func form1120AccountLabel(account repository.Form1120Account) models.ReportCell {
	if account.AccountID == uuid.Nil {
		return txt(account.Name)
	}
	return accountCell(account.AccountID, account.Code, account.Name)
}

// form1120UnmappedRow is the last section of the workpaper and the reason it
// exists: every account that carries a figure and that no 1120 line claims.
// Until it is empty, the return cannot be filled in from these books.
func form1120UnmappedRow(rep *repository.Form1120Report) models.ReportRow {
	section := models.ReportRow{
		RowType: models.ReportRowTypeSection,
		Title:   "Accounts with no 1120 line",
	}
	section.Rows = append(section.Rows, models.ReportRow{
		RowType: models.ReportRowTypeRow,
		Cells: []models.ReportCell{txt("These accounts carry a figure that no 1120 line above claims. " +
			"Until each one is placed in report_line_mappings — the organisation's own table of which " +
			"account goes on which line (migration 00028) — or written off, this workpaper is incomplete.")},
	})
	section.Rows = append(section.Rows, headerRow("Account", "Type", "Period amount", "Closing balance"))
	for _, account := range rep.Unmapped {
		// An income statement account has no balance of its own: the balance
		// sheet carries its effect in retained earnings, so its closing column
		// stays blank rather than showing a zero it does not have.
		closing := money(account.Closing)
		if repository.Form1120IncomeStatementType(account.Type) {
			closing = models.ReportCell{Value: ""}
		}
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				form1120AccountLabel(account),
				txt(account.Type),
				money(account.Period),
				closing,
			},
		})
	}
	return section
}
