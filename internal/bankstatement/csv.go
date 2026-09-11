package bankstatement

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/shopspring/decimal"
)

// Mapping tells ApplyCSV which column feeds which statement field. Field values
// are column *names* (when the file has a header row) or zero-based indices
// rendered as strings (when it does not).
//
// The shape mirrors what the import wizard's "Import settings" step collects,
// so the frontend can post its form state straight through.
type Mapping struct {
	HasHeader        bool   `json:"HasHeader"`
	SkipRows         int    `json:"SkipRows"`
	DateFormat       string `json:"DateFormat,omitempty"`
	DecimalSeparator string `json:"DecimalSeparator,omitempty"`
	// AmountMode is "SIGNED" (one signed amount column, Xero's default),
	// "DEBIT_CREDIT" (separate money-out / money-in columns), or
	// "AMOUNT_WITH_TYPE" (an amount plus a sign indicator column).
	AmountMode string `json:"AmountMode,omitempty"`

	Date         string `json:"Date,omitempty"`
	Amount       string `json:"Amount,omitempty"`
	Debit        string `json:"Debit,omitempty"`
	Credit       string `json:"Credit,omitempty"`
	Type         string `json:"Type,omitempty"` // sign indicator for AMOUNT_WITH_TYPE
	Payee        string `json:"Payee,omitempty"`
	Description  string `json:"Description,omitempty"`
	Reference    string `json:"Reference,omitempty"`
	ChequeNumber string `json:"ChequeNumber,omitempty"`
	Balance      string `json:"Balance,omitempty"`

	// NegativeIsDebit is false when the file writes debts as positive numbers,
	// so every amount must be negated to reach our sign convention.
	NegativeIsDebit *bool `json:"NegativeIsDebit,omitempty"`
}

// DetectedColumn is one column of an uploaded CSV plus our guess at its role.
type DetectedColumn struct {
	Name    string   `json:"Name"`
	Index   int      `json:"Index"`
	Samples []string `json:"Samples,omitempty"`
	// Guess is the Mapping field name we think this column is, or "".
	Guess string `json:"Guess,omitempty"`
}

// columnAliases maps a normalised header to a Mapping field.
var columnAliases = map[string]string{
	"date": "Date", "transactiondate": "Date", "transdate": "Date", "posteddate": "Date",
	"postingdate": "Date", "valuedate": "Date", "effectivedate": "Date", "дата": "Date",
	"when": "Date", "dateoftransaction": "Date", "bookdate": "Date",
	"amount": "Amount", "transactionamount": "Amount", "value": "Amount", "сумма": "Amount",
	"amountusd": "Amount", "amountgbp": "Amount", "amounteur": "Amount",
	"debit": "Debit", "debitamount": "Debit", "withdrawal": "Debit", "withdrawals": "Debit",
	"moneyout": "Debit", "paidout": "Debit", "списание": "Debit", "debitamountusd": "Debit",
	"credit": "Credit", "creditamount": "Credit", "deposit": "Credit", "deposits": "Credit",
	"moneyin": "Credit", "paidin": "Credit", "зачисление": "Credit", "creditamountusd": "Credit",
	"type": "Type", "transactiontype": "Type", "debitcredit": "Type", "drcr": "Type",
	"payee": "Payee", "name": "Payee", "merchant": "Payee", "counterparty": "Payee",
	"payeename": "Payee", "получатель": "Payee",
	"description": "Description", "narrative": "Description", "details": "Description",
	"memo": "Description", "particulars": "Description", "transactiondescription": "Description",
	"additionalinfo": "Description", "назначение": "Description",
	"reference": "Reference", "ref": "Reference", "referencenumber": "Reference",
	"transactionid": "Reference", "id": "Reference",
	"checknumber": "ChequeNumber", "chequenumber": "ChequeNumber", "checkno": "ChequeNumber",
	"chequeno": "ChequeNumber", "checknum": "ChequeNumber", "serialnumber": "ChequeNumber",
	"number":  "ChequeNumber",
	"balance": "Balance", "runningbalance": "Balance", "balanceamount": "Balance",
	"остаток": "Balance", "ledgerbalance": "Balance",
}

