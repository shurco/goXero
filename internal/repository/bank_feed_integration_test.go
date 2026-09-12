package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

// newFeedConnection stages a PENDING connection the way CreateConnection does.
func newFeedConnection(t *testing.T, repos *Repositories, provider string) *models.BankFeedConnection {
	t.Helper()
	conn := &models.BankFeedConnection{
		Provider:        provider,
		Status:          models.BankFeedStatusPending,
		InstitutionID:   "ins_56",
		InstitutionName: "Chase",
		Country:         "US",
	}
	require.NoError(t, repos.BankFeeds.CreateConnection(context.Background(), seedDemoOrgID, conn))
	return conn
}

// TestIntegration_BankFeed_ConsentRoundTrip covers the storage contract a
// per-connection secret depends on: the sealed blob survives a write/read cycle
// byte for byte, and it is what the model carries — not the plaintext.
func TestIntegration_BankFeed_ConsentRoundTrip(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	// Ciphertext, deliberately not valid UTF-8: a bytea column that silently
	// round-tripped through text would corrupt a real GCM blob.
	sealed := []byte{0x00, 0xff, 0x10, 0x9c, 0x00, 0x7f, 0xfe}

	// First the session the user is sent off to finish, then what it produced.
	require.NoError(t, repos.BankFeeds.SetConnectionSession(ctx, seedDemoOrgID, conn.ConnectionID,
		"link-sandbox-1", "https://secure.plaid.com/hl/abc"))
	require.NoError(t, repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, conn.ConnectionID,
		"item-42", sealed))

	got, err := repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, "item-42", got.ExternalReference, "the durable id replaces the throwaway session")
	assert.Equal(t, sealed, got.ExternalSecret)
	assert.Empty(t, got.SessionRef, "the session is over and must not be left to be replayed")
	assert.Empty(t, got.AuthURL, "nor the link that opened it")
	assert.Equal(t, "US", got.Country, "the country survives so a repair can name it again")

	// The list path must read the secret too: the background poller syncs from
	// ListLinkedConnections and needs it to authenticate.
	list, err := repos.BankFeeds.ListConnections(ctx, seedDemoOrgID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, sealed, list[0].ExternalSecret)
}

// TestIntegration_BankFeed_ConsentKeepsExistingSecret — CreateConnection and
// providers with no secret (GoCardless) both call SetConnectionConsent with a
// nil blob. That must not wipe a token that is already stored.
func TestIntegration_BankFeed_ConsentKeepsExistingSecret(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	sealed := []byte{0x01, 0x02, 0x03}
	require.NoError(t, repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, conn.ConnectionID,
		"item-42", sealed))

	// A later call with nothing to seal, and an empty reference to leave alone.
	require.NoError(t, repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, conn.ConnectionID,
		"", nil))

	got, err := repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, "item-42", got.ExternalReference)
	assert.Equal(t, sealed, got.ExternalSecret)

	// A missing connection is a not-found, not a silent success.
	err = repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, uuid.New(), "item-x", nil)
	require.ErrorIs(t, err, ErrNotFound)
}

