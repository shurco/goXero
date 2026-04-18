package bankfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// goCardlessBaseURL is the GoCardless Bank Account Data v2 endpoint. Override
// in tests via NewGoCardless(..., WithBaseURL).
const goCardlessBaseURL = "https://bankaccountdata.gocardless.com/api/v2"

// GoCardless is an adapter for the GoCardless Bank Account Data API (formerly
// Nordigen). It implements the full PSD2 consent flow:
//
//  1. POST /token/new/              — exchange secret_id/secret_key → JWT
//  2. GET  /institutions/           — list banks by country
//  3. POST /requisitions/           — create consent link
//  4. GET  /requisitions/{id}/      — poll, collect linked account ids
//  5. GET  /accounts/{id}/transactions/ — pull booked + pending movements
//
// Docs: https://bankaccountdata.gocardless.com/api/docs
type GoCardless struct {
	baseURL    string
	secretID   string
	secretKey  string
	httpClient *http.Client

	mu          sync.Mutex
	accessTok   string
	accessExpAt time.Time
}

// GoCardlessOption configures the adapter.
type GoCardlessOption func(*GoCardless)

// WithGoCardlessHTTPClient swaps the http client (tests mostly).
func WithGoCardlessHTTPClient(c *http.Client) GoCardlessOption {
	return func(g *GoCardless) { g.httpClient = c }
}

// WithGoCardlessBaseURL overrides the API host. Used by tests.
func WithGoCardlessBaseURL(u string) GoCardlessOption {
	return func(g *GoCardless) { g.baseURL = strings.TrimRight(u, "/") }
}

