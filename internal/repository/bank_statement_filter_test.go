package repository

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A FEED line belongs to a bank account through its feed account's binding, not
// directly, so filtering the inbox by bank account has to match both. The
// subquery that resolves the binding must reference the same id as the direct
// branch: ListLines prepends the org id as $1, so an off-by-one here would point
// the subquery at the org id and silently drop every feed line from the inbox.
func TestStatementLineFilter_BankAccountBindsOneIDAtTheRightPlaceholders(t *testing.T) {
	accountID := uuid.New()
	where, args := StatementLineFilter{BankAccountID: &accountID}.where()

	require.Equal(t, []any{accountID}, args, "where() binds only the bank account id")
	assert.Contains(t, where, "l.bank_account_id=$2")
	assert.Contains(t, where, "bank_feed_accounts WHERE account_id=$2")

	highest := 0
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(where, -1) {
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		if n > highest {
			highest = n
		}
	}
	assert.Equal(t, len(args)+1, highest, "no placeholder may exceed the arguments ListLines will bind")
}
