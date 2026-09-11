package bankstatement

import (
	"fmt"
	"strings"
)

// ParseQIF reads Quicken Interchange Format files. QIF records are terminated
// by a lone caret and each line is a single-letter code followed by its value:
//
//	!Type:Bank
//	D01/15/2026
//	T-42.50
//	PCOFFEE SHOP
//	MCard ending 1234
//	N1234
//	^
//
// QIF has three quirks the other formats do not: the file can hold several
// account sections, amounts are already signed (negative = money out), and
// the date order is whatever the exporting Quicken was configured for — so the
// layout is detected once across the whole file, exactly as for CSV.
func ParseQIF(data []byte) (*Statement, error) {
	text := strings.ReplaceAll(string(stripBOM(data)), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	st := &Statement{Format: FormatQIF}
	var raw []rawQIF

	// A line-based state machine rather than a split on "^": real QIF files
	// omit the final caret, indent records inconsistently, and interleave
	// `!Account` blocks with the `!Type:` sections.
	var (
		cur          map[byte]string
		accountBlock bool
	)
	flush := func() {
		if cur == nil {
			return
		}
		fields := cur
		cur = nil
		if accountBlock {
			// An `!Account` block describes the account, not a transaction.
			accountBlock = false
			if st.AccountID == "" {
				st.AccountID = strings.TrimSpace(fields['N'])
			}
			return
		}
		if fields['D'] == "" || fields['T'] == "" {
			return
		}
		raw = append(raw, rawQIF{
			date:   fields['D'],
			amount: fields['T'],
			payee:  fields['P'],
			memo:   fields['M'],
			number: fields['N'],
		})
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		if line == "^" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "!") {
			flush()
			if strings.HasPrefix(strings.ToUpper(line), "!ACCOUNT") {
				accountBlock = true
			}
			continue
		}
		code := line[0]
		if code < 'A' || code > 'Z' {
			continue
		}
		if cur == nil {
			cur = map[byte]string{}
		}
		// First occurrence wins: repeated `S`/`E`/`$` split lines would
		// otherwise overwrite the memo-derived fields we care about.
		if _, seen := cur[code]; !seen {
			cur[code] = strings.TrimSpace(line[1:])
		}
	}
	flush()

	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: no QIF transactions found", ErrUnsupportedFormat)
	}

	samples := make([]string, 0, len(raw))
	for _, r := range raw {
		samples = append(samples, r.date)
	}
	layout := detectDateFormat(samples, "")

	for i, r := range raw {
		d, err := parseDate(r.date, layout)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w: %q", i+1, errUnparseableDate, r.date)
		}
		amount, err := parseAmount(r.amount, "")
		if err != nil {
			return nil, fmt.Errorf("record %d: %w: %q", i+1, errUnparseableAmount, r.amount)
		}
		l := Line{
			Date:        d,
			Amount:      amount,
			Payee:       r.payee,
			Description: r.memo,
			Reference:   r.number,
		}
		if l.Reference != "" && allDigits(l.Reference) {
			l.ChequeNumber = l.Reference
		}
		st.Lines = append(st.Lines, l)
	}
	applyStatementBounds(st, st.Lines)
	return st, nil
}

// rawQIF is one unparsed record, kept so the date layout can be detected over
// the whole file before anything is converted.
type rawQIF struct {
	date   string
	amount string
	payee  string
	memo   string
	number string
}