// NewGoCardless returns a configured adapter. The caller is expected to check
// Credentials() (both non-empty) before registering it — an adapter without
// creds would return auth failures on every call.
func NewGoCardless(secretID, secretKey string, opts ...GoCardlessOption) *GoCardless {
	g := &GoCardless{
		baseURL:    goCardlessBaseURL,
		secretID:   secretID,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Name implements Provider.
func (g *GoCardless) Name() string { return ProviderGoCardlessBAD }

// Credentials reports whether the adapter has non-empty secrets. Main.go uses
// it to decide whether to register the adapter at boot.
func (g *GoCardless) Credentials() bool { return g.secretID != "" && g.secretKey != "" }

// ListInstitutions implements Provider.
func (g *GoCardless) ListInstitutions(ctx context.Context, country string) ([]Institution, error) {
	q := url.Values{}
	if country != "" {
		q.Set("country", strings.ToUpper(country))
	}
	var out []struct {
		ID                   string   `json:"id"`
		Name                 string   `json:"name"`
		BIC                  string   `json:"bic"`
		Countries            []string `json:"countries"`
		Logo                 string   `json:"logo"`
		TransactionTotalDays string   `json:"transaction_total_days"`
	}
	if err := g.do(ctx, http.MethodGet, "/institutions/?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	res := make([]Institution, 0, len(out))
	for _, i := range out {
		res = append(res, Institution{
			ID: i.ID, Name: i.Name, BIC: i.BIC,
			Countries: i.Countries, LogoURL: i.Logo,
			TxDays: atoiDefault(i.TransactionTotalDays, 90),
		})
	}
	return res, nil
}

// CreateSession implements Provider — creates a requisition + returns the
// GoCardless-hosted consent link.
func (g *GoCardless) CreateSession(ctx context.Context, req SessionRequest) (*Session, error) {
	body := map[string]any{
		"redirect":       req.RedirectURL,
		"institution_id": req.InstitutionID,
		"reference":      req.Reference,
		"user_language":  "EN",
	}
	var out struct {
		ID   string `json:"id"`
		Link string `json:"link"`
	}
	if err := g.do(ctx, http.MethodPost, "/requisitions/", body, &out); err != nil {
		return nil, err
	}
	return &Session{ExternalReference: out.ID, AuthURL: out.Link}, nil
}

// FinalizeSession implements Provider. After the user returns from the bank,
// the requisition status flips to LN (linked) and carries the account ids —
// we then hydrate each one with /accounts/{id}/ and /balances/.
func (g *GoCardless) FinalizeSession(ctx context.Context, externalReference string) ([]Account, error) {
	var req struct {
		Status   string   `json:"status"`
		Accounts []string `json:"accounts"`
	}
	if err := g.do(ctx, http.MethodGet, "/requisitions/"+url.PathEscape(externalReference)+"/", nil, &req); err != nil {
		return nil, err
	}
	if req.Status != "LN" && req.Status != "LINKED" {
		return nil, fmt.Errorf("requisition not linked yet (status=%s)", req.Status)
	}
	out := make([]Account, 0, len(req.Accounts))
	for _, id := range req.Accounts {
		a, err := g.hydrateAccount(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, nil
}

// FetchStatementLines implements Provider — pulls both booked and pending
// transactions and returns them as a single slice. Amounts are signed: credit
// stays positive, debit becomes negative (provider returns them as absolute
// values in the booked/pending arrays, with no explicit side, so we rely on
// the amount already being signed per ISO 20022 / PSD2 convention).
func (g *GoCardless) FetchStatementLines(ctx context.Context, externalAccountID string, from, to time.Time) ([]StatementLine, error) {
	q := url.Values{}
	if !from.IsZero() {
		q.Set("date_from", from.Format("2006-01-02"))
	}
	if !to.IsZero() {
		q.Set("date_to", to.Format("2006-01-02"))
	}
	var out struct {
		Transactions struct {
			Booked  []goCardlessTx `json:"booked"`
			Pending []goCardlessTx `json:"pending"`
		} `json:"transactions"`
	}
	path := "/accounts/" + url.PathEscape(externalAccountID) + "/transactions/"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	if err := g.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	all := append(out.Transactions.Booked, out.Transactions.Pending...)
	res := make([]StatementLine, 0, len(all))
	for _, t := range all {
		sl, err := mapGoCardlessTx(t)
		if err != nil {
			return nil, err
		}
		res = append(res, sl)
	}
	return res, nil
}

// hydrateAccount fetches /accounts/{id}/details/ + /balances/ and collapses
// them into a single Account.
func (g *GoCardless) hydrateAccount(ctx context.Context, id string) (*Account, error) {
	a := &Account{ExternalID: id}

	var details struct {
		Account struct {
			IBAN       string `json:"iban"`
			Name       string `json:"name"`
			OwnerName  string `json:"ownerName"`
			Currency   string `json:"currency"`
			Product    string `json:"product"`
			ResourceID string `json:"resourceId"`
		} `json:"account"`
	}
	if err := g.do(ctx, http.MethodGet, "/accounts/"+url.PathEscape(id)+"/details/", nil, &details); err != nil {
		return nil, err
	}
	a.IBAN = details.Account.IBAN
	a.CurrencyCode = details.Account.Currency
	a.DisplayName = firstNonEmpty(details.Account.Name, details.Account.OwnerName, details.Account.Product, id)

	var bal struct {
		Balances []struct {
			BalanceAmount struct {
				Amount   string `json:"amount"`
				Currency string `json:"currency"`
			} `json:"balanceAmount"`
			BalanceType string `json:"balanceType"`
		} `json:"balances"`
	}
	if err := g.do(ctx, http.MethodGet, "/accounts/"+url.PathEscape(id)+"/balances/", nil, &bal); err == nil {
		for _, b := range bal.Balances {
			if d, err := decimal.NewFromString(b.BalanceAmount.Amount); err == nil {
				a.Balance = &d
				if a.CurrencyCode == "" {
					a.CurrencyCode = b.BalanceAmount.Currency
				}
				break
			}
		}
	}
	return a, nil
}

// do handles auth token refresh + JSON marshalling + error envelope. Payloads
// larger than 5 MiB are rejected to keep us honest.
func (g *GoCardless) do(ctx context.Context, method, path string, body, out any) error {
	if err := g.ensureToken(ctx); err != nil {
		return err
	}
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+g.accessTok)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gocardless %s %s: %d %s", method, path, resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (g *GoCardless) ensureToken(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.accessTok != "" && time.Now().Before(g.accessExpAt) {
		return nil
	}
	body, err := json.Marshal(map[string]string{
		"secret_id":  g.secretID,
		"secret_key": g.secretKey,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/token/new/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gocardless token: %d %s", resp.StatusCode, string(raw))
	}
	var tok struct {
		Access        string `json:"access"`
		AccessExpires int    `json:"access_expires"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return err
	}
	if tok.Access == "" {
		return errors.New("gocardless token: empty access")
	}
	g.accessTok = tok.Access
	// Refresh 60s before expiry to avoid races around 401s.
	ttl := time.Duration(tok.AccessExpires)*time.Second - time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	g.accessExpAt = time.Now().Add(ttl)
	return nil
}

// goCardlessTx matches the shape under /accounts/{id}/transactions/ booked or
// pending arrays (trimmed to fields we care about).
type goCardlessTx struct {
	TransactionID         string `json:"transactionId"`
	InternalTransactionID string `json:"internalTransactionId"`
	BookingDate           string `json:"bookingDate"`
	ValueDate             string `json:"valueDate"`
	TransactionAmount     struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"transactionAmount"`
	CreditorName                      string `json:"creditorName"`
	DebtorName                        string `json:"debtorName"`
	RemittanceInformationUnstructured string `json:"remittanceInformationUnstructured"`
	EndToEndID                        string `json:"endToEndId"`
	MandateID                         string `json:"mandateId"`
}

// mapGoCardlessTx converts a provider row into our provider-agnostic shape.
// Exported via the test-only alias MapGoCardlessTx so unit tests can pin the
// behaviour.
func mapGoCardlessTx(t goCardlessTx) (StatementLine, error) {
	amount, err := decimal.NewFromString(t.TransactionAmount.Amount)
	if err != nil {
		return StatementLine{}, fmt.Errorf("invalid amount %q: %w", t.TransactionAmount.Amount, err)
	}
	id := firstNonEmpty(t.TransactionID, t.InternalTransactionID, t.EndToEndID)
	if id == "" {
		return StatementLine{}, errors.New("transaction without id — cannot dedup")
	}
	posted := parseDateOrZero(t.BookingDate)
	if posted.IsZero() {
		posted = parseDateOrZero(t.ValueDate)
	}
	counterparty := firstNonEmpty(t.CreditorName, t.DebtorName)
	raw, _ := json.Marshal(t)
	return StatementLine{
		ProviderTxID: id,
		PostedAt:     posted,
		Amount:       amount,
		CurrencyCode: t.TransactionAmount.Currency,
		Description:  t.RemittanceInformationUnstructured,
		Counterparty: counterparty,
		Reference:    firstNonEmpty(t.EndToEndID, t.MandateID),
		Raw:          raw,
	}, nil
}

// MapGoCardlessTx is a test-visible alias for mapGoCardlessTx.
func MapGoCardlessTx(payload []byte) (StatementLine, error) {
	var t goCardlessTx
	if err := json.Unmarshal(payload, &t); err != nil {
		return StatementLine{}, err
	}
	return mapGoCardlessTx(t)
}

func parseDateOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func atoiDefault(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}