// TestIntegration_BankFeed_ProvisionLedgerAccount — a finished consent stages the
// accounts the user granted, but the bank accounts screen lists ledger accounts,
// so provisioning is what actually makes a connected bank visible there.
func TestIntegration_BankFeed_ProvisionLedgerAccount(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	fa := &models.BankFeedAccount{
		ConnectionID:      conn.ConnectionID,
		ExternalAccountID: "acc-chk",
		DisplayName:       "Plaid Gold Standard Checking",
		CurrencyCode:      "USD",
	}
	require.NoError(t, repos.BankFeeds.UpsertAccount(ctx, seedDemoOrgID, fa))

	accountID, err := repos.BankFeeds.ProvisionLedgerAccount(ctx, seedDemoOrgID, fa.FeedAccountID)
	require.NoError(t, err)

	acct, err := repos.Accounts.GetByID(ctx, seedDemoOrgID, accountID)
	require.NoError(t, err)
	assert.Equal(t, "Plaid Gold Standard Checking", acct.Name)
	assert.Equal(t, models.AccountTypeBank, acct.Type)
	assert.Equal(t, "USD", acct.CurrencyCode)
	assert.Equal(t, "ACTIVE", acct.Status)
	assert.Equal(t, models.AccountClassAsset, acct.Class)
	// The demo chart's own bank series is 090/091 — a new bank account continues
	// it rather than inventing a range of its own.
	assert.Equal(t, "092", acct.Code)

	// Bound, which is what lets the inbox post under it.
	got, err := repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	require.Len(t, got.Accounts, 1)
	require.NotNil(t, got.Accounts[0].AccountID)
	assert.Equal(t, accountID, *got.Accounts[0].AccountID)

	// Twice is still one account: the webhook and the browser both finish the
	// same consent, and a repair finishes it again.
	again, err := repos.BankFeeds.ProvisionLedgerAccount(ctx, seedDemoOrgID, fa.FeedAccountID)
	require.NoError(t, err)
	assert.Equal(t, accountID, again)

	bankAccounts, err := repos.Accounts.List(ctx, seedDemoOrgID, AccountFilter{Type: models.AccountTypeBank})
	require.NoError(t, err)
	assert.Len(t, bankAccounts, 3, "090, 091 and the one the feed provisioned")

	// Another tenant naming the same feed account id gets nothing: provisioning
	// writes into an organisation's chart, so the lookup that finds the feed
	// account has to be scoped to the caller's organisation first.
	other := &models.Organisation{Name: "Other-" + uuid.NewString()[:6], BaseCurrency: "USD"}
	require.NoError(t, repos.Organisations.Create(ctx, other))
	_, err = repos.BankFeeds.ProvisionLedgerAccount(ctx, other.OrganisationID, fa.FeedAccountID)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = repos.BankFeeds.ProvisionLedgerAccount(ctx, seedDemoOrgID, uuid.New())
	assert.ErrorIs(t, err, ErrNotFound, "an unknown feed account is not a server fault")
}

// TestIntegration_BankFeed_SyncCursor round-trips the incremental sync position.
func TestIntegration_BankFeed_SyncCursor(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	got, err := repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Empty(t, got.SyncCursor, "a fresh connection has no cursor")

	require.NoError(t, repos.BankFeeds.SetSyncCursor(ctx, seedDemoOrgID, conn.ConnectionID, "cursor-1"))
	got, err = repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, "cursor-1", got.SyncCursor)

	// An empty cursor clears it, which is how a re-consent restarts history.
	require.NoError(t, repos.BankFeeds.SetSyncCursor(ctx, seedDemoOrgID, conn.ConnectionID, ""))
	got, err = repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Empty(t, got.SyncCursor)
}