// ParseCSV reads a delimited statement file and returns it with the raw grid
// intact, ready for a mapping to be chosen and applied.
func ParseCSV(data []byte) (*Statement, error) {
	data = stripBOM(data)
	delim := detectDelimiter(data)
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = delim
	reader.FieldsPerRecord = -1 // banks are inconsistent; normalise ourselves
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	var rows [][]string
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip malformed lines rather than failing the whole import — a
			// trailing summary line is common.
			continue
		}
		if len(rec) == 0 || allBlank(rec) {
			continue
		}
		rows = append(rows, trimAll(rec))
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: no rows found", ErrUnsupportedFormat)
	}

	st := &Statement{Format: FormatCSV, Rows: rows, Delimiter: string(delim)}
	st.HasHeader = looksLikeHeader(rows[0], rows[min(1, len(rows)-1)])
	if st.HasHeader {
		st.Columns = rows[0]
		st.Rows = rows[1:]
	} else {
		st.Columns = indexNames(len(rows[0]))
		st.Rows = rows
	}
	return st, nil
}

// DetectedColumns reports each column with sample values and our best guess at
// its role — this is what the wizard renders in the "Import settings" step.
func DetectedColumns(st *Statement) []DetectedColumn {
	if st == nil || len(st.Columns) == 0 {
		return nil
	}
	guess := DetectMapping(st.Columns)
	out := make([]DetectedColumn, 0, len(st.Columns))
	for i, name := range st.Columns {
		col := DetectedColumn{Name: name, Index: i}
		for r := 0; r < len(st.Rows) && r < 3; r++ {
			if i < len(st.Rows[r]) {
				col.Samples = append(col.Samples, st.Rows[r][i])
			}
		}
		col.Guess = guessField(guess, name)
		out = append(out, col)
	}
	return out
}

// DetectMapping guesses a Mapping from the column headers, mirroring the
// defaults Xero applies after you upload a CSV.
func DetectMapping(columns []string) Mapping {
	// DecimalSeparator is left empty, which means "detect it per value": a
	// bank exporting "1.234,56" and one exporting "1,234.56" both work
	// without the user having to tell us which convention they use.
	m := Mapping{HasHeader: true, AmountMode: "SIGNED"}
	for _, c := range columns {
		field := columnAliases[normaliseHeader(c)]
		if field == "" {
			continue
		}
		assign(&m, field, c)
	}
	// A file with separate debit/credit columns needs the other amount mode.
	if m.Debit != "" || m.Credit != "" {
		if m.Amount == "" {
			m.AmountMode = "DEBIT_CREDIT"
		} else {
			m.AmountMode = "AMOUNT_WITH_TYPE"
		}
	}
	if m.Payee == "" && m.Description == "" {
		// Fall back to the first free-text column so something lands in Payee.
		for _, c := range columns {
			if columnAliases[normaliseHeader(c)] == "" {
				m.Payee = c
				break
			}
		}
	}
	return m
}

// assign fills `field` unless it is already taken — first matching column wins,
// so a header appearing twice does not silently override the earlier one.
func assign(m *Mapping, field, column string) {
	switch field {
	case "Date":
		if m.Date == "" {
			m.Date = column
		}
	case "Amount":
		if m.Amount == "" {
			m.Amount = column
		}
	case "Debit":
		if m.Debit == "" {
			m.Debit = column
		}
	case "Credit":
		if m.Credit == "" {
			m.Credit = column
		}
	case "Type":
		if m.Type == "" {
			m.Type = column
		}
	case "Payee":
		if m.Payee == "" {
			m.Payee = column
		}
	case "Description":
		if m.Description == "" {
			m.Description = column
		}
	case "Reference":
		if m.Reference == "" {
			m.Reference = column
		}
	case "ChequeNumber":
		if m.ChequeNumber == "" {
			m.ChequeNumber = column
		}
	case "Balance":
		if m.Balance == "" {
			m.Balance = column
		}
	}
}

func guessField(m Mapping, column string) string {
	switch column {
	case m.Date:
		return "Date"
	case m.Amount:
		return "Amount"
	case m.Debit:
		return "Debit"
	case m.Credit:
		return "Credit"
	case m.Type:
		return "Type"
	case m.Payee:
		return "Payee"
	case m.Description:
		return "Description"
	case m.Reference:
		return "Reference"
	case m.ChequeNumber:
		return "ChequeNumber"
	case m.Balance:
		return "Balance"
	}
	return ""
}

