package bankfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Plaid API hosts. Which one we talk to is PLAID_ENV; tests override it via
// WithPlaidBaseURL.
const (
	plaidSandboxURL    = "https://sandbox.plaid.com"
	plaidProductionURL = "https://production.plaid.com"
)

// Plaid caps a single institution page at 500 and a single sync page at 500
// transactions; the loops below page until they are done, bounded by these caps
// so a misbehaving upstream cannot spin forever.
const (
	plaidInstitutionPage     = 500
	plaidTransactionPage     = 500
	plaidMaxSyncPages        = 40  // 20k changed transactions per sync
	plaidMaxGetPages         = 20  // 10k transactions per date-range fetch
	plaidHistoryDays         = 730 // Plaid serves up to 24 months
	plaidHostedLinkURLTTLSec = 3600
)

// plaidAccountTypes are the account classes whose transactions /transactions/sync
// actually serves and that Xero models as bank accounts. Investment accounts need
// the Investments product, and a mortgage is not something you reconcile, so both
// are dropped rather than shown as an unusable row.
var plaidAccountTypes = map[string]bool{"depository": true, "credit": true}

// Plaid is an adapter for the Plaid API (https://plaid.com/docs), the US/CA
// aggregator. It implements the consent flow through Hosted Link, so goXero
// needs no JavaScript SDK: /link/token/create returns a plaid-hosted URL, the
// user completes consent there, and we pick the resulting public_token back up
// through /link/token/get.
//
//  1. POST /link/token/create        — hosted_link_url + link_token
//  2. (user consents on Plaid's page, then returns to our redirect URL)
//  3. POST /link/token/get           — public_token of the finished session
//  4. POST /item/public_token/exchange — access_token + item_id
//  5. POST /accounts/get             — the accounts the user granted
//  6. POST /transactions/sync        — incremental movements, by cursor
//
// Unlike GoCardless, Plaid authenticates per Item with an access_token issued at
// step 4 — which is why every call after consent takes a Credential.
type Plaid struct {
	baseURL    string
	clientID   string
	secret     string
	clientName string
	// webhookURL is where Plaid posts notifications, given to it on every link
	// token we create. Empty means this deployment cannot be reached from the
	// outside, in which case the browser redirect and /link/token/get carry the
	// flow on their own.
	webhookURL string
	httpClient *http.Client

	// keys caches what Plaid said about a signing key id, hit or miss. See
	// webhookKey.
	keyMu sync.Mutex
	keys  map[string]plaidWebhookKey
	// keyLookupStart and keyLookupCount spend the unknown-key budget. They share
	// keyMu with the cache because they are the same bookkeeping: what we know,
	// and what we are willing to ask about next.
	keyLookupStart time.Time
	keyLookupCount int
}

// PlaidOption configures the adapter.
type PlaidOption func(*Plaid)

// WithPlaidHTTPClient swaps the http client (tests mostly).
func WithPlaidHTTPClient(c *http.Client) PlaidOption {
	return func(p *Plaid) { p.httpClient = c }
}

// WithPlaidBaseURL overrides the API host. Used by tests.
func WithPlaidBaseURL(u string) PlaidOption {
	return func(p *Plaid) { p.baseURL = strings.TrimRight(u, "/") }
}

// WithPlaidWebhookURL sets the public URL Plaid should post notifications to.
// Omit it and Plaid is told nothing, which is right for a deployment that is not
// reachable from the internet: consent then completes when the browser returns.
func WithPlaidWebhookURL(u string) PlaidOption {
	return func(p *Plaid) { p.webhookURL = strings.TrimSpace(u) }
}

