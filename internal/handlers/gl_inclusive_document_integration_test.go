package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tax on the documents below is 6 on a line figure of 60: the 60 already
// contains it, which is what LineAmountTypes "Inclusive" means on the wire.
const (
	inclusiveLineAmount = "60"
	inclusiveTaxAmount  = "6"
)

// Authorising a document whose lines carry tax-inclusive amounts used to fail
// with "post gl journal: unbalanced journal". The document total already
// contains the tax, so posting each line at its full amount *and* a separate tax
// line counted the tax twice and left the journal off by exactly the tax — a
// 500 that rejected the whole document and saved nothing.
//
// This is the end-to-end regression test for the split that fixes it: the
// control account carries the document total, the coded line carries the net
// (total − tax), and the tax account carries the tax. Each is asserted by its
// own account code, and the three are asserted to sum to zero — which is the
// condition insertJournal enforces and the reason the old arithmetic failed.
func TestHTTP_TaxInclusiveDocumentsAuthoriseAndBalance(t *testing.T) {
	h := newHarness(t)
	contactID := createTradingContact(t, h)

	cases := []struct {
		what     string
		path     string
		key      string // the envelope key the create response uses
		idField  string // the id field inside it
		docType  string
		source   string // gl_journals.source_type
		lineCode string // the account the line was coded to
		taxType  string
		// wantArithmetic is the journal by account code, signed the way the
		// posting writes it: a sale debits the control account, a bill and a
		// credit note credit it.
		want map[string]string
	}{
		{
			what: "a sales invoice", path: "/api/v1/invoices", key: "Invoices",
			idField: "InvoiceID", docType: "ACCREC", source: "INVOICE",
			lineCode: "200", taxType: "OUTPUT",
			want: map[string]string{"610": "60", "200": "-54", "820": "-6"},
		},
		{
			what: "a purchase bill", path: "/api/v1/invoices", key: "Invoices",
			idField: "InvoiceID", docType: "ACCPAY", source: "INVOICE",
			lineCode: "400", taxType: "INPUT",
			want: map[string]string{"800": "-60", "400": "54", "820": "6"},
		},
		{
			what: "a sales credit note", path: "/api/v1/credit-notes", key: "CreditNotes",
			idField: "CreditNoteID", docType: "ACCRECCREDIT", source: "CREDITNOTE",
			lineCode: "200", taxType: "OUTPUT",
			want: map[string]string{"610": "-60", "200": "54", "820": "6"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			status, body := h.do(t, http.MethodPost, tc.path, map[string]any{
				"Type":            tc.docType,
				"Status":          "AUTHORISED",
				"ContactID":       contactID,
				"Date":            "2026-04-01T00:00:00Z",
				"DueDate":         "2026-04-30T00:00:00Z",
				"LineAmountTypes": "Inclusive",
				"LineItems": []map[string]any{{
					"Description": "Inclusive line",
					"Quantity":    "1",
					"UnitAmount":  inclusiveLineAmount,
					"AccountCode": tc.lineCode,
					"TaxType":     tc.taxType,
					"TaxAmount":   inclusiveTaxAmount,
				}},
			}, true)
			require.Equalf(t, http.StatusCreated, status, "%s was refused: %s", tc.what, body)

			doc := firstDocument(t, body, tc.key)
			docID, _ := doc[tc.idField].(string)
			require.NotEmptyf(t, docID, "no %s in %s", tc.idField, body)

			// The document itself must report the tax inside its total: 60 of
			// which 6 is tax. Asserting this alongside the journal keeps the two
			// from drifting — the journal is only balanced because they agree.
			assert.Equal(t, "60", moneyField(t, doc, "SubTotal"), "subtotal")
			assert.Equal(t, "6", moneyField(t, doc, "TotalTax"), "tax")
			assert.Equal(t, "60", moneyField(t, doc, "Total"), "total")

			journal := journalByAccountCode(t, h, tc.source, docID)
			assert.Equal(t, tc.want, journal, "journal split")
			assert.Truef(t, sumJournalLines(t, journal).IsZero(),
				"journal does not balance: %v", journal)
		})
	}
}

