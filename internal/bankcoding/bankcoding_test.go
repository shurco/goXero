package bankcoding

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(day int) time.Time { return time.Date(2026, 8, day, 0, 0, 0, 0, time.UTC) }

func TestKeyIgnoresPunctuationAndCase(t *testing.T) {
	// The same bank changes its spacing and punctuation between exports, so
	// those differences must not look like a different counterparty.
	assert.Equal(t, Key("SMART Agency 01950210"), Key("Smart  agency-01950210"))
	assert.Equal(t, Key("7-Eleven"), Key("7 eleven"))
	assert.Equal(t, "7eleven", Key("7-Eleven"))
	// Different counterparties must stay apart.
	assert.NotEqual(t, Key("City Limousines"), Key("City Council"))
	// Nothing to match on.
	assert.Equal(t, "", Key("  --  "))
}

func TestSuggestUsesTheMostRecentEntry(t *testing.T) {
	contact := uuid.New()
	h := NewHistory([]Entry{
		{Payee: "Ridgeway Bank", UsedAt: at(1), AccountCode: "408", TaxType: "NONE", Description: "Bank fee"},
		{Payee: "Ridgeway Bank", UsedAt: at(20), AccountCode: "404", TaxType: "NONE",
			Description: "Bank fee", Reference: "Acct fee", ContactID: &contact, ContactName: "Ridgeway Bank"},
	})

	s := h.Suggest("Ridgeway  BANK", "")
	require.NotNil(t, s)
	assert.Equal(t, "404", s.AccountCode, "the last time is what we suggest")
	assert.Equal(t, "NONE", s.TaxType)
	assert.Equal(t, "Acct fee", s.Reference)
	assert.Equal(t, &contact, s.ContactID)
	assert.Equal(t, 2, s.MatchCount)
	require.NotNil(t, s.LastUsedAt)
	assert.Equal(t, at(20), *s.LastUsedAt)
}

func TestSuggestFallsBackToTheNewestEntryThatNamesAnAccount(t *testing.T) {
	// An entry with no account code has nothing to teach, but it still counts
	// as a previous entry.
	h := NewHistory([]Entry{
		{Payee: "Truxton", UsedAt: at(1), AccountCode: "461"},
		{Payee: "Truxton", UsedAt: at(5)},
	})

	s := h.Suggest("Truxton", "")
	require.NotNil(t, s)
	assert.Equal(t, "461", s.AccountCode)
	assert.Equal(t, 2, s.MatchCount)
	assert.Equal(t, at(1), *s.LastUsedAt, "the date is the entry the coding came from")
}

func TestSuggestHasNothingToSayForANewPayee(t *testing.T) {
	h := NewHistory([]Entry{{Payee: "Ridgeway Bank", UsedAt: at(1), AccountCode: "404"}})
	assert.Nil(t, h.Suggest("Somewhere New", ""))
	assert.Nil(t, h.Suggest("", ""), "a line with no payee and no reference cannot be matched")
	assert.Nil(t, NewHistory(nil).Suggest("Ridgeway Bank", ""))
}

func TestSuggestFallsBackToTheReference(t *testing.T) {
	h := NewHistory([]Entry{{Reference: "INV-0025", UsedAt: at(1), AccountCode: "200"}})
	s := h.Suggest("", "inv 0025")
	require.NotNil(t, s)
	assert.Equal(t, "200", s.AccountCode)
}

func TestEntryWithAPayeeIsNotFiledUnderItsReference(t *testing.T) {
	// Otherwise an unrelated line quoting the same reference — an invoice
	// number says nothing about who was paid — would match it.
	h := NewHistory([]Entry{{Payee: "Ridgeway University", Reference: "INV-0025", UsedAt: at(1), AccountCode: "200"}})
	assert.Nil(t, h.Suggest("", "INV-0025"))
	assert.NotNil(t, h.Suggest("Ridgeway University", ""))
}

func TestSuggestForDescriptionFindsWhereTheLastOneWent(t *testing.T) {
	// A bank fee has no counterparty to match on, so the payee index cannot
	// answer for it. The description can: it is the one thing the next bank fee
	// will share with the last one.
	h := NewHistory([]Entry{
		{UsedAt: at(1), AccountCode: "404", Description: "Bank fee"},
		{UsedAt: at(20), AccountCode: "429", Description: "BANK  FEE"},
		{UsedAt: at(25), AccountCode: "400", Description: "Minor adjustment"},
	})

	e := h.SuggestForDescription("Bank fee")
	require.NotNil(t, e)
	assert.Equal(t, "429", e.AccountCode, "the most recent is what we follow")
	assert.Equal(t, "400", h.SuggestForDescription("minor  adjustment").AccountCode)
	// A description these books have never used has nothing to offer.
	assert.Nil(t, h.SuggestForDescription("Bank charges"))
	assert.Nil(t, h.SuggestForDescription("  --  "))
}

func TestSuggestForDescriptionSkipsEntriesWithNothingToFollow(t *testing.T) {
	// An entry that was never coded, and one coded without a description, are
	// both useless as a precedent -- and neither may hide an older entry that
	// does answer the question.
	h := NewHistory([]Entry{
		{UsedAt: at(1), AccountCode: "404", Description: "Bank fee"},
		{UsedAt: at(10), AccountCode: "", Description: "Bank fee"},
		{UsedAt: at(20), AccountCode: "429", Description: ""},
	})

	e := h.SuggestForDescription("Bank fee")
	require.NotNil(t, e)
	assert.Equal(t, "404", e.AccountCode)
}
