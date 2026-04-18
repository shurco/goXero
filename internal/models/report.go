package models

import "time"

// Xero Reporting API response shape. Mirrors
// https://developer.xero.com/documentation/api/accounting/reports — every
// report endpoint returns `{"Reports":[{ … }]}` with a tree of `Rows[]`
// classified by `RowType`. Downstream clients (Xero SDKs, Excel add-ins)
// rely on this structure so matching it precisely is important.
type ReportsEnvelope struct {
	ID           string    `json:"Id"`
	Status       string    `json:"Status"`
	ProviderName string    `json:"ProviderName"`
	DateTimeUTC  time.Time `json:"DateTimeUTC"`
	Reports      []Report  `json:"Reports"`
}

type Report struct {
	ReportID       string      `json:"ReportID"`
	ReportName     string      `json:"ReportName"`
	ReportType     string      `json:"ReportType"`
	ReportTitles   []string    `json:"ReportTitles"`
	ReportDate     string      `json:"ReportDate"`
	UpdatedDateUTC time.Time   `json:"UpdatedDateUTC"`
	Fields         []any       `json:"Fields"`
	Rows           []ReportRow `json:"Rows"`
}

// ReportRowType enumerates the RowType discriminator used by Xero clients
// to render headings, sections, rows and summary totals differently.
const (
	ReportRowTypeHeader  = "Header"
	ReportRowTypeSection = "Section"
	ReportRowTypeRow     = "Row"
	ReportRowTypeSummary = "SummaryRow"
)

type ReportRow struct {
	RowType string       `json:"RowType"`
	Title   string       `json:"Title,omitempty"`
	Cells   []ReportCell `json:"Cells,omitempty"`
	Rows    []ReportRow  `json:"Rows,omitempty"`
}

type ReportCell struct {
	Value      string                `json:"Value"`
	Attributes []ReportCellAttribute `json:"Attributes,omitempty"`
}

type ReportCellAttribute struct {
	ID    string `json:"Id"`
	Value string `json:"Value"`
}
