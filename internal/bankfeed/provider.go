// Package bankfeed abstracts Open Banking aggregators (Plaid, GoCardless Bank
// Account Data, TrueLayer, Salt Edge, …) behind a small Provider interface.
//
// Adding a new aggregator means:
//  1. Implement Provider for it.
//  2. Register it from main.go via Registry.Register(name, instance).
//
// No handler / repository code needs to change — the `/bank-feeds/*` routes
// look providers up by name through the Registry.
package bankfeed

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// Provider names we know about. The actual list of available providers depends
// on which adapters have credentials configured at boot.
const (
	ProviderGoCardlessBAD = "gocardless_bad" // GoCardless Bank Account Data (ex-Nordigen) — PSD2, EU/UK
	ProviderPlaid         = "plaid"          // Plaid — US/CA, Hosted Link consent flow
)

// ErrProviderNotRegistered is returned by Registry.Get when no adapter matches
// the requested name. Handlers translate it into HTTP 404.
var ErrProviderNotRegistered = errors.New("bank feed provider not registered")

// ErrConsentNotFinished is returned when a provider is asked for the result of a
// consent the user has not completed at the bank yet. Nothing has gone wrong —
// the session is still open — so handlers answer it as a 4xx and leave the
// connection as it was, rather than recording a failure that never happened.
var ErrConsentNotFinished = errors.New("bank feed consent has not been completed yet")

// Institution represents one bank/financial institution the user can pick from
// during the consent flow.
type Institution struct {
	ID        string   `json:"ID"`
	Name      string   `json:"Name"`
	BIC       string   `json:"BIC,omitempty"`
	Countries []string `json:"Countries,omitempty"`
	LogoURL   string   `json:"LogoURL,omitempty"`
	TxDays    int      `json:"TransactionTotalDays,omitempty"` // how far back the provider can fetch
}

// SessionRequest is what a handler passes to Provider.CreateSession when the
// user has picked an institution and we want to start the consent flow.
type SessionRequest struct {
	InstitutionID string
	Country       string // the institution's country, where the provider needs it
	RedirectURL   string // where the provider sends the browser after consent
	Reference     string // our own connection_id — echoed back for correlation
}

// Session is the result of starting consent: the URL to redirect the user to
// plus the opaque reference we'll hand back to FinalizeSession once they return.
type Session struct {
	ExternalReference string
	AuthURL           string
	ExpiresAt         *time.Time
	// InstitutionPinned reports whether the session opens on the institution the
	// caller asked for. It is false when the provider would not accept a pinned
	// one and fell back to its own picker (see Plaid.CreateSession), in which
	// case the user picks the bank inside the provider's UI and the connection
	// is re-labelled with what they chose once consent completes
	// (Consent.Institution).
	InstitutionPinned bool
}

// Account is a bank account discovered after consent is granted.
type Account struct {
	ExternalID   string
	DisplayName  string
	IBAN         string
	CurrencyCode string
	Balance      *decimal.Decimal
}

// Credential is the per-connection identity a provider needs on every call
// after consent. GoCardless carries nothing in Secret — its secret_id/secret_key
// are application-level and live on the adapter — while Plaid needs the Item's
// access_token on every request, so it travels here (decrypted for the duration
// of the call, never logged).
type Credential struct {
	Reference string // durable provider-side id: Plaid item_id, GoCardless requisition_id
	Secret    string // per-connection secret; empty when the provider has none
}

// Consent is what a finished consent flow yields: the durable identity of the
// connection (which may differ from the throwaway reference that started it —
// Plaid trades a link_token for an item_id + access_token), and the accounts the
// user granted access to.
type Consent struct {
	Reference string
	Secret    string
	Accounts  []Account
	// Institution is the institution the connection actually ended up at. It is
	// normally the one the caller asked for and differs when the provider's own
	// picker was used — which is why the connection's identity is taken from
	// here rather than from the request that started the flow, or a row can end
	// up named after a bank the user never connected. Zero for providers that do
	// not report one, and the caller then keeps what it already has.
	Institution Institution
}

