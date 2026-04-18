package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests for the "extras" added alongside the Reporting refresh:
// Prepayments, Overpayments, Repeating invoices, Batch payments,
// Linked transactions, Employees, Receipts, Expense claims, Users endpoint,
// plus the new Executive / Cash / Budget / BAS / Journal reports.
//
// They all run against the pgtestdb template so they hit the real schema and
// query paths end-to-end.

func TestHTTP_Prepayments_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/prepayments", map[string]any{
		"Type":         "RECEIVE-PREPAYMENT",
		"CurrencyCode": "USD",
		"Date":         "2026-02-01T00:00:00Z",
		"Reference":    "Retainer",
		"Total":        "100",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Prepayments []struct {
			PrepaymentID string `json:"PrepaymentID"`
		} `json:"Prepayments"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.Prepayments[0].PrepaymentID
	require.NotEmpty(t, id)

	status, _ = h.do(t, http.MethodGet, "/api/v1/prepayments", nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodGet, "/api/v1/prepayments/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/prepayments/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Overpayments_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/overpayments", map[string]any{
		"Type":      "RECEIVE-OVERPAYMENT",
		"Date":      "2026-02-01T00:00:00Z",
		"Reference": "Overpaid",
		"Total":     "50",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Overpayments []struct {
			OverpaymentID string `json:"OverpaymentID"`
		} `json:"Overpayments"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.Overpayments[0].OverpaymentID

	status, _ = h.do(t, http.MethodGet, "/api/v1/overpayments/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)
	status, _ = h.do(t, http.MethodDelete, "/api/v1/overpayments/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_RepeatingInvoices_CRUD(t *testing.T) {
	h := newHarness(t)

	// Need a contact.
	status, body := h.do(t, http.MethodPost, "/api/v1/contacts", map[string]any{
		"Name":       "Repeating " + uuid.NewString()[:6],
		"IsCustomer": true,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var cEnv struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &cEnv))

	status, body = h.do(t, http.MethodPost, "/api/v1/repeating-invoices", map[string]any{
		"Type":      "ACCREC",
		"ContactID": cEnv.Contacts[0].ContactID,
		"Reference": "RI-1",
		"Schedule": map[string]any{
			"Period":      1,
			"Unit":        "MONTHLY",
			"DueDate":     0,
			"DueDateType": "DAYSAFTERBILLDATE",
			"StartDate":   "2026-03-01T00:00:00Z",
		},
		"LineItems": []map[string]any{
			{"Description": "Monthly retainer", "Quantity": "1", "UnitAmount": "100", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		RepeatingInvoices []struct {
			RepeatingInvoiceID string `json:"RepeatingInvoiceID"`
		} `json:"RepeatingInvoices"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.RepeatingInvoices[0].RepeatingInvoiceID

	status, _ = h.do(t, http.MethodGet, "/api/v1/repeating-invoices/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/repeating-invoices/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_BatchPayments_Create(t *testing.T) {
	h := newHarness(t)

	// Find bank account id.
	status, body := h.do(t, http.MethodGet, "/api/v1/accounts?type=BANK", nil, true)
	require.Equal(t, http.StatusOK, status)
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
	require.NotEmpty(t, bankID)

	// Contact + invoice so we have something to pay.
	status, body = h.do(t, http.MethodPost, "/api/v1/contacts", map[string]any{
		"Name":       "Batch " + uuid.NewString()[:6],
		"IsCustomer": true,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var cEnv struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &cEnv))

	status, body = h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":      "ACCREC",
		"Status":    "AUTHORISED",
		"ContactID": cEnv.Contacts[0].ContactID,
		"Date":      "2026-03-01T00:00:00Z",
		"DueDate":   "2026-03-15T00:00:00Z",
		"LineItems": []map[string]any{
			{"Description": "Service", "Quantity": "1", "UnitAmount": "50", "AccountCode": "200"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var invEnv struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(body, &invEnv))
	invID := invEnv.Invoices[0].InvoiceID

	status, body = h.do(t, http.MethodPost, "/api/v1/batch-payments", map[string]any{
		"AccountID": bankID,
		"Date":      "2026-03-20T00:00:00Z",
		"Reference": "Batch-1",
		"Payments": []map[string]any{
			{"InvoiceID": invID, "Amount": "50", "Reference": "Pay-1"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

func TestHTTP_LinkedTransactions_CRUD(t *testing.T) {
	h := newHarness(t)

	source := uuid.NewString()
	status, body := h.do(t, http.MethodPost, "/api/v1/linked-transactions", map[string]any{
		"SourceTransactionID": source,
		"Type":                "BILLABLE_EXPENSE",
		"Status":              "DRAFT",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		LinkedTransactions []struct {
			LinkedTransactionID string `json:"LinkedTransactionID"`
		} `json:"LinkedTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.LinkedTransactions[0].LinkedTransactionID

	status, _ = h.do(t, http.MethodGet, "/api/v1/linked-transactions/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/linked-transactions/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Employees_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/employees", map[string]any{
		"FirstName": "Jane",
		"LastName":  "Doe",
		"Email":     "jane@example.test",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Employees []struct {
			EmployeeID string `json:"EmployeeID"`
		} `json:"Employees"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.Employees[0].EmployeeID

	status, _ = h.do(t, http.MethodPut, "/api/v1/employees/"+id, map[string]any{
		"FirstName": "Janet", "LastName": "Doe", "Status": "ACTIVE",
	}, true)
	assert.Equal(t, http.StatusOK, status)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/employees/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_Receipts_CRUD(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/receipts", map[string]any{
		"Date":      "2026-02-10T00:00:00Z",
		"Reference": "Taxi",
		"LineItems": []map[string]any{
			{"Description": "Airport taxi", "Quantity": "1", "UnitAmount": "35", "AccountCode": "429"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var env struct {
		Receipts []struct {
			ReceiptID string `json:"ReceiptID"`
		} `json:"Receipts"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	id := env.Receipts[0].ReceiptID

	status, _ = h.do(t, http.MethodGet, "/api/v1/receipts/"+id, nil, true)
	assert.Equal(t, http.StatusOK, status)
	status, _ = h.do(t, http.MethodDelete, "/api/v1/receipts/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestHTTP_ExpenseClaims_CRUD(t *testing.T) {
	h := newHarness(t)

	// First create a receipt.
	status, body := h.do(t, http.MethodPost, "/api/v1/receipts", map[string]any{
		"Date": "2026-02-01T00:00:00Z",
		"LineItems": []map[string]any{
			{"Description": "Lunch", "Quantity": "1", "UnitAmount": "20", "AccountCode": "429"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var rEnv struct {
		Receipts []struct {
			ReceiptID string `json:"ReceiptID"`
		} `json:"Receipts"`
	}
	require.NoError(t, json.Unmarshal(body, &rEnv))

	status, body = h.do(t, http.MethodPost, "/api/v1/expense-claims", map[string]any{
		"ReceiptIDs": []string{rEnv.Receipts[0].ReceiptID},
		"Status":     "SUBMITTED",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

func TestHTTP_Users_List(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(t, http.MethodGet, "/api/v1/users", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var env struct {
		Payload struct {
			Users []struct {
				UserID       string `json:"UserID"`
				EmailAddress string `json:"EmailAddress"`
			} `json:"Users"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmpty(t, env.Payload.Users, "demo organisation should expose at least the seed user")
}

func TestHTTP_Reports_XeroShape(t *testing.T) {
	h := newHarness(t)

	for _, ep := range []string{
		"/api/v1/reports",
		"/api/v1/reports/trial-balance?date=2026-03-31",
		"/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-03-31",
		"/api/v1/reports/balance-sheet?date=2026-03-31",
		"/api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-03-31",
		"/api/v1/reports/bank-summary?fromDate=2026-01-01&toDate=2026-03-31",
		"/api/v1/reports/aged-receivables",
		"/api/v1/reports/aged-payables",
		"/api/v1/reports/executive-summary",
		"/api/v1/reports/budget-summary",
		"/api/v1/reports/bas?fromDate=2026-01-01&toDate=2026-03-31",
		"/api/v1/reports/journal-report?fromDate=2026-01-01&toDate=2026-03-31",
	} {
		status, body := h.do(t, http.MethodGet, ep, nil, true)
		require.Equalf(t, http.StatusOK, status, "%s => %s", ep, string(body))

		// All the individual report endpoints (everything except the index
		// `/reports`) must return the Xero envelope shape.
		if ep == "/api/v1/reports" {
			continue
		}
		var env struct {
			Status  string `json:"Status"`
			Reports []struct {
				ReportID   string `json:"ReportID"`
				ReportName string `json:"ReportName"`
				ReportType string `json:"ReportType"`
				Rows       []struct {
					RowType string `json:"RowType"`
				} `json:"Rows"`
			} `json:"Reports"`
		}
		require.NoErrorf(t, json.Unmarshal(body, &env), "%s: %s", ep, string(body))
		require.Equal(t, "OK", env.Status, ep)
		require.Len(t, env.Reports, 1, ep)
		r := env.Reports[0]
		require.NotEmpty(t, r.ReportID, ep)
		require.NotEmpty(t, r.ReportName, ep)
		require.NotEmpty(t, r.Rows, ep)
		assert.Equal(t, "Header", r.Rows[0].RowType, ep)
	}
}