// NewPlaid returns a configured adapter. `production` selects the API host;
// clientName is what the user sees inside Plaid's consent UI and is capped by
// Plaid at 30 characters. The caller is expected to check Credentials() before
// registering — an adapter without them would 400 on every call.
func NewPlaid(clientID, secret, clientName string, production bool, opts ...PlaidOption) *Plaid {
	p := &Plaid{
		baseURL:    plaidSandboxURL,
		clientID:   clientID,
		secret:     secret,
		clientName: clientName,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		keys:       map[string]plaidWebhookKey{},
	}
	if production {
		p.baseURL = plaidProductionURL
	}
	if p.clientName == "" {
		p.clientName = "goxero"
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name implements Provider.
func (p *Plaid) Name() string { return ProviderPlaid }

// Credentials reports whether the adapter has non-empty secrets. Router uses it
// to decide whether to register the adapter at boot.
func (p *Plaid) Credentials() bool { return p.clientID != "" && p.secret != "" }

// ListInstitutions implements Provider. Plaid's US catalogue runs to ~12k
// institutions, far too many to ship in one response, so a non-empty query is
// answered by Plaid's own search index; an empty one falls back to the first
// page of the catalogue and the UI asks the user to narrow it down.
func (p *Plaid) ListInstitutions(ctx context.Context, country, query string) ([]Institution, error) {
	if country == "" {
		country = "US"
	}
	country = strings.ToUpper(country)

	if strings.TrimSpace(query) == "" {
		var out struct {
			Institutions []plaidInstitution `json:"institutions"`
		}
		body := map[string]any{
			"count":         plaidInstitutionPage,
			"offset":        0,
			"country_codes": []string{country},
			"options":       map[string]any{"products": []string{"transactions"}},
		}
		if err := p.do(ctx, "/institutions/get", body, &out); err != nil {
			return nil, err
		}
		return mapPlaidInstitutions(out.Institutions), nil
	}

	// The two endpoints disagree about where `products` goes, and Plaid answers a
	// misplaced one with UNKNOWN_FIELDS rather than a hint: /institutions/get takes
	// it under `options`, /institutions/search takes it at the top level and rejects
	// it anywhere else. Both were confirmed against the sandbox — the one thing that
	// could have told us, since a stub answers whatever it is asked.
	var out struct {
		Institutions []plaidInstitution `json:"institutions"`
	}
	body := map[string]any{
		"query":         query,
		"country_codes": []string{country},
		"products":      []string{"transactions"},
	}
	if err := p.do(ctx, "/institutions/search", body, &out); err != nil {
		return nil, err
	}
	return mapPlaidInstitutions(out.Institutions), nil
}

// CreateSession implements Provider — creates a Hosted Link session and returns
// the URL Plaid hosts the consent UI at. The institution the user already picked
// is passed through so Plaid's own picker is skipped.
//
// A Plaid account can be restricted to choosing institutions itself, and the
// refusal covers *every* institution_id — including the ones its own
// /institutions/get just returned — with INVALID_INSTITUTION, which is a fact
// about the account rather than about the id. `institution_id` is definitely the
// right field (any other spelling earns UNKNOWN_FIELDS), so there is nothing to
// fix in the body: Hosted Link is the whole Link flow, and dropping the field
// hands the choice back to Plaid's picker instead of failing the connection
// outright. Consent then reports the institution the user actually picked, and
// the connection is labelled with it.
func (p *Plaid) CreateSession(ctx context.Context, req SessionRequest) (*Session, error) {
	body := p.linkBody(req)
	body["products"] = []string{"transactions"}
	// How much history the Item is consented to carry; sync cannot reach
	// further back than this.
	body["transactions"] = map[string]any{"days_requested": plaidHistoryDays}
	if req.InstitutionID != "" {
		body["institution_id"] = req.InstitutionID
		session, err := p.linkSession(ctx, body, req.RedirectURL)
		if err == nil {
			session.InstitutionPinned = true
			return session, nil
		}
		var apiErr *PlaidError
		if !errors.As(err, &apiErr) || apiErr.Code != plaidErrInvalidInstitution {
			return nil, err
		}
		delete(body, "institution_id")
	}
	return p.linkSession(ctx, body, req.RedirectURL)
}

// PicksInstitution implements InstitutionPicker: Hosted Link is the full Link
// flow, so a session started without an institution simply opens on Plaid's
// picker.
func (p *Plaid) PicksInstitution() bool { return true }

// CreateReconsentSession implements ReconsentProvider. Link started this way is
// what Plaid calls update mode: the session authenticates the Item we already
// hold rather than creating a new one, so it carries the access token and — per
// Plaid's rules for update mode — no `products` at all, because it renews
// consent for what the Item already has instead of asking for anything new.
//
// Account selection is deliberately left off: it is a separate opt-in for
// letting the user re-pick which accounts they share, and a repair should not
// quietly change what we are subscribed to.
func (p *Plaid) CreateReconsentSession(ctx context.Context, cred Credential, req SessionRequest) (*Session, error) {
	if cred.Secret == "" {
		return nil, errors.New("plaid: connection has no access token")
	}
	body := p.linkBody(req)
	body["access_token"] = cred.Secret
	return p.linkSession(ctx, body, req.RedirectURL)
}

// linkBody is the part of a /link/token/create body that is the same whether the
// session is asking for consent or repairing one. Keeping it in one place is what
// makes the repair name the country the user originally searched under, since
// Plaid checks country_codes against the Item being repaired.
func (p *Plaid) linkBody(req SessionRequest) map[string]any {
	return map[string]any{
		"client_name":   p.clientName,
		"language":      "en",
		"country_codes": plaidCountries(req.Country),
		"user":          map[string]any{"client_user_id": req.Reference},
	}
}

// CompleteReconsent implements ReconsentProvider. An Item's access token does
// not change when Link is used in update mode, so there is nothing to exchange
// here — and exchanging a public token would create a second Item for a bank
// account we already track. All that is left is to re-read the accounts, which
// is what picks up one that was renamed or added back.
func (p *Plaid) CompleteReconsent(ctx context.Context, cred Credential) (*Consent, error) {
	if cred.Secret == "" {
		return nil, errors.New("plaid: connection has no access token")
	}
	accounts, institution, err := p.accounts(ctx, cred.Secret, cred.Reference)
	if err != nil {
		return nil, err
	}
	return &Consent{Reference: cred.Reference, Secret: cred.Secret, Accounts: accounts, Institution: institution}, nil
}

// linkSession creates a link token and returns the session around it. Both the
// first-time flow and a repair go through here, so the Hosted Link setup — the
// part that lets goXero do without Plaid's JavaScript SDK — cannot drift between
// the two.
func (p *Plaid) linkSession(ctx context.Context, body map[string]any, redirectURL string) (*Session, error) {
	hosted := map[string]any{"url_lifetime_seconds": plaidHostedLinkURLTTLSec}
	if redirectURL != "" {
		hosted["completion_redirect_uri"] = redirectURL
	}
	body["hosted_link"] = hosted
	if p.webhookURL != "" {
		// Plaid calls the webhook the primary means of delivering the
		// public_token. It is only a convenience here: /link/token/get answers
		// the same question for six hours after the session ends, which is what
		// carries a deployment that cannot be reached from the outside.
		body["webhook"] = p.webhookURL
	}
	var out struct {
		LinkToken     string `json:"link_token"`
		HostedLinkURL string `json:"hosted_link_url"`
		Expiration    string `json:"expiration"`
	}
	if err := p.do(ctx, "/link/token/create", body, &out); err != nil {
		return nil, err
	}
	if out.HostedLinkURL == "" {
		return nil, errors.New("plaid: /link/token/create returned no hosted_link_url — is Hosted Link enabled for this account?")
	}
	sess := &Session{ExternalReference: out.LinkToken, AuthURL: out.HostedLinkURL}
	if t, err := time.Parse(time.RFC3339, out.Expiration); err == nil {
		sess.ExpiresAt = &t
	}
	return sess, nil
}

// plaidCountries is the country_codes list for a session. Plaid rejects an
// institution that is not in it, so this follows the country the user searched
// in — the adapter covers CA as well as US, and a hardcoded "US" would fail
// Canadian banks with an opaque error.
func plaidCountries(country string) []string {
	if country == "" {
		return []string{"US"}
	}
	return []string{strings.ToUpper(country)}
}

// FinalizeSession implements Provider. The reference it is handed is the
// link_token from CreateSession; it asks Plaid what that session produced,
// trades the public_token for a durable Item, and returns the accounts. The
// durable identity (item_id + access_token) is what the caller must store — the
// link_token is single-use and short-lived.
//
// Plaid keeps completed session data for six hours, and Hosted Link has no
// callback of its own, so this is the primary (not backup) path for self-hosted
// deployments that cannot receive webhooks.
func (p *Plaid) FinalizeSession(ctx context.Context, cred Credential) (*Consent, error) {
	publicToken, err := p.sessionPublicToken(ctx, cred.Reference)
	if err != nil {
		return nil, err
	}
	return p.ExchangePublicToken(ctx, publicToken)
}

// ExchangePublicToken implements PublicTokenExchanger: the public token becomes
// the durable Item identity plus the accounts behind it. It is shared by the
// browser return (which reads the token out of the session first) and by the
// SESSION_FINISHED webhook (which is handed the token directly).
func (p *Plaid) ExchangePublicToken(ctx context.Context, publicToken string) (*Consent, error) {
	if publicToken == "" {
		return nil, errors.New("plaid: no public_token to exchange")
	}
	var exchanged struct {
		AccessToken string `json:"access_token"`
		ItemID      string `json:"item_id"`
	}
	if err := p.do(ctx, "/item/public_token/exchange",
		map[string]any{"public_token": publicToken}, &exchanged); err != nil {
		return nil, err
	}
	if exchanged.AccessToken == "" || exchanged.ItemID == "" {
		return nil, errors.New("plaid: token exchange returned no access_token/item_id")
	}
	accounts, institution, err := p.accounts(ctx, exchanged.AccessToken, exchanged.ItemID)
	if err != nil {
		return nil, err
	}
	return &Consent{
		Reference:   exchanged.ItemID,
		Secret:      exchanged.AccessToken,
		Accounts:    accounts,
		Institution: institution,
	}, nil
}

// RevokeConsent deletes the Item this connection *is*. Plaid does not do that
// on its own: an Item left behind keeps its access token valid and keeps
// occupying one of the account's Items, which is a limited resource on the trial
// plan. A credential with no secret is a consent that never completed — no Item
// was ever created, and there is nothing to remove.
func (p *Plaid) RevokeConsent(ctx context.Context, cred Credential) error {
	if cred.Secret == "" {
		return nil
	}
	return p.do(ctx, "/item/remove", map[string]any{"access_token": cred.Secret}, nil)
}

// sessionPublicToken reads the newest completed session of a link_token and
// returns the public_token it produced. A session that has not finished yet is
// ErrConsentNotFinished — the user simply has not finished consenting, which the
// handler answers as a 400 and leaves the connection pending.
func (p *Plaid) sessionPublicToken(ctx context.Context, linkToken string) (string, error) {
	var out struct {
		LinkSessions []struct {
			Results struct {
				ItemAddResults []struct {
					PublicToken string `json:"public_token"`
				} `json:"item_add_results"`
			} `json:"results"`
		} `json:"link_sessions"`
	}
	if err := p.do(ctx, "/link/token/get", map[string]any{"link_token": linkToken}, &out); err != nil {
		return "", err
	}
	// Sessions come oldest-first, so walk backwards and take the newest one that
	// produced a token. `results.item_add_results` is the documented field — the
	// older `on_success` object only supports single-Item sessions.
	for i := len(out.LinkSessions) - 1; i >= 0; i-- {
		for _, r := range out.LinkSessions[i].Results.ItemAddResults {
			if r.PublicToken != "" {
				return r.PublicToken, nil
			}
		}
	}
	return "", fmt.Errorf("%w: plaid link session has not completed yet", ErrConsentNotFinished)
}

// accounts hydrates the Item's accounts. Plaid has no per-account detail call —
// /accounts/get returns everything, balances included.
//
// The Item also names the institution behind it, which is the one piece of the
// connection we cannot know before consent: a user who chose the bank in Plaid's
// own picker chose it there, not in our catalogue. It rides along here rather
// than costing a call of its own, and the name comes with it — a finalize must
// not fail over a display field, so an Item that reports neither leaves it zero.
func (p *Plaid) accounts(ctx context.Context, accessToken, itemID string) ([]Account, Institution, error) {
	var out struct {
		Accounts []plaidAccount `json:"accounts"`
		Item     struct {
			InstitutionID   string `json:"institution_id"`
			InstitutionName string `json:"institution_name"`
		} `json:"item"`
	}
	if err := p.do(ctx, "/accounts/get", map[string]any{"access_token": accessToken}, &out); err != nil {
		return nil, Institution{}, err
	}
	res := make([]Account, 0, len(out.Accounts))
	for _, a := range out.Accounts {
		if !plaidAccountTypes[a.Type] {
			continue
		}
		acc := Account{
			ExternalID:   a.AccountID,
			DisplayName:  plaidAccountName(a),
			CurrencyCode: plaidCurrency(a.Balances.ISOCurrencyCode, a.Balances.UnofficialCurrency),
		}
		// US accounts have no IBAN; Plaid exposes routing/account numbers under
		// the Auth product, which is a separate consent we do not request.
		if a.Balances.Current != nil {
			bal := *a.Balances.Current
			acc.Balance = &bal
		}
		res = append(res, acc)
	}
	if len(res) == 0 {
		return nil, Institution{}, fmt.Errorf("plaid: item %s has no depository or credit accounts", itemID)
	}
	inst := Institution{ID: out.Item.InstitutionID, Name: out.Item.InstitutionName}
	return res, inst, nil
}

// FetchStatementLines implements Provider with Plaid's date-range endpoint.
// Plaid prefers /transactions/sync, which SyncConnection implements — this
// exists for one-off backfills over an explicit window, and for callers that
// hold no cursor.
func (p *Plaid) FetchStatementLines(ctx context.Context, cred Credential, externalAccountID string, from, to time.Time) ([]StatementLine, error) {
	if cred.Secret == "" {
		return nil, errors.New("plaid: connection has no access token")
	}
	var res []StatementLine
	for page := 0; page < plaidMaxGetPages; page++ {
		body := map[string]any{
			"access_token": cred.Secret,
			"start_date":   from.Format("2006-01-02"),
			"end_date":     to.Format("2006-01-02"),
			"options": map[string]any{
				"count":  plaidTransactionPage,
				"offset": page * plaidTransactionPage,
			},
		}
		var out struct {
			TotalTransactions int       `json:"total_transactions"`
			Transactions      []plaidTx `json:"transactions"`
		}
		if err := p.do(ctx, "/transactions/get", body, &out); err != nil {
			return nil, err
		}
		for _, t := range out.Transactions {
			if t.AccountID != externalAccountID {
				continue
			}
			line, err := mapPlaidTx(t)
			if err != nil {
				return nil, err
			}
			res = append(res, line)
		}
		if len(out.Transactions) == 0 || (page+1)*plaidTransactionPage >= out.TotalTransactions {
			break
		}
	}
	return res, nil
}

// SyncConnection implements IncrementalProvider using /transactions/sync, the
// endpoint Plaid recommends: it returns what changed since the cursor rather
// than re-serving a window, and — crucially — it reports the transactions the
// bank deleted. A pending charge is replaced by a posted one under a new
// transaction_id, so without that `removed` list the inbox would keep the
// abandoned pending row forever.
func (p *Plaid) SyncConnection(ctx context.Context, cred Credential, cursor string) ([]ConnStatementLine, []string, string, error) {
	if cred.Secret == "" {
		return nil, nil, cursor, errors.New("plaid: connection has no access token")
	}
	var (
		lines   []ConnStatementLine
		removed []string
		nextCur = cursor
	)
	for page := 0; page < plaidMaxSyncPages; page++ {
		body := map[string]any{
			"access_token": cred.Secret,
			"count":        plaidTransactionPage,
			"cursor":       nextCur,
		}
		var out struct {
			Added    []plaidTx `json:"added"`
			Modified []plaidTx `json:"modified"`
			Removed  []struct {
				TransactionID string `json:"transaction_id"`
			} `json:"removed"`
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		}
		if err := p.do(ctx, "/transactions/sync", body, &out); err != nil {
			return nil, nil, cursor, err
		}
		// `modified` re-serves transactions we already staged: a pending charge
		// whose amount moves as the tip clears is "added" once and "modified"
		// after. Neither is deduplicated here — the staging upsert keys on
		// provider_tx_id, so replaying a line overwrites it, and processing
		// added-then-modified leaves the newest version in place.
		for _, batch := range [][]plaidTx{out.Added, out.Modified} {
			staged, err := mapPlaidTxLines(batch)
			if err != nil {
				return nil, nil, cursor, err
			}
			lines = append(lines, staged...)
		}
		for _, r := range out.Removed {
			if r.TransactionID != "" {
				removed = append(removed, r.TransactionID)
			}
		}
		nextCur = out.NextCursor
		if !out.HasMore {
			break
		}
	}
	return lines, removed, nextCur, nil
}

// plaidInstitution is the trimmed shape of an institution record.
type plaidInstitution struct {
	ID           string   `json:"institution_id"`
	Name         string   `json:"name"`
	Logo         *string  `json:"logo"`
	CountryCodes []string `json:"country_codes"`
}

func mapPlaidInstitutions(in []plaidInstitution) []Institution {
	res := make([]Institution, 0, len(in))
	for _, i := range in {
		inst := Institution{
			ID: i.ID, Name: i.Name,
			Countries: i.CountryCodes,
			TxDays:    plaidHistoryDays,
		}
		if i.Logo != nil {
			inst.LogoURL = *i.Logo
		}
		res = append(res, inst)
	}
	return res
}

// plaidAccount is the trimmed shape of an account from /accounts/get.
type plaidAccount struct {
	AccountID    string  `json:"account_id"`
	Name         string  `json:"name"`
	OfficialName *string `json:"official_name"`
	Mask         *string `json:"mask"`
	Type         string  `json:"type"`
	Balances     struct {
		Current            *decimal.Decimal `json:"current"`
		ISOCurrencyCode    *string          `json:"iso_currency_code"`
		UnofficialCurrency *string          `json:"unofficial_currency_code"`
	} `json:"balances"`
}

// plaidAccountName prefers the bank's own long name and appends the mask, which
// is what distinguishes two accounts at the same institution in the UI.
func plaidAccountName(a plaidAccount) string {
	name := firstNonEmpty(deref(a.OfficialName), a.Name, a.AccountID)
	if a.Mask != nil && *a.Mask != "" {
		return name + " ••" + *a.Mask
	}
	return name
}

// plaidTx is the trimmed shape of a transaction, shared by /transactions/sync and
// /transactions/get.
type plaidTx struct {
	TransactionID      string          `json:"transaction_id"`
	AccountID          string          `json:"account_id"`
	Amount             decimal.Decimal `json:"amount"`
	ISOCurrencyCode    *string         `json:"iso_currency_code"`
	UnofficialCurrency *string         `json:"unofficial_currency_code"`
	Date               string          `json:"date"`
	Name               string          `json:"name"`
	MerchantName       *string         `json:"merchant_name"`
	Pending            bool            `json:"pending"`
	CheckNumber        *string         `json:"check_number"`
	PaymentMeta        struct {
		ReferenceNumber *string `json:"reference_number"`
	} `json:"payment_meta"`
}

// mapPlaidTx converts a Plaid transaction into our provider-agnostic shape.
//
// The sign convention is inverted on purpose: Plaid reports a positive amount for
// money leaving the account, while StatementLine.Amount is positive for money
// coming in. Getting this backwards would silently flip every reconciliation, so
// it happens here and nowhere else.
func mapPlaidTx(t plaidTx) (StatementLine, error) {
	if t.TransactionID == "" {
		return StatementLine{}, errors.New("plaid: transaction without transaction_id — cannot dedup")
	}
	posted, err := time.Parse("2006-01-02", t.Date)
	if err != nil {
		return StatementLine{}, fmt.Errorf("plaid: transaction %s has unparseable date %q", t.TransactionID, t.Date)
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return StatementLine{}, fmt.Errorf("plaid: marshal transaction %s: %w", t.TransactionID, err)
	}
	counterparty := firstNonEmpty(deref(t.MerchantName), t.Name)
	return StatementLine{
		ProviderTxID: t.TransactionID,
		PostedAt:     posted,
		Amount:       t.Amount.Neg(),
		// bank_statement_lines.currency_code is NOT NULL, and Plaid leaves the
		// ISO code null for a handful of institutions that only supply an
		// unofficial one.
		CurrencyCode: plaidCurrency(t.ISOCurrencyCode, t.UnofficialCurrency),
		Description:  t.Name,
		Counterparty: counterparty,
		Reference:    firstNonEmpty(deref(t.PaymentMeta.ReferenceNumber), deref(t.CheckNumber)),
		Raw:          raw,
	}, nil
}

// mapPlaidTxLines converts a batch of Plaid transactions into staging lines,
// each tagged with the account it was reported under — a sync covers whole Items,
// so the account id has to ride along with the line rather than hang off the call.
func mapPlaidTxLines(txs []plaidTx) ([]ConnStatementLine, error) {
	out := make([]ConnStatementLine, 0, len(txs))
	for _, t := range txs {
		line, err := mapPlaidTx(t)
		if err != nil {
			return nil, err
		}
		out = append(out, ConnStatementLine{ExternalAccountID: t.AccountID, StatementLine: line})
	}
	return out, nil
}

// plaidCurrency picks the currency code for a Plaid object, falling back to USD
// because every account we accept comes from a US/CA institution and the column
// is NOT NULL.
func plaidCurrency(iso, unofficial *string) string {
	return firstNonEmpty(deref(iso), deref(unofficial), "USD")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// plaidErrInvalidInstitution is what Plaid answers when a link token names an
// institution the account is not allowed to pin. See CreateSession.
const plaidErrInvalidInstitution = "INVALID_INSTITUTION"

// PlaidError is an error envelope returned by the Plaid API. Callers can match on
// Code (ITEM_LOGIN_REQUIRED, INVALID_ACCESS_TOKEN, …) to decide whether a
// connection needs the user to re-authenticate.
type PlaidError struct {
	Type       string `json:"error_type"`
	Code       string `json:"error_code"`
	Message    string `json:"error_message"`
	Display    string `json:"display_message"`
	StatusCode int    `json:"-"`
}

func (e *PlaidError) Error() string {
	msg := firstNonEmpty(e.Display, e.Message, e.Code)
	return fmt.Sprintf("plaid %s: %s (%s, %d)", e.Code, msg, e.Type, e.StatusCode)
}

// plaidConsentBrokenCodes are the Plaid errors that mean the Item we hold no
// longer works and the user is the only one who can fix it. They are the codes
// the bank's own events report — an ITEM_LOGIN_REQUIRED the user has not got
// round to repairing, or access withdrawn at the bank — and the reason a sync
// that meets one takes the connection out of service rather than retrying.
//
// Everything else Plaid can answer (rate limits, an outage, a timeout) leaves
// the Item intact and is a bad sync, not a dead feed.
var plaidConsentBrokenCodes = map[string]bool{
	"ITEM_LOGIN_REQUIRED":     true, // the bank wants the user to sign in again
	"ITEM_LOCKED":             true, // the bank has locked the Item
	"USER_PERMISSION_REVOKED": true, // the user withdrew access
	"ACCESS_NOT_GRANTED":      true, // consent no longer covers what we ask for
	"USER_SETUP_REQUIRED":     true, // the user has an action outstanding at the bank
	"INVALID_ACCESS_TOKEN":    true, // the Item no longer exists
	"ITEM_NOT_FOUND":          true,
}

// ConsentBroken implements ConsentDiagnoser.
func (p *Plaid) ConsentBroken(err error) bool {
	var apiErr *PlaidError
	return errors.As(err, &apiErr) && plaidConsentBrokenCodes[apiErr.Code]
}

// do performs a POST against the Plaid API. Every endpoint we use is a POST with
// client_id/secret in the body — Plaid has no bearer token and no session to
// refresh, so unlike GoCardless there is no token cache here.
// Retrying a call is only ever safe when asking twice cannot change anything.
// The stall this exists for is not Plaid's: a local content filter or VPN can
// hold a flow's packets while it decides, and Go then reports a TLS handshake
// timeout with TCP already up. Nothing about the request is wrong, and the same
// bytes succeed a moment later — so the retry is bounded, and only reads and
// cursor syncs are eligible.
const (
	plaidRetryAttempts = 2
	plaidRetryBackoff  = 300 * time.Millisecond
)

// retryablePlaidCall reports whether a call that failed on the wire may simply
// be sent again. `/item/public_token/exchange` is deliberately absent: it creates
// the Item, so a repeat would mint a second one — and a repeat whose first
// attempt actually succeeded would come back an error, losing the consent the
// user had just given. Creation is worth far more than one saved round trip.
func retryablePlaidCall(path string) bool {
	switch path {
	case "/accounts/get", "/link/token/get", "/transactions/sync", "/transactions/get",
		// Keyed by key_id and free of side effects, so asking twice is harmless —
		// and the stall above is exactly what this call is most exposed to, since
		// it runs while Plaid is holding a delivery open waiting for our answer.
		// Failing it means answering 401 to a delivery that was genuine.
		"/webhook_verification_key/get":
		return true
	}
	return strings.HasPrefix(path, "/institutions/")
}

func (p *Plaid) do(ctx context.Context, path string, body map[string]any, out any) error {
	payload := map[string]any{"client_id": p.clientID, "secret": p.secret}
	for k, v := range body {
		payload[k] = v
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	attempts := 1
	if retryablePlaidCall(path) {
		attempts = plaidRetryAttempts
	}
	var (
		resp *http.Response
		raw  []byte
	)
	for attempt := 1; ; attempt++ {
		resp, raw, err = p.send(ctx, path, encoded)
		if err == nil || attempt == attempts {
			break
		}
		if cerr := sleepCtx(ctx, plaidRetryBackoff); cerr != nil {
			return cerr
		}
	}
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		apiErr := &PlaidError{StatusCode: resp.StatusCode}
		if json.Unmarshal(raw, apiErr) != nil || apiErr.Code == "" {
			// A status but no envelope: Plaid answered with something that is not
			// one of its errors — a proxy's page, or a truncated body. The status
			// alone still tells the caller what happened, and the body is carried
			// because it is often the only explanation available.
			return fmt.Errorf("plaid %s: %d %s", path, resp.StatusCode, errBody(raw))
		}
		return apiErr
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// send is one attempt: a fresh request, because the previous attempt consumed
// the body reader. The response body is read here rather than by the caller
// because both the status and the payload are needed to tell an API refusal
// from a failure on the wire.
func (p *Plaid) send(ctx context.Context, path string, encoded []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, nil, err
	}
	return resp, raw, nil
}

// sleepCtx waits out the backoff, and gives up the moment the caller's context
// ends: a request the caller has already abandoned must not be held open by our
// own retry.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
