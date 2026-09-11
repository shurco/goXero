package bankstatement

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestParseDetectsFormat(t *testing.T) {
	cases := []struct {
		name string
		file string
		data string
		want string
	}{
		{
			name: "ofx by content despite csv extension",
			file: "statement.csv",
			data: "OFXHEADER:100\nDATA:OFXSGML\n<OFX><BANKMSGSRSV1></BANKMSGSRSV1></OFX>\n",
			want: FormatOFX,
		},
		{
			name: "qfx when the intuit header is present",
			file: "statement.ofx",
			data: "OFXHEADER:100\n<INTU.BID>1234\n<OFX></OFX>\n",
			want: FormatQFX,
		},
		{
			name: "qif by bang-type line",
			file: "statement.txt",
			data: "!Type:Bank\nD01/15/2026\nT-5.00\n^\n",
			want: FormatQIF,
		},
		{
			name: "csv by extension",
			file: "jan.csv",
			data: "Date,Amount\n2026-01-15,-5.00\n",
			want: FormatCSV,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DetectFormat(tc.file, []byte(tc.data))
			if !ok {
				t.Fatalf("DetectFormat: not recognised")
			}
			if got != tc.want {
				t.Fatalf("format = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseOFXSGMLUnclosedTags(t *testing.T) {
	data := `OFXHEADER:100
DATA:OFXSGML
VERSION:102

<OFX>
<SIGNONMSGSRSV1><SONRS><STATUS><CODE>0</STATUS></SONRS></SIGNONMSGSRSV1>
<BANKMSGSRSV1><STMTTRNRS><STMTRS>
<CURDEF>USD
<BANKACCTFROM><BANKID>021000021<ACCTID>1234567890<ACCTTYPE>CHECKING</BANKACCTFROM>
<BANKTRANLIST>
<DTSTART>20260101
<DTEND>20260131
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260115120000[-5:EST]
<TRNAMT>-42.50
<FITID>2026011500001
<NAME>COFFEE SHOP
<MEMO>Card purchase
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20260120
<TRNAMT>1500.00
<FITID>2026012000002
<NAME>ACME PAYROLL
</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL><BALAMT>3456.78<DTASOF>20260131</LEDGERBAL>
</STMTRS></STMTTRNRS></BANKMSGSRSV1>
</OFX>
`
	st, err := ParseOFX([]byte(data), FormatOFX, "jan.ofx")
	if err != nil {
		t.Fatalf("ParseOFX: %v", err)
	}
	if st.Currency != "USD" {
		t.Errorf("currency = %q, want USD", st.Currency)
	}
	if st.AccountID != "021000021 1234567890" {
		t.Errorf("account = %q", st.AccountID)
	}
	if len(st.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(st.Lines))
	}
	first := st.Lines[0]
	if first.Date.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("date = %s", first.Date)
	}
	if !first.Amount.Equal(decimal.RequireFromString("-42.50")) {
		t.Errorf("amount = %s, want -42.50", first.Amount)
	}
	if first.Payee != "COFFEE SHOP" {
		t.Errorf("payee = %q", first.Payee)
	}
	if first.ProviderTxID != "2026011500001" {
		t.Errorf("fitid = %q", first.ProviderTxID)
	}
	if !st.Lines[1].Amount.Equal(decimal.RequireFromString("1500.00")) {
		t.Errorf("credit amount = %s", st.Lines[1].Amount)
	}
	if st.Closing == nil || !st.Closing.Equal(decimal.RequireFromString("3456.78")) {
		t.Errorf("closing = %v", st.Closing)
	}
	if st.Start == nil || st.Start.Format("2006-01-02") != "2026-01-01" {
		t.Errorf("start = %v", st.Start)
	}
	// LEDGERBAL legitimately differs from the sum of the listed lines, so the
	// parsed closing balance must not be overwritten by applyStatementBounds.
	if st.End == nil || st.End.Format("2006-01-02") != "2026-01-31" {
		t.Errorf("end = %v", st.End)
	}
}

func TestParseOFXXMLClosedTags(t *testing.T) {
	data := `<?xml version="1.0" encoding="UTF-8"?>
<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS>
<CURDEF>EUR</CURDEF>
<BANKTRANLIST><DTSTART>20260301</DTSTART><DTEND>20260331</DTEND>
<STMTTRN><TRNTYPE>DEBIT</TRNTYPE><DTPOSTED>20260305</DTPOSTED><TRNAMT>-10,50</TRNAMT><FITID>x1</FITID><NAME>Cafe</NAME></STMTTRN>
</BANKTRANLIST>
</STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>`
	// European decimal comma is not valid OFX, but banks do emit it; the
	// tolerant scanner must at least not lose the transaction.
	st, err := ParseOFX([]byte(data), FormatOFX, "mar.ofx")
	if err != nil {
		t.Fatalf("ParseOFX: %v", err)
	}
	if len(st.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(st.Lines))
	}
	if st.Lines[0].Payee != "Cafe" {
		t.Errorf("payee = %q", st.Lines[0].Payee)
	}
	if !st.Lines[0].Amount.Equal(decimal.RequireFromString("-10.50")) {
		t.Errorf("amount = %s, want -10.50", st.Lines[0].Amount)
	}
}

func TestParseQIF(t *testing.T) {
	data := "!Type:Bank\r\n" +
		"D01/15/2026\r\n" +
		"T-42.50\r\n" +
		"PCOFFEE SHOP\r\n" +
		"MCard ending 1234\r\n" +
		"N1234\r\n" +
		"^\r\n" +
		"D01/20/2026\r\n" +
		"T1500.00\r\n" +
		"PACME PAYROLL\r\n" +
		"LIncome\r\n" +
		"^\r\n"
	st, err := ParseQIF([]byte(data))
	if err != nil {
		t.Fatalf("ParseQIF: %v", err)
	}
	if len(st.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(st.Lines))
	}
	l := st.Lines[0]
	if l.Date.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("date = %s", l.Date)
	}
	if !l.Amount.Equal(decimal.RequireFromString("-42.50")) {
		t.Errorf("amount = %s", l.Amount)
	}
	if l.Payee != "COFFEE SHOP" || l.Description != "Card ending 1234" {
		t.Errorf("payee/description = %q / %q", l.Payee, l.Description)
	}
	if l.ChequeNumber != "1234" {
		t.Errorf("cheque = %q", l.ChequeNumber)
	}
	if !st.Lines[1].Amount.Equal(decimal.RequireFromString("1500.00")) {
		t.Errorf("second amount = %s", st.Lines[1].Amount)
	}
}

