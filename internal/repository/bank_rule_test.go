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

func makeBankRule(name string) *models.BankRule {
	return &models.BankRule{
		RuleType: "SPEND",
		Name:     name,
		IsActive: true,
		Definition: models.BankRuleDefinition{
			MatchMode: "ANY",
			RunOn:     "AUTO",
			Conditions: []models.BankRuleCondition{
				{Field: "Description", Operator: "CONTAINS", Value: "uber"},
			},
		},
	}
}

// TestIntegration_BankRule_CRUD covers full CRUD lifecycle and ensures JSONB
// Definition round-trips through scanBankRule without silent data loss.
func TestIntegration_BankRule_CRUD(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	br := makeBankRule("Rule-" + uuid.NewString()[:6])
	require.NoError(t, repos.BankRules.Create(ctx, seedDemoOrgID, br))
	require.NotEqual(t, uuid.Nil, br.BankRuleID)
	require.False(t, br.CreatedAt.IsZero())

	got, err := repos.BankRules.GetByID(ctx, seedDemoOrgID, br.BankRuleID)
	require.NoError(t, err)
	assert.Equal(t, br.Name, got.Name)
	assert.Equal(t, "ANY", got.Definition.MatchMode)
	require.Len(t, got.Definition.Conditions, 1)
	assert.Equal(t, "uber", got.Definition.Conditions[0].Value)

	got.Name = got.Name + "-upd"
	got.IsActive = false
	got.Definition.Conditions = append(got.Definition.Conditions, models.BankRuleCondition{
		Field: "Reference", Operator: "EQUALS", Value: "R1",
	})
	require.NoError(t, repos.BankRules.Update(ctx, seedDemoOrgID, got))

	fresh, err := repos.BankRules.GetByID(ctx, seedDemoOrgID, br.BankRuleID)
	require.NoError(t, err)
	assert.Contains(t, fresh.Name, "-upd")
	assert.False(t, fresh.IsActive)
	assert.Len(t, fresh.Definition.Conditions, 2)

	list, err := repos.BankRules.List(ctx, seedDemoOrgID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 1)

	require.NoError(t, repos.BankRules.Delete(ctx, seedDemoOrgID, br.BankRuleID))
	_, err = repos.BankRules.GetByID(ctx, seedDemoOrgID, br.BankRuleID)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestIntegration_BankRule_CrossTenantIsolation guarantees that a rule
// created in one org cannot be read, updated or deleted via another org scope.
func TestIntegration_BankRule_CrossTenantIsolation(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	other := &models.Organisation{Name: "Other-" + uuid.NewString()[:6], BaseCurrency: "USD"}
	require.NoError(t, repos.Organisations.Create(ctx, other))

	br := makeBankRule("Iso-" + uuid.NewString()[:6])
	require.NoError(t, repos.BankRules.Create(ctx, seedDemoOrgID, br))

	_, err := repos.BankRules.GetByID(ctx, other.OrganisationID, br.BankRuleID)
	assert.ErrorIs(t, err, ErrNotFound)

	err = repos.BankRules.Update(ctx, other.OrganisationID, br)
	assert.ErrorIs(t, err, ErrNotFound)

	err = repos.BankRules.Delete(ctx, other.OrganisationID, br.BankRuleID)
	assert.ErrorIs(t, err, ErrNotFound)

	still, err := repos.BankRules.GetByID(ctx, seedDemoOrgID, br.BankRuleID)
	require.NoError(t, err)
	assert.Equal(t, br.BankRuleID, still.BankRuleID)
}

// TestIntegration_BankRule_UpdateMissing surfaces ErrNotFound when targeting
// a non-existent rule id in the correct tenant.
func TestIntegration_BankRule_UpdateMissing(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	br := makeBankRule("Ghost")
	br.BankRuleID = uuid.New()
	err := repos.BankRules.Update(context.Background(), seedDemoOrgID, br)
	assert.ErrorIs(t, err, ErrNotFound)
}
