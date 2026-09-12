package bankfeed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsentBroken pins the rule that keeps a working feed running: only an
// error the provider blames on the credential takes a connection out of service.
// Everything else — a rate limit, an outage, a provider that cannot say — leaves
// it alone, because the cost of the two mistakes is not symmetric: a feed wrongly
// killed costs the user a repair that fixes nothing, while a feed wrongly left
// running merely keeps failing, visibly, with the reason on the row.
func TestConsentBroken(t *testing.T) {
	t.Parallel()

	plaid := NewPlaid("id", "secret", "goxero", false)
	cardless := NewGoCardless("secret-id", "secret-key")

	cases := []struct {
		name     string
		provider Provider
		err      error
		want     bool
	}{
		{"plaid: the user must sign in again", plaid,
			&PlaidError{Code: "ITEM_LOGIN_REQUIRED"}, true},
		{"plaid: access withdrawn at the bank", plaid,
			&PlaidError{Code: "USER_PERMISSION_REVOKED"}, true},
		{"plaid: the Item is gone", plaid,
			&PlaidError{Code: "INVALID_ACCESS_TOKEN"}, true},
		{"plaid: a rate limit", plaid,
			&PlaidError{Code: "RATE_LIMIT_EXCEEDED"}, false},
		{"plaid: an outage on their side", plaid,
			&PlaidError{Code: "INTERNAL_SERVER_ERROR"}, false},
		{"plaid: a failure with no envelope", plaid,
			errors.New("dial tcp: i/o timeout"), false},

		{"gocardless: the access no longer covers the call", cardless,
			&GoCardlessError{StatusCode: 403, Subject: "GET /accounts/x/transactions/"}, true},
		{"gocardless: the requisition is gone", cardless,
			&GoCardlessError{StatusCode: 404, Subject: "GET /requisitions/x/"}, true},
		{"gocardless: rate limited", cardless,
			&GoCardlessError{StatusCode: 429, Subject: "GET /accounts/x/transactions/"}, false},
		{"gocardless: an outage on their side", cardless,
			&GoCardlessError{StatusCode: 503, Subject: "GET /accounts/x/transactions/"}, false},

		{"a provider that cannot tell", undiagnosingProvider{}, errors.New("anything at all"), false},
		{"no error at all", plaid, nil, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ConsentBroken(tc.provider, tc.err))
		})
	}
}

// TestGoCardlessError_KeepsWhatItWasTold — the body is the only explanation
// GoCardless gives, so the message has to carry it and the status that decided
// the diagnosis has to survive for ConsentBroken to read.
func TestGoCardlessError_KeepsWhatItWasTold(t *testing.T) {
	t.Parallel()
	err := error(&GoCardlessError{StatusCode: 403, Body: `{"detail":"not authorised"}`, Subject: "GET /accounts/x/"})
	require.Contains(t, err.Error(), "403")
	require.Contains(t, err.Error(), "not authorised")

	var apiErr *GoCardlessError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, 403, apiErr.StatusCode)
}

// TestErrBody — a provider's response body ends up in last_error and in the log,
// so one failed call must not be able to carry a proxy's megabyte of HTML into
// the database. The cut is also where a UTF-8 sequence can be split, and half a
// character is not valid text for the column that records the failure.
func TestErrBody(t *testing.T) {
	t.Parallel()

	short := []byte(`{"detail":"not authorised"}`)
	assert.Equal(t, string(short), errBody(short), "a body that fits is carried whole")

	// Three-byte runes, so the limit falls inside a character rather than between
	// two — a cut that ignores runes is what produces the invalid text.
	wide := []byte(strings.Repeat("€", 400))
	got := errBody(wide)
	assert.True(t, utf8.ValidString(got), "the cut lands on a rune boundary")
	assert.True(t, strings.HasPrefix(got, strings.Repeat("€", 170)), "the beginning is what is kept")
	assert.True(t, strings.HasSuffix(got, "…(truncated)"))
	assert.Less(t, len(got), len(wide), "a long body is shortened")
}

// undiagnosingProvider is an adapter that implements only the required contract,
// which is what makes it the control for ConsentDiagnoser: the absence of the
// optional interface is the point.
type undiagnosingProvider struct{}

func (undiagnosingProvider) Name() string { return "undiagnosing" }

func (undiagnosingProvider) ListInstitutions(context.Context, string, string) ([]Institution, error) {
	return nil, nil
}

func (undiagnosingProvider) CreateSession(context.Context, SessionRequest) (*Session, error) {
	return nil, nil
}

func (undiagnosingProvider) FinalizeSession(context.Context, Credential) (*Consent, error) {
	return nil, nil
}

func (undiagnosingProvider) FetchStatementLines(context.Context, Credential, string, time.Time, time.Time) ([]StatementLine, error) {
	return nil, nil
}