// TestIntegration_BankFeed_ApplyRemovedLines covers the `removed` half of
// incremental sync: Plaid replaces a pending transaction with a posted one under
// a new id, and the abandoned row has to go — but only while it is still NEW.
// A line the user has already coded is part of the books: it survives, marked as
// withdrawn rather than deleted out from under the ledger.
func TestIntegration_BankFeed_ApplyRemovedLines(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	fa := &models.BankFeedAccount{
		ConnectionID:      conn.ConnectionID,
		ExternalAccountID: "acc-chk",
		DisplayName:       "Plaid Checking",
		CurrencyCode:      "USD",
	}
	require.NoError(t, repos.BankFeeds.UpsertAccount(ctx, seedDemoOrgID, fa))

	stage := func(txID string) {
		t.Helper()
		row := &models.BankStatementLine{
			FeedAccountID: &fa.FeedAccountID,
			Source:        models.StatementLineSourceFeed,
			ProviderTxID:  txID,
			PostedAt:      time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Amount:        decimal.RequireFromString("-12.34"),
			CurrencyCode:  "USD",
			Description:   "COFFEE",
		}
		inserted, err := repos.BankFeeds.UpsertStatementLine(ctx, seedDemoOrgID, fa.FeedAccountID, row, []byte(`{}`))
		require.NoError(t, err)
		require.True(t, inserted)
	}
	for _, id := range []string{"tx-pending", "tx-posted", "tx-coded"} {
		stage(id)
	}

	// One of the lines has already been coded by the user.
	_, err := pool.Exec(ctx,
		`UPDATE bank_statement_lines SET status = $1 WHERE provider_tx_id = 'tx-coded'`,
		models.BankFeedLineStatusImported)
	require.NoError(t, err)

	// A second connection's line carrying an id from the same `removed` batch must
	// not be touched: the delete is scoped to the connection being synced.
	other := newFeedConnection(t, repos, "plaid")
	otherFA := &models.BankFeedAccount{
		ConnectionID:      other.ConnectionID,
		ExternalAccountID: "acc-other",
		CurrencyCode:      "USD",
	}
	require.NoError(t, repos.BankFeeds.UpsertAccount(ctx, seedDemoOrgID, otherFA))
	otherRow := &models.BankStatementLine{
		FeedAccountID: &otherFA.FeedAccountID,
		Source:        models.StatementLineSourceFeed,
		ProviderTxID:  "tx-other-account",
		PostedAt:      time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Amount:        decimal.RequireFromString("5"),
		CurrencyCode:  "USD",
	}
	_, err = repos.BankFeeds.UpsertStatementLine(ctx, seedDemoOrgID, otherFA.FeedAccountID, otherRow, nil)
	require.NoError(t, err)

	// An empty list is a no-op rather than a full-table delete.
	removed, withdrawn, err := repos.BankFeeds.ApplyRemovedLines(ctx, seedDemoOrgID, conn.ConnectionID, nil)
	require.NoError(t, err)
	assert.Zero(t, removed)
	assert.Zero(t, withdrawn)

	removed, withdrawn, err = repos.BankFeeds.ApplyRemovedLines(ctx, seedDemoOrgID, conn.ConnectionID,
		[]string{"tx-pending", "tx-coded", "tx-other-account", "tx-unknown"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, removed, "only the untouched line on this connection goes")
	assert.EqualValues(t, 1, withdrawn, "the coded line stays and is marked instead")

	// The withdrawal is a notice, not a deletion: the booked line is still there
	// and now says the bank has taken it back.
	var marked bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT upstream_removed_at IS NOT NULL FROM bank_statement_lines WHERE provider_tx_id = 'tx-coded'`,
	).Scan(&marked))
	assert.True(t, marked)

	// Running the same batch again changes nothing: the notice is already up.
	removed, withdrawn, err = repos.BankFeeds.ApplyRemovedLines(ctx, seedDemoOrgID, conn.ConnectionID,
		[]string{"tx-pending", "tx-coded"})
	require.NoError(t, err)
	assert.Zero(t, removed)
	assert.Zero(t, withdrawn)

	remaining := func(feedAccountID uuid.UUID) []string {
		t.Helper()
		rows, err := pool.Query(ctx,
			`SELECT provider_tx_id FROM bank_statement_lines
			 WHERE feed_account_id = $1 ORDER BY provider_tx_id`, feedAccountID)
		require.NoError(t, err)
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			require.NoError(t, rows.Scan(&id))
			out = append(out, id)
		}
		require.NoError(t, rows.Err())
		return out
	}
	assert.Equal(t, []string{"tx-coded", "tx-posted"}, remaining(fa.FeedAccountID))
	assert.Equal(t, []string{"tx-other-account"}, remaining(otherFA.FeedAccountID))
}

// TestIntegration_BankFeed_ConnectionLookups covers the two lookups an
// out-of-band notification needs. A webhook arrives with no tenant header, so
// the connection has to be found from the id in the payload — and the session
// one must stop answering once the session is over, or a replayed delivery would
// look like work still to do.
func TestIntegration_BankFeed_ConnectionLookups(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	require.NoError(t, repos.BankFeeds.SetConnectionSession(ctx, seedDemoOrgID, conn.ConnectionID,
		"link-sandbox-9", "https://secure.plaid.com/hl/xyz"))

	orgID, connID, err := repos.BankFeeds.ConnectionBySessionRef(ctx, "link-sandbox-9")
	require.NoError(t, err)
	assert.Equal(t, seedDemoOrgID, orgID)
	assert.Equal(t, conn.ConnectionID, connID)

	// An unknown session is a not-found, not somebody else's connection.
	_, _, err = repos.BankFeeds.ConnectionBySessionRef(ctx, "link-sandbox-unknown")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = repos.BankFeeds.ConnectionBySessionRef(ctx, "")
	require.ErrorIs(t, err, ErrNotFound)

	_, _, err = repos.BankFeeds.ConnectionByExternalReference(ctx, "plaid", "item-77")
	require.ErrorIs(t, err, ErrNotFound, "a connection with no item yet is not found by one")

	// The id is the Item's, so it answers as soon as the exchange has happened —
	// including in the moment before the row is flipped to LINKED, which is
	// exactly when a webhook may arrive.
	require.NoError(t, repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, conn.ConnectionID, "item-77", nil))
	orgID, connID, err = repos.BankFeeds.ConnectionByExternalReference(ctx, "plaid", "item-77")
	require.NoError(t, err)
	assert.Equal(t, seedDemoOrgID, orgID)
	assert.Equal(t, conn.ConnectionID, connID)

	// Lookups are scoped to the provider that issued the id.
	_, _, err = repos.BankFeeds.ConnectionByExternalReference(ctx, "gocardless_bad", "item-77")
	require.ErrorIs(t, err, ErrNotFound)

	// The session lookup goes quiet once the connection is linked: the webhook
	// that arrives after the browser has already finished finds nothing to do.
	_, _, err = repos.BankFeeds.ConnectionBySessionRef(ctx, "link-sandbox-9")
	require.ErrorIs(t, err, ErrNotFound)
}

// TestIntegration_BankFeed_UpsertKeepsCoding is the rule that keeps a feed from
// rewriting the books: while a line is in the inbox the bank's newer version
// replaces ours, and once it has been coded the bank's version is parked beside
// it instead. The notice is derived, so it clears itself when the two agree.
func TestIntegration_BankFeed_UpsertKeepsCoding(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")
	fa := &models.BankFeedAccount{
		ConnectionID:      conn.ConnectionID,
		ExternalAccountID: "acc-chk",
		CurrencyCode:      "USD",
	}
	require.NoError(t, repos.BankFeeds.UpsertAccount(ctx, seedDemoOrgID, fa))

	stage := func(amount string, day int) {
		t.Helper()
		row := &models.BankStatementLine{
			FeedAccountID: &fa.FeedAccountID,
			Source:        models.StatementLineSourceFeed,
			ProviderTxID:  "tx-1",
			PostedAt:      time.Date(2026, 4, day, 0, 0, 0, 0, time.UTC),
			Amount:        decimal.RequireFromString(amount),
			CurrencyCode:  "USD",
			Description:   "COFFEE",
		}
		_, err := repos.BankFeeds.UpsertStatementLine(ctx, seedDemoOrgID, fa.FeedAccountID, row, []byte(`{}`))
		require.NoError(t, err)
	}

	stage("-10.00", 1)
	// A pending charge settling before it posts: the line is still in the inbox,
	// so the new figure simply becomes the line's.
	stage("-12.00", 1)

	line := func() *models.BankStatementLine {
		t.Helper()
		rows, err := pool.Query(ctx,
			`SELECT l.amount, l.posted_at, l.upstream_amount, `+upstreamChangeExpr+`
			   FROM bank_statement_lines l WHERE l.provider_tx_id = 'tx-1'`)
		require.NoError(t, err)
		defer rows.Close()
		require.True(t, rows.Next())
		var (
			amount   decimal.Decimal
			posted   time.Time
			upstream *decimal.Decimal
			change   *string
		)
		require.NoError(t, rows.Scan(&amount, &posted, &upstream, &change))
		require.NoError(t, rows.Err())
		return &models.BankStatementLine{Amount: amount, PostedAt: posted,
			UpstreamAmount: upstream, UpstreamChange: derefString(change)}
	}

	got := line()
	assert.True(t, decimal.RequireFromString("-12.00").Equal(got.Amount), "the inbox line tracks the bank")
	assert.Nil(t, got.UpstreamAmount, "an inbox line has nothing parked beside it")
	assert.Empty(t, got.UpstreamChange)

	// The user codes it, and only then does the bank restate it.
	_, err := pool.Exec(ctx,
		`UPDATE bank_statement_lines SET status = $1 WHERE provider_tx_id = 'tx-1'`,
		models.BankFeedLineStatusImported)
	require.NoError(t, err)
	stage("-15.00", 2)

	got = line()
	assert.True(t, decimal.RequireFromString("-12.00").Equal(got.Amount),
		"a coded line is the books: the feed must not rewrite it")
	assert.Equal(t, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), got.PostedAt)
	require.NotNil(t, got.UpstreamAmount)
	assert.True(t, decimal.RequireFromString("-15.00").Equal(*got.UpstreamAmount),
		"the bank's version is parked beside the coding")
	assert.Equal(t, "MODIFIED", got.UpstreamChange)

	// The bank comes back to what was booked: nothing left to reconcile.
	stage("-12.00", 1)
	got = line()
	assert.Empty(t, got.UpstreamChange, "the notice is derived, so agreement clears it")
}

// TestIntegration_BankFeed_LiveConnectionAtInstitution — the lookup behind the
// one-live-connection-per-bank rule. What counts as live is the whole of it: a
// consent in flight and a working feed do, a broken or revoked one does not,
// because those are repaired in place or replaced rather than duplicated.
func TestIntegration_BankFeed_LiveConnectionAtInstitution(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	conn := newFeedConnection(t, repos, "plaid")

	// Nothing else at this bank: the caller sees a not-found, not an empty row.
	_, err := repos.BankFeeds.LiveConnectionAtInstitution(ctx, seedDemoOrgID, "plaid", "ins_99", uuid.Nil)
	assert.ErrorIs(t, err, ErrNotFound)

	found, err := repos.BankFeeds.LiveConnectionAtInstitution(ctx, seedDemoOrgID, "plaid", "ins_56", uuid.Nil)
	require.NoError(t, err)
	assert.Equal(t, conn.ConnectionID, found.ConnectionID)
	assert.Equal(t, models.BankFeedStatusPending, found.Status)

	// The connection being finished is not a duplicate of itself.
	_, err = repos.BankFeeds.LiveConnectionAtInstitution(ctx, seedDemoOrgID, "plaid", "ins_56", conn.ConnectionID)
	assert.ErrorIs(t, err, ErrNotFound)

	// A different provider is a different bank as far as this is concerned:
	// institution ids are per aggregator, and GoCardless's OB_ ids never collide
	// with Plaid's ins_ ones by accident.
	_, err = repos.BankFeeds.LiveConnectionAtInstitution(ctx, seedDemoOrgID, "gocardless", "ins_56", uuid.Nil)
	assert.ErrorIs(t, err, ErrNotFound)

	// A granted consent is still live.
	require.NoError(t, repos.BankFeeds.UpdateConnectionStatus(ctx, seedDemoOrgID, conn.ConnectionID,
		models.BankFeedStatusLinked, "", nil))
	found, err = repos.BankFeeds.LiveConnectionAtInstitution(ctx, seedDemoOrgID, "plaid", "ins_56", uuid.Nil)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusLinked, found.Status)

	// A broken one is not: the way forward there is a repair or a fresh consent,
	// and blocking it would leave the user with a bank they cannot re-add.
	for _, status := range []string{models.BankFeedStatusError, models.BankFeedStatusRevoked} {
		require.NoError(t, repos.BankFeeds.UpdateConnectionStatus(ctx, seedDemoOrgID, conn.ConnectionID,
			status, "provider said no", nil))
		_, err = repos.BankFeeds.LiveConnectionAtInstitution(ctx, seedDemoOrgID, "plaid", "ins_56", uuid.Nil)
		assert.ErrorIs(t, err, ErrNotFound, status+" must not read as connected")
	}

	// Another tenant's connection to the same bank is not ours to collide with.
	other := &models.Organisation{Name: "Other-" + uuid.NewString()[:6], BaseCurrency: "USD"}
	require.NoError(t, repos.Organisations.Create(ctx, other))
	require.NoError(t, repos.BankFeeds.UpdateConnectionStatus(ctx, seedDemoOrgID, conn.ConnectionID,
		models.BankFeedStatusLinked, "", nil))
	_, err = repos.BankFeeds.LiveConnectionAtInstitution(ctx, other.OrganisationID, "plaid", "ins_56", uuid.Nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
