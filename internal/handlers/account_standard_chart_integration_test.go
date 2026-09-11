package handlers_test

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
)

// accountTypes is Xero's Account.Type enum (models.AccountType*), listed here so
// that a reference row outside the enum fails the test rather than reaching a
// client that will not accept it.
var accountTypes = []string{
	models.AccountTypeBank, models.AccountTypeCurrent, models.AccountTypeCurrLiab,
	models.AccountTypeDepreciatn, models.AccountTypeDirectCosts, models.AccountTypeEquity,
	models.AccountTypeExpense, models.AccountTypeFixed, models.AccountTypeInventory,
	models.AccountTypeLiability, models.AccountTypeNonCurrent, models.AccountTypeOverheads,
	models.AccountTypePrepayment, models.AccountTypeRevenue, models.AccountTypeSales,
	models.AccountTypeTermLiab, models.AccountTypePAYGLiab, models.AccountTypeSuperLiab,
	models.AccountTypeWages,
}

type capturedAccount struct {
	Code        string
	Name        string
	TaxRateName string
	Description string
}

// readCapturedChart reads migrations/data/xero/accounts.csv — the frozen capture
// of Xero's own chart (docs/xero-reference/README.md), which is where the
// reference table's rows come from and in what order.
func readCapturedChart(t *testing.T) []capturedAccount {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "migrations", "data", "xero", "accounts.csv"))
	require.NoError(t, err)
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	header := rows[0]
	col := func(name string) int {
		for i, h := range header {
			if h == name {
				return i
			}
		}
		t.Fatalf("the captured chart has no %q column", name)
		return -1
	}
	code, name, rate, desc := col("code"), col("name"), col("tax_rate"), col("description")

	out := make([]capturedAccount, 0, len(rows)-1)
	for _, r := range rows[1:] {
		out = append(out, capturedAccount{Code: r[code], Name: r[name], TaxRateName: r[rate], Description: r[desc]})
	}
	return out
}

// TestHTTP_StandardChart pins what "Import standard chart" offers against the
// capture it is meant to reproduce: the same rows, in the same order, with the
// two fields the API spells differently resolved and nothing else changed.
func TestHTTP_StandardChart(t *testing.T) {
	h := newHarness(t)

	status, body := h.do(t, http.MethodGet, "/api/v1/accounts/standard-chart", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var env struct {
		Payload struct {
			Accounts []models.Account `json:"Accounts"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	got := env.Payload.Accounts

	captured := readCapturedChart(t)
	require.Len(t, got, len(captured),
		"the standard chart is the captured chart: %d rows in the capture, %d served", len(captured), len(got))

	// This organisation's own tax rates, by the name Xero prints: the reference
	// table carries the name, the API speaks the type, and the type is a row of
	// whichever organisation is importing.
	rates, err := h.repos.TaxRates.List(t.Context(), seedDemoOrgID)
	require.NoError(t, err)
	taxTypeByName := make(map[string]string, len(rates))
	for _, r := range rates {
		taxTypeByName[r.Name] = r.TaxType
	}

	for i, want := range captured {
		acc := got[i]
		assert.Equal(t, want.Code, acc.Code, "row %d", i)
		assert.Equal(t, want.Name, acc.Name, "code %s", want.Code)
		assert.Equal(t, want.Description, acc.Description, "code %s", want.Code)
		assert.Equal(t, "ACTIVE", acc.Status, "code %s", want.Code)

		assert.Contains(t, accountTypes, acc.Type, "code %s: %q is not an Account.Type", want.Code, acc.Type)
		assert.Equal(t, models.AccountClassForType(acc.Type), acc.Class,
			"code %s: the class is the one its type gives it", want.Code)

		taxType, ok := taxTypeByName[want.TaxRateName]
		require.True(t, ok, "code %s: these books have no tax rate named %q", want.Code, want.TaxRateName)
		assert.Equal(t, taxType, acc.TaxType, "code %s: the tax type is this organisation's row for %q",
			want.Code, want.TaxRateName)

		// A chart template carries no money: the capture's own YTD column is
		// deliberately not here, so an import cannot invent an opening balance.
		assert.Empty(t, acc.SystemAccount, "code %s: a role is the organisation's to declare, not a template's", want.Code)
	}
}

// TestHTTP_StandardChart_IsNotAnAccountID guards the route's place ahead of
// /accounts/:id: read as an id, "standard-chart" would be a 400, and a chart
// read as an id is a chart nobody can import.
func TestHTTP_StandardChart_IsNotAnAccountID(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(t, http.MethodGet, "/api/v1/accounts/standard-chart", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), `"Code":"090"`)
}