// ApplyCSV turns the stored grid plus a user-confirmed Mapping into lines.
// It is the only place that knows how a CSV row becomes a statement line, so
// the wizard's preview and the final commit can never disagree.
func ApplyCSV(st *Statement, m Mapping) ([]Line, error) {
	if st == nil || len(st.Columns) == 0 {
		return nil, fmt.Errorf("%w: no CSV columns", ErrUnsupportedFormat)
	}
	if m.Date == "" {
		return nil, fmt.Errorf("%w: no date column selected", ErrUnsupportedFormat)
	}
	if m.Amount == "" && m.Debit == "" && m.Credit == "" {
		return nil, fmt.Errorf("%w: no amount column selected", ErrUnsupportedFormat)
	}
	// SkipRows counts lines from the top of the file. When the header row has
	// already been stripped from st.Rows, a SkipRows of 1 (Xero's "the first
	// line is column headings") must not eat a real data row.
	skip := m.SkipRows
	if st.HasHeader && skip > 0 {
		skip--
	}
	rows := st.Rows
	if skip > 0 {
		if skip >= len(rows) {
			rows = nil
		} else {
			rows = rows[skip:]
		}
	}

	// Pick the date layout once, from a sample of the whole file.
	samples := make([]string, 0, len(rows))
	for _, r := range rows {
		if v, ok := cell(r, st.Columns, m.Date); ok {
			samples = append(samples, v)
		}
	}
	layout := detectDateFormat(samples, m.DateFormat)

	out := make([]Line, 0, len(rows))
	for i, r := range rows {
		raw, _ := cell(r, st.Columns, m.Date)
		d, err := parseDate(raw, layout)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w: %q", i+1, errUnparseableDate, raw)
		}
		amount, err := amountFromRow(r, st.Columns, m)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i+1, err)
		}
		payee, _ := cell(r, st.Columns, m.Payee)
		desc, _ := cell(r, st.Columns, m.Description)
		ref, _ := cell(r, st.Columns, m.Reference)
		cheque, _ := cell(r, st.Columns, m.ChequeNumber)

		l := Line{
			Date:         d,
			Amount:       amount,
			Payee:        payee,
			Description:  desc,
			Reference:    ref,
			ChequeNumber: cheque,
		}
		if balRaw, ok := cell(r, st.Columns, m.Balance); ok && strings.TrimSpace(balRaw) != "" {
			if bal, err := parseAmount(balRaw, m.DecimalSeparator); err == nil {
				l.Balance = &bal
			}
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no data rows", ErrUnsupportedFormat)
	}
	applyStatementBounds(st, out)
	return out, nil
}

// amountFromRow resolves the signed amount according to the mapping's mode.
func amountFromRow(row, columns []string, m Mapping) (decimal.Decimal, error) {
	switch m.AmountMode {
	case "DEBIT_CREDIT":
		debitRaw, _ := cell(row, columns, m.Debit)
		creditRaw, _ := cell(row, columns, m.Credit)
		debit, errD := parseAmount(debitRaw, m.DecimalSeparator)
		credit, errC := parseAmount(creditRaw, m.DecimalSeparator)
		switch {
		case errD == nil && !debit.IsZero():
			return debit.Abs().Neg(), nil
		case errC == nil && !credit.IsZero():
			return credit.Abs(), nil
		case errD == nil && errC == nil:
			return decimal.Zero, nil
		}
		return decimal.Zero, errUnparseableAmount
	default:
		raw, _ := cell(row, columns, m.Amount)
		amount, err := parseAmount(raw, m.DecimalSeparator)
		if err != nil {
			return decimal.Zero, err
		}
		if m.AmountMode == "AMOUNT_WITH_TYPE" {
			typeRaw, _ := cell(row, columns, m.Type)
			if isDebitIndicator(typeRaw) {
				amount = amount.Abs().Neg()
			} else if isCreditIndicator(typeRaw) {
				amount = amount.Abs()
			}
		}
		if m.NegativeIsDebit != nil && !*m.NegativeIsDebit {
			amount = amount.Neg()
		}
		return amount, nil
	}
}

