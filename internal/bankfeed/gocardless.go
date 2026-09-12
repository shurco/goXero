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

// ListInstitutions implements Provider. GoCardless answers with a whole
// country at once (a few hundred banks), so `query` is applied here rather than
// upstream — unlike Plaid, whose catalogue is far too large to enumerate.
func (g *GoCardless) ListInstitutions(ctx context.Context, country, query string) ([]Institution, error) {
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
	needle := strings.ToLower(strings.TrimSpace(query))
	for _, i := range out {
		if needle != "" &&
			!strings.Contains(strings.ToLower(i.Name), needle) &&
			!strings.Contains(strings.ToLower(i.BIC), needle) {
			continue
		}
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
//
// GoCardless needs no per-connection secret: the requisition id is the durable
// identity, so the returned Consent echoes it and carries no Secret.
func (g *GoCardless) FinalizeSession(ctx context.Context, cred Credential) (*Consent, error) {
	var req struct {
		Status   string   `json:"status"`
		Accounts []string `json:"accounts"`
	}
	if err := g.do(ctx, http.MethodGet, "/requisitions/"+url.PathEscape(cred.Reference)+"/", nil, &req); err != nil {
		return nil, err
	}
	if req.Status != "LN" && req.Status != "LINKED" {
		return nil, fmt.Errorf("%w: requisition not linked yet (status=%s)", ErrConsentNotFinished, req.Status)
	}
	out := make([]Account, 0, len(req.Accounts))
	for _, id := range req.Accounts {
		a, err := g.hydrateAccount(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return &Consent{Reference: cred.Reference, Accounts: out}, nil
}

// FetchStatementLines implements Provider — pulls both booked and pending
// transactions and returns them as a single slice.
//
// The amount is passed through untouched: this adapter relies on GoCardless
// already reporting money out as a negative amount, which is StatementLine's own
// convention and the opposite of Plaid's (mapPlaidTx negates). That reliance is
// pinned by TestMapGoCardlessTx's fixtures but has never been checked against the
// live API — a real debit arriving positive would silently invert every
// GoCardless reconciliation, and this is where the flip would go.
func (g *GoCardless) FetchStatementLines(ctx context.Context, cred Credential, externalAccountID string, from, to time.Time) ([]StatementLine, error) {
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

// GoCardlessError is a rejected call, carrying the status that says what kind of
// rejection it was. The body is kept verbatim because GoCardless explains the
// failure in it and there is no error code to read out.
type GoCardlessError struct {
	StatusCode int
	Body       string
	// Subject names what was called, for a message that says which step failed.
	Subject string
}

func (e *GoCardlessError) Error() string {
	return fmt.Sprintf("gocardless %s: %d %s", e.Subject, e.StatusCode, e.Body)
}

// ConsentBroken implements ConsentDiagnoser. A 403 means the access the user
// granted no longer covers the call — the bank-side consent is gone, which is
// what the requisition's own status would say if we could reach it. A 404 means
// the requisition or account no longer exists at all.
//
// A 401 is deliberately not in the set: it is the application's own token being
// refused, which is one broken deployment rather than every connection in it,
// and marking the tenant's feeds dead over it would be a repair per connection
// that fixes nothing. Rate limits and 5xx are transient by definition.
func (g *GoCardless) ConsentBroken(err error) bool {
	var apiErr *GoCardlessError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusForbidden || apiErr.StatusCode == http.StatusNotFound)
}

// do handles auth token refresh + JSON marshalling + error envelope. Payloads
// larger than 5 MiB are rejected to keep us honest.
func (g *GoCardless) do(ctx context.Context, method, path string, body, out any) error {
	// The token is taken under the lock and carried out of it: reading the field
	// here instead would race a refresh running for another connection's sync.
	token, err := g.ensureToken(ctx)
	if err != nil {
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
	req.Header.Set("Authorization", "Bearer "+token)

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
		return &GoCardlessError{StatusCode: resp.StatusCode, Body: errBody(raw), Subject: method + " " + path}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// RevokeConsent deletes the requisition, which is what ends our access to the
// accounts behind it. A reference is either the durable requisition id or — for
// a consent the user is still in the middle of — the session handle, which is
// the same thing at GoCardless; both are deletable, and deleting a requisition
// the user never finished is exactly as correct as deleting one they did.
func (g *GoCardless) RevokeConsent(ctx context.Context, cred Credential) error {
	if cred.Reference == "" {
		return nil
	}
	return g.do(ctx, http.MethodDelete, "/requisitions/"+url.PathEscape(cred.Reference)+"/", nil, nil)
}

// ensureToken returns a live access token, minting one if the cached token is
// missing or near expiry. It returns the token rather than leaving it in the
// field so the caller never reads it without the lock.
func (g *GoCardless) ensureToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.accessTok != "" && time.Now().Before(g.accessExpAt) {
		return g.accessTok, nil
	}
	body, err := json.Marshal(map[string]string{
		"secret_id":  g.secretID,
		"secret_key": g.secretKey,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/token/new/", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("gocardless token: %d %s", resp.StatusCode, errBody(raw))
	}
	var tok struct {
		Access        string `json:"access"`
		AccessExpires int    `json:"access_expires"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return "", err
	}
	if tok.Access == "" {
		return "", errors.New("gocardless token: empty access")
	}
	g.accessTok = tok.Access
	// Refresh 60s before expiry to avoid races around 401s.
	ttl := time.Duration(tok.AccessExpires)*time.Second - time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	g.accessExpAt = time.Now().Add(ttl)
	return g.accessTok, nil
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
