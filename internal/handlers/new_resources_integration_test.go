package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpUploadRequest(t *testing.T, h *appHarness, path string, body []byte, mime string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", mime)
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	return req
}

// Tests covering the new resources (credit notes, bank tx/transfers, manual
// journals, journals, quotes, purchase orders, contact groups, currencies,
// branding themes, tracking categories, attachments/history) plus the five
// accounting reports. They run against the pgtestdb template and verify
// end-to-end wiring from router → handler → repository → Postgres.

func TestHTTP_CreditNotes_CRUD(t *testing.T) {
	h := newHarness(t)

	// Create a contact to allocate the credit note to.
	status, body := h.do(t, http.MethodPost, "/api/v1/contacts",
		map[string]any{"Name": "Credit " + uuid.NewString()[:6], "IsCustomer": true}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var cEnv struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &cEnv))
	contactID := cEnv.Contacts[0].ContactID

	status, body = h.do(t, http.MethodPost, "/api/v1/credit-notes", map[string]any{
		"Type":      "ACCRECCREDIT",
		"Status":    "DRAFT",
		"ContactID": contactID,
		"Date":      "2026-02-01T00:00:00Z",
		"LineItems": []map[string]any{
			{"Description": "Return", "Quantity": "1", "UnitAmount": "25", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		CreditNotes []struct {
			CreditNoteID string `json:"CreditNoteID"`
		} `json:"CreditNotes"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.Len(t, env.CreditNotes, 1)
	id := env.CreditNotes[0].CreditNoteID

	status, _ = h.do(t, http.MethodGet, "/api/v1/credit-notes", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/credit-notes/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/credit-notes/"+id, map[string]any{
		"Reference": "updated",
	}, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/credit-notes/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_BankTransactions_Create(t *testing.T) {
	h := newHarness(t)

	// Need the seed bank account id.
	status, body := h.do(t, http.MethodGet, "/api/v1/accounts?type=BANK", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var accEnv struct {
		Payload struct {
			Accounts []struct {
				AccountID string `json:"AccountID"`
				Type      string `json:"Type"`
			} `json:"Accounts"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &accEnv))

	var bankID string
	for _, a := range accEnv.Payload.Accounts {
		if a.Type == "BANK" {
			bankID = a.AccountID
			break
		}
	}
	require.NotEmpty(t, bankID, "demo seed must include a BANK account")

	status, body = h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":            "RECEIVE",
		"Status":          "AUTHORISED",
		"LineAmountTypes": "Exclusive",
		"Date":            "2026-02-10T00:00:00Z",
		"BankAccountID":   bankID,
		"LineItems": []map[string]any{
			{"Description": "Cash sale", "Quantity": "1", "UnitAmount": "50", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, _ = h.do(t, http.MethodGet, "/api/v1/bank-transactions", nil, true)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTP_Quotes_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/contacts",
		map[string]any{"Name": "Quote " + uuid.NewString()[:6], "IsCustomer": true}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var cEnv struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &cEnv))

	status, body = h.do(t, http.MethodPost, "/api/v1/quotes", map[string]any{
		"Title":     "Quote Offer",
		"ContactID": cEnv.Contacts[0].ContactID,
		"Status":    "DRAFT",
		"Date":      "2026-02-01T00:00:00Z",
		"LineItems": []map[string]any{
			{"Description": "Widget", "Quantity": "2", "UnitAmount": "50", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Quotes []struct {
			QuoteID string `json:"QuoteID"`
		} `json:"Quotes"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.Quotes[0].QuoteID

	status, _ = h.do(t, http.MethodGet, "/api/v1/quotes", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/quotes/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/quotes/"+id, map[string]any{
		"Title": "Updated Offer",
	}, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/quotes/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_PurchaseOrders_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/contacts",
		map[string]any{"Name": "Supplier " + uuid.NewString()[:6], "IsSupplier": true}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var cEnv struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &cEnv))

	status, body = h.do(t, http.MethodPost, "/api/v1/purchase-orders", map[string]any{
		"ContactID": cEnv.Contacts[0].ContactID,
		"Status":    "DRAFT",
		"Date":      "2026-02-01T00:00:00Z",
		"LineItems": []map[string]any{
			{"Description": "Stationery", "Quantity": "10", "UnitAmount": "5", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		PurchaseOrders []struct {
			PurchaseOrderID string `json:"PurchaseOrderID"`
		} `json:"PurchaseOrders"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.PurchaseOrders[0].PurchaseOrderID

	status, _ = h.do(t, http.MethodGet, "/api/v1/purchase-orders/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/purchase-orders/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_ManualJournals_Create(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/manual-journals", map[string]any{
		"Narration": "Test journal",
		"Status":    "POSTED",
		"Date":      "2026-02-01T00:00:00Z",
		"JournalLines": []map[string]any{
			{"AccountCode": "200", "LineAmount": "-100"},
			{"AccountCode": "090", "LineAmount": "100"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, _ = h.do(t, http.MethodGet, "/api/v1/manual-journals", nil, true)
	assert.Equal(t, http.StatusOK, status)

	// Validation: narration required.
	status, _ = h.do(t, http.MethodPost, "/api/v1/manual-journals",
		map[string]any{"JournalLines": []map[string]any{{}, {}}}, true)
	assert.Equal(t, http.StatusBadRequest, status)
}

func TestHTTP_Journals_List(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(t, http.MethodGet, "/api/v1/journals?from=2026-01-01&to=2026-12-31", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
}

func TestHTTP_ContactGroups_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/contact-groups",
		map[string]any{"Name": "Group " + uuid.NewString()[:6]}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		ContactGroups []struct {
			ContactGroupID string `json:"ContactGroupID"`
		} `json:"ContactGroups"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.ContactGroups[0].ContactGroupID

	status, _ = h.do(t, http.MethodGet, "/api/v1/contact-groups", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/contact-groups/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/contact-groups/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Currencies_ListAndCreate(t *testing.T) {
	h := newHarness(t)

	status, _ := h.do(t, http.MethodGet, "/api/v1/currencies", nil, true)
	assert.Equal(t, http.StatusOK, status)

	code := "X" + uuid.NewString()[:2]
	status, _ = h.do(t, http.MethodPost, "/api/v1/currencies",
		map[string]any{"Code": code, "Description": "Test currency"}, true)
	assert.Equal(t, http.StatusCreated, status)
}

func TestHTTP_BrandingThemes_List(t *testing.T) {
	h := newHarness(t)
	status, _ := h.do(t, http.MethodGet, "/api/v1/branding-themes", nil, true)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTP_TrackingCategories_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/tracking-categories",
		map[string]any{"Name": "Region " + uuid.NewString()[:4]}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		TrackingCategories []struct {
			TrackingCategoryID string `json:"TrackingCategoryID"`
		} `json:"TrackingCategories"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.TrackingCategories[0].TrackingCategoryID

	status, _ = h.do(t, http.MethodPut, "/api/v1/tracking-categories/"+id+"/options",
		map[string]any{"Name": "North"}, true)
	assert.Equal(t, http.StatusCreated, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/tracking-categories/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/tracking-categories/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Reports(t *testing.T) {
	h := newHarness(t)

	endpoints := []string{
		"/api/v1/reports/trial-balance",
		"/api/v1/reports/profit-and-loss",
		"/api/v1/reports/balance-sheet",
		"/api/v1/reports/aged-receivables",
		"/api/v1/reports/aged-payables",
		"/api/v1/reports/bank-summary",
	}
	for _, ep := range endpoints {
		status, body := h.do(t, http.MethodGet, ep, nil, true)
		assert.Equalf(t, http.StatusOK, status, "%s => %s", ep, string(body))
	}
}

func TestHTTP_Invoice_FullUpdate(t *testing.T) {
	h := newHarness(t)

	inv := map[string]any{
		"Type":            "ACCREC",
		"Status":          "DRAFT",
		"LineAmountTypes": "Exclusive",
		"Date":            "2026-03-01T00:00:00Z",
		"DueDate":         "2026-03-15T00:00:00Z",
		"InvoiceNumber":   "FU-" + uuid.NewString()[:4],
		"LineItems": []map[string]any{
			{"Description": "Line1", "Quantity": "1", "UnitAmount": "10", "AccountCode": "200"},
		},
	}
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", inv, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.Invoices[0].InvoiceID

	// Full update: change line items — handler should recompute totals.
	status, body = h.do(t, http.MethodPut, "/api/v1/invoices/"+id, map[string]any{
		"Reference": "updated",
		"LineItems": []map[string]any{
			{"Description": "Line1 renamed", "Quantity": "2", "UnitAmount": "20", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var inv2 struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
			SubTotal  string `json:"SubTotal"`
			Total     string `json:"Total"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(body, &inv2))
	assert.Equal(t, "40", inv2.Invoices[0].SubTotal)

	// Payments list endpoint.
	status, _ = h.do(t, http.MethodGet, "/api/v1/invoices/"+id+"/payments", nil, true)
	assert.Equal(t, http.StatusOK, status)

	// Delete draft — should succeed with 204.
	status, _ = h.do(t, http.MethodDelete, "/api/v1/invoices/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Payment_GetAndVoid(t *testing.T) {
	h := newHarness(t)

	inv := map[string]any{
		"Type":            "ACCREC",
		"Status":          "AUTHORISED",
		"LineAmountTypes": "Exclusive",
		"Date":            "2026-04-01T00:00:00Z",
		"DueDate":         "2026-04-15T00:00:00Z",
		"InvoiceNumber":   "PV-" + uuid.NewString()[:4],
		"LineItems": []map[string]any{
			{"Description": "L", "Quantity": "1", "UnitAmount": "50", "AccountCode": "200"},
		},
	}
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", inv, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	invID := env.Invoices[0].InvoiceID

	status, body = h.do(t, http.MethodPost, "/api/v1/payments", map[string]any{
		"invoiceId": invID,
		"amount":    "10",
		"date":      "2026-04-05",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var pEnv struct {
		Payments []struct {
			PaymentID string `json:"PaymentID"`
		} `json:"Payments"`
	}
	require.NoError(t, json.Unmarshal(body, &pEnv))
	pid := pEnv.Payments[0].PaymentID

	status, _ = h.do(t, http.MethodGet, "/api/v1/payments/"+pid, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/payments/"+pid, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_TaxRates_UpdateDelete(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/tax-rates",
		map[string]any{"Name": "T-" + uuid.NewString()[:4], "TaxType": "OUTPUT",
			"DisplayTaxRate": "10", "EffectiveRate": "10"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		TaxRates []struct {
			TaxRateID string `json:"TaxRateID"`
		} `json:"TaxRates"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.TaxRates[0].TaxRateID

	status, _ = h.do(t, http.MethodGet, "/api/v1/tax-rates/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/tax-rates/"+id, map[string]any{
		"Name": "T-renamed", "TaxType": "OUTPUT",
		"DisplayTaxRate": "12", "EffectiveRate": "12",
	}, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/tax-rates/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Attachments_UploadListFetch(t *testing.T) {
	h := newHarness(t)

	// Create an invoice to attach to.
	inv := map[string]any{
		"Type":            "ACCREC",
		"Status":          "DRAFT",
		"LineAmountTypes": "Exclusive",
		"Date":            "2026-01-01T00:00:00Z",
		"DueDate":         "2026-01-15T00:00:00Z",
		"InvoiceNumber":   "AT-" + uuid.NewString()[:4],
		"LineItems": []map[string]any{
			{"Description": "L", "Quantity": "1", "UnitAmount": "1", "AccountCode": "200"},
		},
	}
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", inv, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	invID := env.Invoices[0].InvoiceID

	// Raw POST with bytes.
	req := httpUploadRequest(t, h, "/api/v1/invoices/"+invID+"/attachments/test.txt",
		[]byte("hello xero"), "text/plain")
	resp, err := h.app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	status, _ = h.do(t, http.MethodGet, "/api/v1/invoices/"+invID+"/attachments", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/invoices/"+invID+"/history", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodPut, "/api/v1/invoices/"+invID+"/history", map[string]any{
		"HistoryRecords": []map[string]any{{"Details": "Manual note"}},
	}, true)
	assert.Equal(t, http.StatusCreated, status)
}
