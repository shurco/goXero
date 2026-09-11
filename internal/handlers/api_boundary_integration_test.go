package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cover what the accounting API does with input it cannot honour. Each
// case used to answer a 500 — blaming the server for the client's mistake and
// burying the cause in an error log — or to answer 201 with a value it had not
// saved.

// The create handlers echo the payload back, so a field they do not persist
// looks saved until the next read. auto_reconcile was exactly that: Update wrote
// it, Create dropped it, and POST answered with the value it had discarded.
func TestHTTP_AccountAutoReconcileSurvivesCreate(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"Code":          "AR-" + uuid.NewString()[:6],
		"Name":          "Auto reconcile",
		"Type":          "BANK",
		"AutoReconcile": true,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var created struct {
		Accounts []struct {
			AccountID string `json:"AccountID"`
		} `json:"Accounts"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Accounts, 1)

	// Read it back rather than trusting the echo: GET loads the row, and unlike
	// the create response it is wrapped in the APIResponse envelope.
	status, body = h.do(t, http.MethodGet, "/api/v1/accounts/"+created.Accounts[0].AccountID, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var fetched struct {
		Payload struct {
			Accounts []struct {
				AutoReconcile bool `json:"AutoReconcile"`
			} `json:"Accounts"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &fetched))
	require.Len(t, fetched.Payload.Accounts, 1)
	assert.True(t, fetched.Payload.Accounts[0].AutoReconcile, "the setting must survive the round trip")
}

// Class is not an independent fact about an account — Xero computes it from
// Type — so a client that omits it, or sends one that disagrees, must still be
// answered with the class its type maps to. It used to be stored exactly as sent,
// which sorted the account into a statement it did not belong to (and, when the
// client sent none at all, into none).
func TestHTTP_AccountClassIsDerivedFromType(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"Code":  "CL-" + uuid.NewString()[:6],
		"Name":  "Mislabelled",
		"Type":  "FIXED",   // a fixed asset...
		"Class": "EXPENSE", // ...whose client called it an expense
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var created struct {
		Accounts []struct {
			AccountID string `json:"AccountID"`
			Class     string `json:"Class"`
		} `json:"Accounts"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Accounts, 1)
	assert.Equal(t, "ASSET", created.Accounts[0].Class, "the create response must carry the derived class")

	// A row written outside Create — before the class was derived, or by any
	// other writer — must be reported by its type too, since the column is only
	// a mirror of the mapping.
	_, err := h.repos.Pool.Exec(context.Background(),
		`UPDATE accounts SET class='EXPENSE' WHERE account_id=$1`, created.Accounts[0].AccountID)
	require.NoError(t, err)

	status, body = h.do(t, http.MethodGet, "/api/v1/accounts/"+created.Accounts[0].AccountID, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var fetched struct {
		Payload struct {
			Accounts []struct {
				Class string `json:"Class"`
			} `json:"Accounts"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &fetched))
	require.Len(t, fetched.Payload.Accounts, 1)
	assert.Equal(t, "ASSET", fetched.Payload.Accounts[0].Class, "a stored class must not override the type")
}

// A line whose AccountCode this organisation has no account for is a bad
// request: the code came from the client. It used to surface as a 500 from the
// posting switch.
func TestHTTP_InvoiceRejectsUnknownAccountCode(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":            "ACCREC",
		"Status":          "AUTHORISED",
		"ContactID":       createContact(t, h, "Unknown code "+uuid.NewString()[:6]),
		"Date":            "2026-04-01T00:00:00Z",
		"DueDate":         "2026-04-30T00:00:00Z",
		"LineAmountTypes": "Exclusive",
		"LineItems": []map[string]any{{
			"Description": "no such account", "Quantity": "1", "UnitAmount": "10",
			"AccountCode": "9999", "TaxAmount": "0",
		}},
	}, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))
}

// An unrecognised document type has no posting rule and no column constrains
// it, so it used to be saved as a draft — or, when authorised, fail in the
// posting switch with a 500.
func TestHTTP_DocumentsRejectUnknownType(t *testing.T) {
	h := newHarness(t)
	contactID := createContact(t, h, "Unknown type "+uuid.NewString()[:6])

	documents := []struct {
		what string
		path string
	}{
		{"invoice", "/api/v1/invoices"},
		{"credit note", "/api/v1/credit-notes"},
	}
	for _, tc := range documents {
		t.Run(tc.what, func(t *testing.T) {
			status, body := h.do(t, http.MethodPost, tc.path, map[string]any{
				"Type":            "NOT-A-TYPE",
				"Status":          "AUTHORISED",
				"ContactID":       contactID,
				"Date":            "2026-04-01T00:00:00Z",
				"DueDate":         "2026-04-30T00:00:00Z",
				"LineAmountTypes": "Exclusive",
				"LineItems": []map[string]any{{
					"Description": "line", "Quantity": "1", "UnitAmount": "10",
					"AccountCode": "200", "TaxAmount": "0",
				}},
			}, true)
			assert.Equal(t, http.StatusBadRequest, status, string(body))
		})
	}
}