// "NoTax" says the document carries no tax at all, but the per-line TaxAmount
// arrives from the client independently of that choice, and the UI offers a tax
// rate on a line whose document is set to No tax. The document's own totals
// already ignore it — recalculateTotals zeroes TotalTax — so posting the line's
// tax anyway put the journal out by exactly that tax and refused the document
// with a 500.
func TestHTTP_NoTaxDocumentIgnoresLineTax(t *testing.T) {
	h := newHarness(t)
	contactID := createTradingContact(t, h)

	cases := []struct {
		what     string
		path     string
		key      string
		idField  string
		docType  string
		source   string
		lineCode string
		taxType  string
		want     map[string]string
	}{
		{
			what: "a sales invoice", path: "/api/v1/invoices", key: "Invoices",
			idField: "InvoiceID", docType: "ACCREC", source: "INVOICE",
			lineCode: "200", taxType: "OUTPUT",
			want: map[string]string{"610": "60", "200": "-60"},
		},
		{
			what: "a purchase bill", path: "/api/v1/invoices", key: "Invoices",
			idField: "InvoiceID", docType: "ACCPAY", source: "INVOICE",
			lineCode: "400", taxType: "INPUT",
			want: map[string]string{"800": "-60", "400": "60"},
		},
		{
			what: "a sales credit note", path: "/api/v1/credit-notes", key: "CreditNotes",
			idField: "CreditNoteID", docType: "ACCRECCREDIT", source: "CREDITNOTE",
			lineCode: "200", taxType: "OUTPUT",
			want: map[string]string{"610": "-60", "200": "60"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			status, body := h.do(t, http.MethodPost, tc.path, map[string]any{
				"Type":            tc.docType,
				"Status":          "AUTHORISED",
				"ContactID":       contactID,
				"Date":            "2026-04-01T00:00:00Z",
				"DueDate":         "2026-04-30T00:00:00Z",
				"LineAmountTypes": "NoTax",
				"LineItems": []map[string]any{{
					"Description": "No tax line with a rate on it",
					"Quantity":    "1",
					"UnitAmount":  inclusiveLineAmount,
					"AccountCode": tc.lineCode,
					"TaxType":     tc.taxType,
					"TaxAmount":   inclusiveTaxAmount,
				}},
			}, true)
			require.Equalf(t, http.StatusCreated, status, "%s was refused: %s", tc.what, body)

			doc := firstDocument(t, body, tc.key)
			docID, _ := doc[tc.idField].(string)
			require.NotEmptyf(t, docID, "no %s in %s", tc.idField, body)

			// The document ignores the line's tax, and so must its journal:
			// no tax leg at all, and the line carries its own figure.
			assert.Equal(t, "60", moneyField(t, doc, "Total"), "total")
			assert.Equal(t, "0", moneyField(t, doc, "TotalTax"), "tax")

			journal := journalByAccountCode(t, h, tc.source, docID)
			assert.Equal(t, tc.want, journal, "journal split")
			assert.Truef(t, sumJournalLines(t, journal).IsZero(),
				"journal does not balance: %v", journal)
		})
	}
}

// createTradingContact posts a contact that is both a customer and a supplier,
// so one contact can carry the sale, the bill and the credit note.
func createTradingContact(t *testing.T, h *appHarness) string {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/contacts", map[string]any{
		"Name":       "Inclusive documents",
		"IsCustomer": true,
		"IsSupplier": true,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var env struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmpty(t, env.Contacts)
	return env.Contacts[0].ContactID
}

// firstDocument unwraps a create response, which is `{key: [{...}]}`.
func firstDocument(t *testing.T, body []byte, key string) map[string]any {
	t.Helper()
	var env map[string][]map[string]any
	require.NoError(t, json.Unmarshal(body, &env))
	require.Lenf(t, env[key], 1, "expected exactly one %s in %s", key, body)
	return env[key][0]
}

// moneyField reads a decimal field the response carries as a JSON string.
func moneyField(t *testing.T, doc map[string]any, field string) string {
	t.Helper()
	raw, ok := doc[field].(string)
	require.Truef(t, ok, "%s is not a string: %#v", field, doc[field])
	return trimMoney(raw)
}

// journalByAccountCode returns the net amount the document posted to each
// account, keyed by the account's code.
func journalByAccountCode(t *testing.T, h *appHarness, sourceType, sourceID string) map[string]string {
	t.Helper()
	rows, err := h.repos.Pool.Query(context.Background(),
		`SELECT a.code, l.net_amount::text
		   FROM gl_journals j
		   JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		   JOIN accounts a ON a.account_id = l.account_id
		  WHERE j.organisation_id = $1 AND j.source_type = $2 AND j.source_id = $3`,
		seedDemoOrgID, sourceType, sourceID)
	require.NoError(t, err)
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var code, amount string
		require.NoError(t, rows.Scan(&code, &amount))
		out[code] = trimMoney(amount)
	}
	require.NoError(t, rows.Err())
	require.NotEmptyf(t, out, "no journal was posted for %s %s", sourceType, sourceID)
	return out
}

// sumJournalLines adds a journal up by account code — the sum insertJournal
// requires to be zero.
func sumJournalLines(t *testing.T, journal map[string]string) decimal.Decimal {
	t.Helper()
	sum := decimal.Zero
	for code, amount := range journal {
		d, err := decimal.NewFromString(amount)
		require.NoErrorf(t, err, "account %s: %q", code, amount)
		sum = sum.Add(d)
	}
	return sum
}