// StatementLine is a provider-agnostic ledger entry pulled from an upstream
// account. Sign convention: positive = credit (money in), negative = debit.
type StatementLine struct {
	ProviderTxID string
	PostedAt     time.Time
	Amount       decimal.Decimal
	CurrencyCode string
	Description  string
	Counterparty string
	Reference    string
	Raw          []byte // original provider payload, stored as JSONB for audit
}

// ConnStatementLine is a StatementLine plus the upstream account it belongs to.
// Providers that sync a whole connection at once have no per-account call to
// hang the account id off, so it rides along with the line instead.
type ConnStatementLine struct {
	ExternalAccountID string
	StatementLine
}

// Provider is the contract every aggregator adapter implements.
type Provider interface {
	Name() string
	// ListInstitutions returns the provider's bank catalogue. `query` is a
	// free-text search term: providers with huge catalogues (Plaid lists ~12k
	// US institutions) must search upstream, the rest may filter locally.
	ListInstitutions(ctx context.Context, country, query string) ([]Institution, error)
	CreateSession(ctx context.Context, req SessionRequest) (*Session, error)
	// FinalizeSession is called after the user returns from the bank — the
	// adapter inspects the reference and returns the durable connection
	// identity plus the accounts now available.
	FinalizeSession(ctx context.Context, cred Credential) (*Consent, error)
	FetchStatementLines(ctx context.Context, cred Credential, externalAccountID string, from, to time.Time) ([]StatementLine, error)
}

// IncrementalProvider is implemented by adapters whose upstream API syncs a
// whole consent by cursor rather than a date window per account — Plaid's
// /transactions/sync works this way, and its cursor is per Item, not per
// account, so a per-account date range cannot express it. Handlers type-assert
// for this and prefer it over FetchStatementLines when it is available.
type IncrementalProvider interface {
	// SyncConnection returns everything that changed since cursor, the ids of
	// transactions the bank has deleted, and the cursor to resume from.
	SyncConnection(ctx context.Context, cred Credential, cursor string) (lines []ConnStatementLine, removed []string, nextCursor string, err error)
}

// PublicTokenExchanger is implemented by adapters that can complete a consent
// from the public token alone. A provider can hand that token to us out of band
// — Plaid's SESSION_FINISHED webhook delivers it without waiting for the browser
// to come back — and the session is then finished by exchanging it.
type PublicTokenExchanger interface {
	// ExchangePublicToken trades a public token for the durable connection
	// identity plus the accounts behind it.
	ExchangePublicToken(ctx context.Context, publicToken string) (*Consent, error)
}

// ReconsentProvider is implemented by adapters whose consent can be repaired in
// place: the bank has invalidated the connection (Plaid: ITEM_LOGIN_REQUIRED),
// but the durable identity still exists and only the authentication behind it
// needs renewing. Running the first-time flow again would leave the tenant with
// two consents on one account and a duplicated history, so a repair happens on
// the connection that broke.
type ReconsentProvider interface {
	// CreateReconsentSession starts a repair session for an existing consent.
	CreateReconsentSession(ctx context.Context, cred Credential, req SessionRequest) (*Session, error)
	// CompleteReconsent re-reads what the repaired consent exposes. No new
	// credential is issued — the stored one is still the one to use.
	CompleteReconsent(ctx context.Context, cred Credential) (*Consent, error)
}

// InstitutionPicker is implemented by adapters whose consent flow can run
// without being told which institution to use, because the provider's own UI
// asks the user. Plaid's Hosted Link is the full Link flow, so it qualifies —
// and on an account that refuses a pinned institution that picker is the only
// way in (see Plaid.CreateSession). A provider whose API demands an institution
// (GoCardless) does not implement this, and the handler keeps requiring one.
type InstitutionPicker interface {
	// PicksInstitution reports that InstitutionID may be left empty on
	// CreateSession.
	PicksInstitution() bool
}