func TestParseQIFWithoutTrailingCaretAndAccountBlock(t *testing.T) {
	data := "!Account\nNEveryday Checking\nTBank\n^\n" +
		"!Type:Bank\n" +
		"D02/03/2026\nT-9.99\nPSUBSCRIPTION\n"
	st, err := ParseQIF([]byte(data))
	if err != nil {
		t.Fatalf("ParseQIF: %v", err)
	}
	if st.AccountID != "Everyday Checking" {
		t.Errorf("account = %q", st.AccountID)
	}
	if len(st.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(st.Lines))
	}
	if st.Lines[0].Date.Format("2006-01-02") != "2026-02-03" {
		t.Errorf("date = %s", st.Lines[0].Date)
	}
}

func TestParseCSVDetectsDelimiterHeaderAndMapping(t *testing.T) {
	data := "Date;Description;Amount;Balance\n" +
		"15/01/2026;Coffee shop;-42,50;1000,00\n" +
		"20/01/2026;Payroll;1500,00;2500,00\n"
	st, err := ParseCSV([]byte(data))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if st.Delimiter != ";" {
		t.Fatalf("delimiter = %q, want ;", st.Delimiter)
	}
	if !st.HasHeader {
		t.Fatal("expected a header row")
	}
	m := DetectMapping(st.Columns)
	if m.Date != "Date" || m.Amount != "Amount" || m.Balance != "Balance" || m.Description != "Description" {
		t.Fatalf("mapping = %+v", m)
	}
	lines, err := ApplyCSV(st, m)
	if err != nil {
		t.Fatalf("ApplyCSV: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// Ambiguous 15/01 can only be 15 January; the whole file must agree.
	if lines[0].Date.Format("2006-01-02") != "2026-01-15" {
		t.Errorf("date = %s", lines[0].Date)
	}
	if !lines[0].Amount.Equal(decimal.RequireFromString("-42.5")) {
		t.Errorf("amount = %s, want -42.5", lines[0].Amount)
	}
	// A column literally headed "Description" is the description, not the payee.
	if lines[0].Description != "Coffee shop" {
		t.Errorf("description = %q, want %q", lines[0].Description, "Coffee shop")
	}
	if lines[0].Payee != "" {
		t.Errorf("payee = %q, want empty (no payee column)", lines[0].Payee)
	}
	if st.Closing == nil || !st.Closing.Equal(decimal.RequireFromString("2500")) {
		t.Errorf("closing = %v", st.Closing)
	}
}

func TestApplyCSVDebitCreditColumns(t *testing.T) {
	data := "Date,Description,Withdrawals,Deposits\n" +
		"2026-01-15,Rent,2000.00,\n" +
		"2026-01-20,Client payment,,3500.00\n"
	st, err := ParseCSV([]byte(data))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	m := DetectMapping(st.Columns)
	if m.AmountMode != "DEBIT_CREDIT" {
		t.Fatalf("amount mode = %q, want DEBIT_CREDIT", m.AmountMode)
	}
	lines, err := ApplyCSV(st, m)
	if err != nil {
		t.Fatalf("ApplyCSV: %v", err)
	}
	if !lines[0].Amount.Equal(decimal.RequireFromString("-2000")) {
		t.Errorf("withdrawal = %s, want -2000", lines[0].Amount)
	}
	if !lines[1].Amount.Equal(decimal.RequireFromString("3500")) {
		t.Errorf("deposit = %s, want 3500", lines[1].Amount)
	}
}

func TestApplyCSVWithoutHeader(t *testing.T) {
	data := "2026-01-15,-42.50,COFFEE SHOP\n2026-01-20,1500.00,ACME\n"
	st, err := ParseCSV([]byte(data))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if st.HasHeader {
		t.Fatal("did not expect a header row")
	}
	// A headerless file gives numeric column refs; the wizard's user picks
	// them, and ApplyCSV must resolve them positionally.
	m := Mapping{AmountMode: "SIGNED", Date: "0", Amount: "1", Payee: "2"}
	lines, err := ApplyCSV(st, m)
	if err != nil {
		t.Fatalf("ApplyCSV: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if lines[0].Payee != "COFFEE SHOP" {
		t.Errorf("payee = %q", lines[0].Payee)
	}
}

func TestApplyCSVRowErrorNamesTheRow(t *testing.T) {
	data := "Date,Amount\n2026-01-15,-1.00\nnot-a-date,-2.00\n"
	st, _ := ParseCSV([]byte(data))
	_, err := ApplyCSV(st, Mapping{AmountMode: "SIGNED", Date: "Date", Amount: "Amount"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); got != "row 2: unparseable date: \"not-a-date\"" {
		t.Errorf("error = %q", got)
	}
}

func TestDetectDateFormatPrefersUSWhenAmbiguous(t *testing.T) {
	// 01/02 is both 1 Feb (US) and 2 Jan (UK). Xero, QIF and QFX all use the
	// US order, and the file must be read consistently one way.
	if got := detectDateFormat([]string{"01/02/2026", "03/04/2026"}, ""); got != "01/02/2006" {
		t.Errorf("layout = %q, want 01/02/2006", got)
	}
	// An unambiguous sample pins the layout regardless.
	if got := detectDateFormat([]string{"15/01/2026"}, ""); got != "02/01/2006" {
		t.Errorf("layout = %q, want 02/01/2006", got)
	}
}

func TestParseAmount(t *testing.T) {
	cases := []struct{ in, sep, want string }{
		{"1234.56", ".", "1234.56"},
		{"$1,234.56", ".", "1234.56"},
		{"(1,234.56)", ".", "-1234.56"},
		{"1234.56-", ".", "-1234.56"},
		{"-1234.56", ".", "-1234.56"},
		{"+50", ".", "50"},
		{"1.234,56", ",", "1234.56"},
		{"1.234,56", "", "1234.56"},
		{"1,234", "", "1234"},
		{"1,50", "", "1.5"},
		{"", "", ""},
	}
	for _, tc := range cases {
		got, err := parseAmount(tc.in, tc.sep)
		if tc.want == "" {
			if err == nil {
				t.Errorf("parseAmount(%q) = %s, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAmount(%q): %v", tc.in, err)
			continue
		}
		if !got.Equal(decimal.RequireFromString(tc.want)) {
			t.Errorf("parseAmount(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestParseCSVXeroTemplateKeepsPayeeAndDescription(t *testing.T) {
	data := "*Date,*Amount,Payee,Description,Reference,Check Number\n" +
		"2026-01-15,-42.50,Coffee Shop,Card purchase 1234,INV-1,1001\n"
	st, err := ParseCSV([]byte(data))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	m := DetectMapping(st.Columns)
	if m.Payee != "Payee" || m.Description != "Description" {
		t.Fatalf("mapping = %+v", m)
	}
	lines, err := ApplyCSV(st, m)
	if err != nil {
		t.Fatalf("ApplyCSV: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0].Payee != "Coffee Shop" {
		t.Errorf("payee = %q, want %q", lines[0].Payee, "Coffee Shop")
	}
	if lines[0].Description != "Card purchase 1234" {
		t.Errorf("description = %q", lines[0].Description)
	}
	if lines[0].ChequeNumber != "1001" {
		t.Errorf("cheque = %q", lines[0].ChequeNumber)
	}
}

// Xero's "Don't import the first line because they're column headings" sets
// SkipRows=1; on a file whose header was already detected this must not drop a
// real data row.
func TestApplyCSVSkipRowsDoesNotEatHeaderData(t *testing.T) {
	data := "Date,Amount\n2026-01-15,-1.00\n2026-01-16,-2.00\n"
	st, err := ParseCSV([]byte(data))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	m := Mapping{HasHeader: true, SkipRows: 1, AmountMode: "SIGNED", Date: "Date", Amount: "Amount"}
	lines, err := ApplyCSV(st, m)
	if err != nil {
		t.Fatalf("ApplyCSV: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
}

func TestParseDetectsQBOByExtension(t *testing.T) {
	data := "OFXHEADER:100\n<INTU.BID>1234\n<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS>" +
		"<BANKTRANLIST><STMTTRN><DTPOSTED>20260115</DTPOSTED><TRNAMT>-5.00</TRNAMT><NAME>x</NAME></STMTTRN>" +
		"</BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>\n"
	got, ok := DetectFormat("statement.qbo", []byte(data))
	if !ok || got != FormatQBO {
		t.Fatalf("format = %q, ok=%v, want QBO", got, ok)
	}
}

// A statement's balance column is the balance after each line, so the balance
// the period opened with is the first line's balance less that line's amount.
func TestOpeningBalanceIsDerivedFromFirstLine(t *testing.T) {
	after := decimal.RequireFromString("988.00")
	got := OpeningBalance([]Line{{Amount: decimal.RequireFromString("-12.34"), Balance: &after}})
	if got == nil || !got.Equal(decimal.RequireFromString("1000.34")) {
		t.Fatalf("opening = %v, want 1000.34", got)
	}

	if OpeningBalance(nil) != nil {
		t.Error("an empty statement has no opening balance")
	}
	if OpeningBalance([]Line{{Amount: decimal.NewFromInt(1)}}) != nil {
		t.Error("a file with no balance column has no opening balance")
	}
}