func isDebitIndicator(s string) bool {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "D", "DR", "DEBIT", "WITHDRAWAL", "PAYMENT", "SPEND", "СПИСАНИЕ":
		return true
	}
	return false
}

func isCreditIndicator(s string) bool {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "C", "CR", "CREDIT", "DEPOSIT", "RECEIPT", "RECEIVE", "ЗАЧИСЛЕНИЕ":
		return true
	}
	return false
}

// cell looks a column up by header name, or by numeric index when the file had
// no header row.
func cell(row, columns []string, column string) (string, bool) {
	if column == "" {
		return "", false
	}
	idx := -1
	for i, name := range columns {
		if name == column {
			idx = i
			break
		}
	}
	if idx < 0 {
		if n, err := parseInt(column); err == nil {
			idx = n
		}
	}
	if idx < 0 || idx >= len(row) {
		return "", false
	}
	return row[idx], true
}

// applyStatementBounds fills a statement's start, end and closing from its
// lines — but only where the file did not already declare them. OFX's
// DTSTART/DTEND/LEDGERBAL are authoritative and can legitimately differ from
// the listed transactions (a running balance differs from their sum).
func applyStatementBounds(st *Statement, lines []Line) {
	if st.Start == nil {
		for i := range lines {
			if st.Start == nil || lines[i].Date.Before(*st.Start) {
				v := lines[i].Date
				st.Start = &v
			}
		}
	}
	if st.End == nil {
		for i := range lines {
			if st.End == nil || lines[i].Date.After(*st.End) {
				v := lines[i].Date
				st.End = &v
			}
		}
	}
	// A running-balance column also gives us the statement's closing balance.
	if n := len(lines); st.Closing == nil && n > 0 && lines[n-1].Balance != nil {
		st.Closing = lines[n-1].Balance
	}
}

func detectDelimiter(data []byte) rune {
	candidates := []rune{',', ';', '\t', '|'}
	lines := strings.Split(firstN(string(data), 8192), "\n")
	best, bestScore := ',', -1
	for _, c := range candidates {
		counts := make([]int, 0, len(lines))
		for _, l := range lines[:min(len(lines), 20)] {
			if strings.TrimSpace(l) == "" {
				continue
			}
			counts = append(counts, strings.Count(l, string(c)))
		}
		if len(counts) == 0 || counts[0] == 0 {
			continue
		}
		// Score = how many rows share the modal count, then the count itself.
		modal, hits := 0, 0
		for _, n := range counts {
			freq := 0
			for _, m := range counts {
				if m == n {
					freq++
				}
			}
			if freq > hits || (freq == hits && n > modal) {
				modal, hits = n, freq
			}
		}
		if hits*100+modal > bestScore {
			best, bestScore = c, hits*100+modal
		}
	}
	return best
}

// looksLikeHeader decides whether the first row is a header. Heuristic: a
// header row has no parseable date and no parseable amount in any cell, while
// the following row has at least one of each.
func looksLikeHeader(first, second []string) bool {
	if parseableDateCount(first) > 0 || parseableAmountCount(first) > 0 {
		return false
	}
	if len(second) == 0 {
		return true
	}
	return parseableDateCount(second) > 0 && parseableAmountCount(second) > 0
}

func parseableDateCount(row []string) int {
	n := 0
	for _, c := range row {
		if _, err := parseDate(c, ""); err == nil {
			n++
		}
	}
	return n
}

func parseableAmountCount(row []string) int {
	n := 0
	for _, c := range row {
		if _, err := parseAmount(c, ""); err == nil {
			n++
		}
	}
	return n
}

func normaliseHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "*")
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '_', '-', '.', '(', ')', '/', '\\', '#', ':':
			return -1
		}
		return r
	}, s)
}

func indexNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%d", i)
	}
	return out
}

func stripBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
}

func trimAll(rec []string) []string {
	out := make([]string, len(rec))
	for i, v := range rec {
		out[i] = strings.TrimSpace(v)
	}
	return out
}

func allBlank(rec []string) bool {
	for _, v := range rec {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// firstN returns at most n leading bytes of s (UTF-8 safe enough for sniffing).
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func parseInt(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