// WebhookVerifier is implemented by adapters whose notifications arrive without
// a tenant header and therefore have to authenticate themselves. Plaid signs
// each delivery with a JWS; an unverified webhook must never be allowed to
// change anything, because the endpoint it arrives at is public.
type WebhookVerifier interface {
	// VerifyWebhook checks the signature header against the raw request body. It
	// fails for a signature that is missing, malformed, stale, or simply not the
	// provider's.
	VerifyWebhook(ctx context.Context, header string, body []byte) error
}

// ConsentDiagnoser is implemented by adapters that can tell a consent the bank
// has stopped honouring from a call that merely failed. The distinction decides
// what a failed sync means: a broken consent takes the connection out of service
// until the user re-authenticates, while a timeout, a rate limit or a provider
// outage is one bad sync of a feed that still works, and killing the feed over it
// would cost the user a repair they do not need.
//
// An adapter that does not implement this is treated as unable to tell, so its
// failures stay transient (see ConsentBroken).
type ConsentDiagnoser interface {
	// ConsentBroken reports whether err means the credential we hold is no longer
	// accepted at the bank, so nothing but a fresh consent from the user will make
	// this connection work again.
	ConsentBroken(err error) bool
}

// ConsentBroken asks a provider whether a failed call means the connection needs
// the user. A provider that cannot tell — one that does not implement
// ConsentDiagnoser — answers false, which leaves the connection running: a feed
// wrongly marked broken is a repair the user has to do, while a broken feed
// wrongly left running only keeps failing visibly, with the reason on the row.
func ConsentBroken(p Provider, err error) bool {
	if err == nil {
		return false
	}
	d, ok := p.(ConsentDiagnoser)
	return ok && d.ConsentBroken(err)
}

// ConsentRevoker is implemented by adapters that can be told a consent is over,
// because the user disconnected the bank here. Without it the identity we hold
// outlives the connection: a Plaid Item keeps counting against the account's
// Item limit and its access token keeps working, and a GoCardless requisition
// keeps its access to the accounts — none of which a user who just pressed
// Disconnect expects to still exist.
//
// A provider that does not implement this is simply not told. The connection is
// removed here regardless: a disconnect the provider can veto is not a
// disconnect, and leaving the row behind would trap the user behind an outage.
type ConsentRevoker interface {
	// RevokeConsent ends the consent behind a credential. A credential that was
	// never completed — a session that produced no durable identity and no
	// secret — has nothing to revoke and answers nil rather than an error.
	RevokeConsent(ctx context.Context, cred Credential) error
}

// Registry is a goroutine-safe lookup for Provider instances.
type Registry struct {
	mu    sync.RWMutex
	items map[string]Provider
}

// NewRegistry returns an empty registry. Populate from main.go after reading
// config so the test suite can inject mocks.
func NewRegistry() *Registry { return &Registry{items: map[string]Provider{}} }

// Register installs a provider under its declared Name. Overwriting an entry
// is allowed so tests can swap implementations.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	r.items[p.Name()] = p
	r.mu.Unlock()
}

// Get returns the adapter for name or ErrProviderNotRegistered.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	p, ok := r.items[name]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrProviderNotRegistered
	}
	return p, nil
}

// All returns the registered adapters ordered by slug. A caller that only needs
// the names reads them off the adapters; what it cannot get from a name is which
// optional flows each one implements, and that is what this exists for.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	out := make([]Provider, 0, len(r.items))
	for _, p := range r.items {
		out = append(out, p)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// errBodyMaxBytes bounds how much of a provider's response body an error message
// carries. The body is worth having — for GoCardless it is the only explanation
// of a refusal — but it is also what lands in bank_feed_connections.last_error
// and in the log, and a provider can answer with a proxy's HTML page instead of
// JSON. The beginning is kept, because that is where the reason is.
const errBodyMaxBytes = 512

// errBody renders a response body for an error message, shortened so that one
// failed call cannot carry a megabyte into the database. The cut lands on a rune
// boundary because last_error is a text column: half a UTF-8 sequence is not
// valid text, and Postgres would refuse the very write that records the failure.
func errBody(b []byte) string {
	if len(b) <= errBodyMaxBytes {
		return string(b)
	}
	cut := errBodyMaxBytes
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	return string(b[:cut]) + "…(truncated)"
}
