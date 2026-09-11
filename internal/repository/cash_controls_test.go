package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

// The Cash Summary looks through four control accounts, and which accounts those
// are is a property of the organisation's chart and not of this build. The
// resolution is by the Xero system role the account holds (Account.SystemAccount
// — migration 00027 declares this chart's), never by an account code: a code is
// the organisation's own label, so a chart is free to call its sales tax account
// 220 and be read correctly.
//
// The tags are set with SQL because system_account arrives with an imported
// chart and no application path writes it — which is exactly why the reader has
// to prefer it to a literal.
func TestIntegration_CashControlsFollowTheOrganisationTag(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	org := &models.Organisation{Name: "Tagged chart " + uuid.NewString()[:8]}
	require.NoError(t, repos.Organisations.Create(ctx, org))

	// A chart that has declared nothing resolves to nothing. There is no
	// fallback to an account code, because "the account coded 820" is the sales
	// tax account in one chart and an arbitrary expense in the next: an
	// undeclared role has to read as undeclared, so that the report leaves that
	// cash unclassified instead of putting it in the wrong section.
	then, err := repos.Reports.cashControls(ctx, org.OrganisationID)
	require.NoError(t, err)
	assert.Equal(t, "", then.receivable, "an undeclared role resolves to no account, not to a code")
	assert.Equal(t, "", then.payable)
	assert.Equal(t, "", then.claims)
	assert.Equal(t, "", then.tax)

	// Tag one account with a role, under a code no chart default would predict.
	acc := &models.Account{
		Code:   "1100",
		Name:   "Trade receivables",
		Type:   "CURRENT",
		Status: "ACTIVE",
	}
	require.NoError(t, repos.Accounts.Create(ctx, org.OrganisationID, acc))
	_, err = pool.Exec(ctx,
		`UPDATE accounts SET system_account=$2 WHERE account_id=$1`, acc.AccountID, models.SystemAccountDebtors)
	require.NoError(t, err)

	now, err := repos.Reports.cashControls(ctx, org.OrganisationID)
	require.NoError(t, err)
	assert.Equal(t, "1100", now.receivable, "the role decides which account is looked through, whatever its code")
	assert.Equal(t, "", now.payable, "a role this chart has not declared still resolves to nothing")
	assert.Equal(t, "", now.tax)

	// The roles are read per organisation: tagging one chart must not move
	// another's.
	other, err := repos.Reports.cashControls(ctx, seedDemoOrgID)
	require.NoError(t, err)
	assert.NotEqual(t, "", other.receivable, "the seeded chart declares its control accounts")
	assert.NotEqual(t, "1100", other.receivable)
}

// TestIntegration_CashControlsResolveByRoleNotByCode is the same claim read from
// the other side: for each role the report resolves, the account it resolved to
// must be the account that actually carries that role. Asserting the code that
// comes back would only re-state the literal this change removed — what has to
// hold is that the answer follows the tag.
func TestIntegration_CashControlsResolveByRoleNotByCode(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	got, err := repos.Reports.cashControls(ctx, seedDemoOrgID)
	require.NoError(t, err)
	require.NotEmpty(t, [4]string{got.receivable, got.payable, got.claims, got.tax})

	for _, c := range []struct{ role, code string }{
		{models.SystemAccountDebtors, got.receivable},
		{models.SystemAccountCreditors, got.payable},
		{models.SystemAccountUnpaidExpClm, got.claims},
		{models.SystemAccountGST, got.tax},
	} {
		var held string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COALESCE(system_account,'') FROM accounts
			  WHERE organisation_id=$1 AND code=$2`, seedDemoOrgID, c.code).Scan(&held))
		assert.Equal(t, c.role, held,
			"the account resolved for role %s is the one that holds it", c.role)
	}
}

// A role the write path reads is resolved the same way, and its absence is
// reported rather than guessed at. A document whose tax has nowhere to go folds
// the tax into its counterpart line and says so; a control account that is
// simply missing is an error naming the role, because posting a document with no
// Accounts Receivable to debit is not something a code fallback can rescue.
func TestIntegration_SystemAccountResolutionIsByRole(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	org := &models.Organisation{Name: "Roleless chart " + uuid.NewString()[:8]}
	require.NoError(t, repos.Organisations.Create(ctx, org))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, ok, err := systemAccountIDOrNil(ctx, tx, org.OrganisationID, models.SystemAccountGST)
	require.NoError(t, err)
	assert.False(t, ok, "an undeclared role has no account to return")

	_, err = systemAccountID(ctx, tx, org.OrganisationID, models.SystemAccountDebtors)
	require.Error(t, err)
	assert.Contains(t, err.Error(), models.SystemAccountDebtors,
		"the error names the role, so the missing tag can be found")
}
