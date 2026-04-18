// Package bankfeed abstracts Open Banking aggregators (GoCardless Bank Account
// Data, Plaid, TrueLayer, Salt Edge, …) behind a small Provider interface.
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

	"github.com/shopspring/decimal"
)

// Provider names we know about. The actual list of available providers depends
// on which adapters have credentials configured at boot.
const (
	ProviderGoCardlessBAD = "gocardless_bad" // GoCardless Bank Account Data (ex-Nordigen) — PSD2, free tier EU
	ProviderPlaid         = "plaid"          // stub for future integration
)

// ErrProviderNotRegistered is returned by Registry.Get when no adapter matches
// the requested name. Handlers translate it into HTTP 404.
var ErrProviderNotRegistered = errors.New("bank feed provider not registered")

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
	RedirectURL   string // where the provider sends the browser after consent
	Reference     string // our own connection_id — echoed back for correlation
}

// Session is the result of starting consent: the URL to redirect the user to
// plus the opaque reference we'll hand back to FinalizeSession once they return.
type Session struct {
	ExternalReference string
	AuthURL           string
	ExpiresAt         *time.Time
}

// Account is a bank account discovered after consent is granted.
type Account struct {
	ExternalID   string
	DisplayName  string
	IBAN         string
	CurrencyCode string
	Balance      *decimal.Decimal
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

// Provider is the contract every aggregator adapter implements.
type Provider interface {
	Name() string
	ListInstitutions(ctx context.Context, country string) ([]Institution, error)
	CreateSession(ctx context.Context, req SessionRequest) (*Session, error)
	// FinalizeSession is called after the user returns from the bank — the
	// adapter inspects the reference and returns the accounts now available.
	FinalizeSession(ctx context.Context, externalReference string) ([]Account, error)
	FetchStatementLines(ctx context.Context, externalAccountID string, from, to time.Time) ([]StatementLine, error)
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

// Names returns the sorted list of registered provider slugs.
func (r *Registry) Names() []string {
	r.mu.RLock()
	out := make([]string, 0, len(r.items))
	for k := range r.items {
		out = append(out, k)
	}
	r.mu.RUnlock()
	sort.Strings(out)
	return out
}
