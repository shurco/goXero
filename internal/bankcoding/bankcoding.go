// Package bankcoding answers the question a bookkeeper asks on every statement
// line that has been seen before: "how did we code this last time?".
//
// It is the engine behind Xero's "Suggest previous entries" setting. Given the
// coding history of one bank account, it finds the entries that share the
// line's payee and returns the most recent of them as an advisory suggestion.
//
// The matching is deliberately exact rather than fuzzy: a wrong suggestion is
// worse than none, because it is silently accepted far more often than it is
// checked. Two payee strings count as the same counterparty when they are equal
// once everything but letters and digits has been removed — punctuation,
// spacing and letter case vary between bank exports and the same bank's next
// export, while "City Limousines" and "City Council" stay apart.
package bankcoding

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/models"
)

// Entry is one previously coded entry on a bank account — a statement line that
// has been reconciled, or a transaction entered straight into Xero. Both are
// "previous entries" to the person looking at the next statement line.
type Entry struct {
	// Payee is the payee text the entry is remembered by: the bank's own payee
	// on a reconciled statement line, otherwise the contact name, otherwise the
	// transaction reference.
	Payee       string
	Reference   string
	UsedAt      time.Time
	ContactID   *uuid.UUID
	ContactName string
	AccountCode string
	AccountID   *uuid.UUID
	AccountName string
	TaxType     string
	Description string
}

// History is a bank account's coding history, indexed by payee so a whole page
// of statement lines can be looked up without rescanning it per line.
type History struct {
	byKey map[string][]Entry
	// ordered is the same entries in the order they were handed over --
	// oldest first -- which is what a search by anything other than payee
	// walks, and what makes "the most recent match" the last one found.
	ordered []Entry
}

// NewHistory indexes the entries. The caller is expected to have sorted them
// oldest-first, which is what makes "the most recent match" cheap to find.
func NewHistory(entries []Entry) *History {
	h := &History{byKey: make(map[string][]Entry, len(entries)), ordered: entries}
	for _, e := range entries {
		for _, k := range entryKeys(e) {
			h.byKey[k] = append(h.byKey[k], e)
		}
	}
	return h
}

// entryKeys are the keys an entry can be found by. An entry with a payee is
// filed under it alone: filing it under its reference as well would make an
// unrelated line that happens to quote the same reference match it.
func entryKeys(e Entry) []string {
	if k := Key(e.Payee); k != "" {
		return []string{k}
	}
	if k := Key(e.Reference); k != "" {
		return []string{k}
	}
	return nil
}

// Suggest returns the coding the account used the last time it saw this payee,
// or nil when it has not seen it before. The most recent entry that actually
// names an account wins; older ones only count towards MatchCount.
func (h *History) Suggest(payee, reference string) *models.PreviousEntrySuggestion {
	key := Key(payee)
	if key == "" {
		// Plenty of bank exports leave the payee blank and put the only usable
		// text in the reference, so that is the fallback key.
		key = Key(reference)
	}
	if key == "" {
		return nil
	}
	matches := h.byKey[key]
	if len(matches) == 0 {
		return nil
	}

	s := &models.PreviousEntrySuggestion{Payee: firstNonBlank(matches[len(matches)-1].Payee, payee)}
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i].AccountCode == "" {
			continue
		}
		e := matches[i]
		used := e.UsedAt
		s.MatchCount = len(matches)
		s.LastUsedAt = &used
		s.ContactID = e.ContactID
		s.ContactName = e.ContactName
		s.AccountCode = e.AccountCode
		s.AccountID = e.AccountID
		s.AccountName = e.AccountName
		s.TaxType = e.TaxType
		s.Description = e.Description
		s.Reference = e.Reference
		return s
	}
	return nil
}

// SuggestForDescription returns the entry this organisation coded most recently
// under a given description, or nil when its books have never carried one. The
// caller uses it to answer "where did we put this last time?" for an entry that
// has no counterparty to match on -- a bank fee has no payee, but the books say
// where the last one went.
//
// The description is compared the same way a payee is: letters and digits only,
// folded to lower case. Exact rather than fuzzy, for the same reason -- "Bank
// fee" and "Bank fee refund" are different things and a wrong default is
// accepted far more often than it is checked. An entry with no description, or
// none that names an account, is skipped; older entries still count.
func (h *History) SuggestForDescription(description string) *Entry {
	key := Key(description)
	if key == "" {
		return nil
	}
	for i := len(h.ordered) - 1; i >= 0; i-- {
		e := h.ordered[i]
		if e.AccountCode != "" && Key(e.Description) == key {
			return &e
		}
	}
	return nil
}

// Key normalises payee or reference text to the form the history is indexed by:
// letters and digits only, folded to lower case. An empty result means the text
// carries nothing to match on.
func Key(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
