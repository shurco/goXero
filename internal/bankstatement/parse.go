package bankstatement

import (
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// currencySymbols are stripped from amount cells before parsing. Banks emit
// everything from "$1,234.56" to "£1.234,56-" and we need all of it to work.
const currencySymbols = "$€£¥₹R$USDGBPEUR  "

var errUnparseableDate = errors.New("unparseable date")
var errUnparseableAmount = errors.New("unparseable amount")

// dateLayouts are tried in order. Unambiguous ISO layouts come first; the
// ambiguous DD/MM vs MM/DD pair is resolved by detectDateFormat over the whole
// file rather than per value, so a file can never be read half one way and
// half the other.
var dateLayouts = []string{
	"2006-01-02",
	"2006/01/02",
	"20060102",
	"02.01.2006",
	"2.1.2006",
	// The ambiguous slash pair is ordered US-first: with equal coverage the
	// earlier layout wins, and Xero, QIF and QFX exports are all US-ordered.
	"01/02/2006",
	"02/01/2006",
	"1/2/2006",
	"2/1/2006",
	"01-02-2006",
	"02-01-2006",
	"1-2-2006",
	"2-1-2006",
	"01/02/06",
	"02/01/06",
	"1/2/06",
	"2/1/06",
	"2 Jan 2006",
	"02 Jan 2006",
	"Jan 2, 2006",
	"January 2, 2006",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
}

// ambiguousLayouts are the two layouts whose meaning depends on convention.
// When a file parses under both we prefer the US order, which is what Xero,
// QIF and QFX exports use.
var ambiguousLayouts = []string{"01/02/2006", "02/01/2006", "1/2/2006", "2/1/2006", "01/02/06", "02/01/06"}

// detectDateFormat picks a single layout for the whole file. `samples` are the
// raw date cells; `explicit` (when non-empty) short-circuits detection so a
// user-chosen format from the wizard is honoured verbatim.
//
// A layout is accepted when it parses every sample after normalising
// single-digit day/month forms.
func detectDateFormat(samples []string, explicit string) string {
	if explicit != "" {
		return explicit
	}
	cleaned := make([]string, 0, len(samples))
	for _, s := range samples {
		s = strings.TrimSpace(s)
		if s != "" {
			cleaned = append(cleaned, s)
		}
	}
	if len(cleaned) == 0 {
		return dateLayouts[0]
	}
	best, bestHits := "", 0
	for _, layout := range dateLayouts {
		hits := 0
		for _, s := range cleaned {
			if _, err := parseWithLayout(s, layout); err == nil {
				hits++
			}
		}
		// Prefer full coverage, then the earliest layout in preference order.
		if hits > bestHits {
			best, bestHits = layout, hits
		}
		if hits == len(cleaned) && !isAmbiguous(layout) {
			return layout
		}
	}
	if bestHits == 0 {
		return dateLayouts[0]
	}
	return best
}

func isAmbiguous(layout string) bool {
	for _, a := range ambiguousLayouts {
		if a == layout {
			return true
		}
	}
	return false
}

// parseDate parses one date cell with the detected layout, falling back to a
// best-effort sweep so a single odd row doesn't abort the whole import.
func parseDate(s, layout string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errUnparseableDate
	}
	if t, err := parseWithLayout(s, layout); err == nil {
		return t, nil
	}
	for _, l := range dateLayouts {
		if t, err := parseWithLayout(s, l); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errUnparseableDate
}

// parseWithLayout parses with `layout`, also trying the non-padded variant so
// a padded layout ("01/02/2006") still reads an unpadded value ("1/2/2026").
// Go's non-padded layouts already accept padded input, so the reverse mapping
// is neither needed nor safe — it corrupts the year token.
func parseWithLayout(s, layout string) (time.Time, error) {
	if t, err := time.Parse(layout, s); err == nil {
		return truncateToDay(t), nil
	}
	alt := strings.NewReplacer("01", "1", "02", "2").Replace(layout)
	if alt != layout {
		if t, err := time.Parse(alt, s); err == nil {
			return truncateToDay(t), nil
		}
	}
	return time.Time{}, errUnparseableDate
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// parseAmount reads a money cell. `decimalSep` is the separator the user (or
// the format) declared — "." for the OFX/QIF/English-CSV world, "," for
// European CSV exports. Detection is applied when it is left empty.
func parseAmount(s, decimalSep string) (decimal.Decimal, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return decimal.Zero, errUnparseableAmount
	}
	// Accounting notation: (1,234.56) and 1.234,56- both mean negative.
	negative := false
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}} {
		if strings.HasPrefix(raw, pair[0]) && strings.HasSuffix(raw, pair[1]) {
			negative = true
			raw = strings.TrimSuffix(strings.TrimPrefix(raw, pair[0]), pair[1])
		}
	}
	if strings.HasSuffix(raw, "-") {
		negative = true
		raw = strings.TrimSuffix(raw, "-")
	}
	if strings.HasPrefix(raw, "-") {
		negative = true
		raw = strings.TrimPrefix(raw, "-")
	}
	if strings.HasPrefix(raw, "+") {
		raw = strings.TrimPrefix(raw, "+")
	}
	raw = strings.Trim(raw, " \t")
	raw = strings.Map(func(r rune) rune {
		if strings.ContainsRune(currencySymbols, r) {
			return -1
		}
		return r
	}, raw)
	raw = strings.ReplaceAll(raw, " ", "")
	if raw == "" {
		return decimal.Zero, errUnparseableAmount
	}

	sep := decimalSep
	if sep == "" {
		sep = detectDecimalSeparator(raw)
	}
	switch sep {
	case ",":
		raw = strings.ReplaceAll(raw, ".", "") // "." is the thousands separator
		raw = strings.ReplaceAll(raw, ",", ".")
	default:
		raw = strings.ReplaceAll(raw, ",", "")
	}
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return decimal.Zero, errUnparseableAmount
	}
	if negative {
		d = d.Neg()
	}
	return d, nil
}

// detectDecimalSeparator infers which of "." / "," is the decimal separator in
// a single amount cell. The last separator wins when both appear; a lone ","
// is only decimal when it is followed by exactly two digits, so "1,234" stays
// one thousand two hundred thirty-four.
func detectDecimalSeparator(raw string) string {
	lastDot := strings.LastIndex(raw, ".")
	lastComma := strings.LastIndex(raw, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot {
			return ","
		}
		return "."
	case lastComma >= 0:
		tail := raw[lastComma+1:]
		if len(tail) == 2 && allDigits(tail) {
			return ","
		}
		return "."
	default:
		return "."
	}
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ofxDate parses OFX's `YYYYMMDD[HHMMSS][.sss][gmt offset:tz]` date form.
func ofxDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errUnparseableDate
	}
	if i := strings.IndexAny(s, "[+-"); i > 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	for _, layout := range []string{"20060102150405", "200601021504", "20060102"} {
		if t, err := time.Parse(layout, s); err == nil {
			return truncateToDay(t), nil
		}
	}
	return time.Time{}, errUnparseableDate
}
