// Package bankstatement parses bank statement files into a common set of
// statement lines.
//
// It covers the formats Xero's "Import bank statement" wizard accepts —
// OFX (recommended), QFX, QBO, QIF and CSV — so the same import pipeline can
// serve every bank. Nothing here touches the database or the HTTP layer: the
// package turns bytes into []Line plus, for CSV, the raw grid the wizard needs
// to render its column-mapping step.
package bankstatement

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ErrUnsupportedFormat is returned by Parse for anything we cannot read.
var ErrUnsupportedFormat = errors.New("unsupported statement format")

// Supported formats, also the values stored in bank_statement_imports.format.
const (
	FormatCSV = "CSV"
	FormatOFX = "OFX"
	FormatQFX = "QFX"
	FormatQBO = "QBO"
	FormatQIF = "QIF"
)

// Line is one transaction read from a statement file.
type Line struct {
	Date         time.Time        `json:"Date"`
	Amount       decimal.Decimal  `json:"Amount"` // signed: positive = money in
	Payee        string           `json:"Payee,omitempty"`
	Description  string           `json:"Description,omitempty"`
	Reference    string           `json:"Reference,omitempty"`
	ChequeNumber string           `json:"ChequeNumber,omitempty"`
	Balance      *decimal.Decimal `json:"Balance,omitempty"`
	ProviderTxID string           `json:"ProviderTxID,omitempty"` // FITID for OFX, "" otherwise
}

// Statement is a parsed statement file.
type Statement struct {
	Format    string           `json:"Format"`
	Currency  string           `json:"Currency,omitempty"`
	AccountID string           `json:"AccountID,omitempty"`
	Start     *time.Time       `json:"Start,omitempty"`
	End       *time.Time       `json:"End,omitempty"`
	Closing   *decimal.Decimal `json:"Closing,omitempty"`
	Lines     []Line           `json:"Lines"`

	// CSV-only fields, populated so the wizard can render the mapping step and
	// re-apply a user-chosen mapping without the original file.
	Columns   []string   `json:"Columns,omitempty"`
	Rows      [][]string `json:"Rows,omitempty"`
	HasHeader bool       `json:"HasHeader,omitempty"`
	Delimiter string     `json:"Delimiter,omitempty"`
}

// Parse sniffs the format of `data` (using `filename` only as a hint) and
// parses it. CSV is returned with its raw grid so a caller can ask the user to
// confirm the column mapping; the other formats are fully parsed.
func Parse(filename string, data []byte) (*Statement, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, errors.New("statement file is empty")
	}
	if format, ok := DetectFormat(filename, data); ok {
		return ParseAs(format, filename, data)
	}
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filename), "."))
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, firstNonEmpty(ext, "unknown"))
}

// DetectFormat reports the format of a statement file, using its contents
// first and its extension only as a fallback — banks routinely mislabel their
// exports, and a `.csv` holding OFX is common. The wizard shows this to the
// user as the format it guessed before they confirm or override it.
func DetectFormat(filename string, data []byte) (string, bool) {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(filename), "."))
	return sniff(data, ext)
}

// ParseAs parses `data` as `format`, bypassing detection. Used when the wizard
// re-runs the parse after the user picks a format manually.
func ParseAs(format, filename string, data []byte) (*Statement, error) {
	switch strings.ToUpper(format) {
	case FormatCSV:
		return ParseCSV(data)
	case FormatOFX, FormatQFX, FormatQBO:
		return ParseOFX(data, strings.ToUpper(format), filename)
	case FormatQIF:
		return ParseQIF(data)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
}

// sniff decides the format from the file contents, falling back to the
// extension. Content wins because banks routinely mislabel their exports.
func sniff(data []byte, ext string) (string, bool) {
	head := strings.ToUpper(string(data[:min(len(data), 4096)]))
	switch {
	case strings.Contains(head, "OFXHEADER") || strings.Contains(head, "<OFX>"):
		// QFX and QBO are both OFX with an Intuit header and are otherwise
		// identical, so the extension decides between them.
		if ext == FormatQBO {
			return FormatQBO, true
		}
		if strings.Contains(head, "INTU.BID") || strings.Contains(head, "QFXHEADER") ||
			strings.Contains(head, "<INTU.BID>") {
			return FormatQFX, true
		}
		return FormatOFX, true
	case strings.HasPrefix(strings.TrimSpace(string(data)), "!Type:") ||
		strings.Contains(head, "\n!TYPE:"):
		return FormatQIF, true
	}
	switch ext {
	case FormatCSV, FormatOFX, FormatQFX, FormatQBO, FormatQIF:
		return ext, true
	case "TXT":
		return FormatCSV, true
	}
	// Anything else that looks like delimited text is treated as CSV; the
	// wizard lets the user override the guess.
	if strings.ContainsAny(firstLine(string(data)), ",;\t|") {
		return FormatCSV, true
	}
	return "", false
}

// OpeningBalance returns the balance the statement period opened with, derived
// from the running balance printed against the first line. A statement's balance
// column is the balance *after* each line, so subtracting that line's amount
// gives what the account held before it — and it needs no assumption about the
// order the file lists its lines in. Nil means the file carried no balance.
func OpeningBalance(lines []Line) *decimal.Decimal {
	if len(lines) == 0 || lines[0].Balance == nil {
		return nil
	}
	opening := lines[0].Balance.Sub(lines[0].Amount)
	return &opening
}

// Fingerprint is the duplicate-detection key for a manually imported line.
// It deliberately mirrors what Xero compares when it warns about duplicates:
// the account, the date, the amount and the payee. Two genuinely identical
// coffees on one day therefore collide — which is why callers treat a match as
// a warning to skip rather than a hard unique constraint.
func Fingerprint(bankAccountID string, l Line) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s", bankAccountID,
		l.Date.UTC().Format("2006-01-02"),
		l.Amount.StringFixed(4),
		normalise(l.Payee),
		normalise(l.Reference))
	return hex.EncodeToString(h.Sum(nil))
}

func normalise(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
