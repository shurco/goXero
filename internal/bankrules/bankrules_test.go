package bankrules

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
)

func spend(desc string, amount float64) Candidate {
	return Candidate{
		Payee:         desc,
		Description:   desc,
		Reference:     "",
		Amount:        decimal.NewFromFloat(amount),
		BankAccountID: "acct-1",
	}
}

func rule(name string, def models.BankRuleDefinition) models.BankRule {
	return models.BankRule{Name: name, RuleType: "SPEND", IsActive: true, Definition: def}
}

func cond(field, op, value string) models.BankRuleCondition {
	return models.BankRuleCondition{Field: field, Operator: op, Value: value}
}

func alloc(account string, percent float64) models.BankRuleAllocationLine {
	return models.BankRuleAllocationLine{Description: "line", AccountID: account, Percent: percent}
}

// The rule editor writes lower-case, underscored operators while the API stores
// upper-case ones; both must resolve to the same thing.
func TestOperatorSpellingsAreEquivalent(t *testing.T) {
	for _, spelling := range []string{"STARTSWITH", "starts_with", "Starts_With"} {
		def := models.BankRuleDefinition{
			MatchMode:    MatchAll,
			Conditions:   []models.BankRuleCondition{cond(FieldPayee, spelling, "Star")},
			PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
		}
		assert.True(t, Scoped(def, spend("Starbucks", -5)), "operator %q must match", spelling)
	}
}

func TestAnyTextFieldIsADisjunctionOverFreeText(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldAnyText, "contains", "invoice")},
		PercentLines: []models.BankRuleAllocationLine{alloc("200", 100)},
	}
	assert.True(t, Scoped(def, Candidate{Reference: "Invoice 1042"}), "reference must be searched")
	assert.True(t, Scoped(def, Candidate{Description: "INVOICE 99"}), "description must be searched")
	assert.True(t, Scoped(def, Candidate{Payee: "Invoice co"}), "payee must be searched")
	assert.False(t, Scoped(def, Candidate{Payee: "Something else"}), "unrelated text must not match")
}

func TestMatchAnyVersusMatchAll(t *testing.T) {
	anyDef := models.BankRuleDefinition{
		MatchMode: MatchAny,
		Conditions: []models.BankRuleCondition{
			cond(FieldPayee, OpContains, "starbucks"),
			cond(FieldAmount, OpGreaterThan, "1000"),
		},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	assert.True(t, Scoped(anyDef, spend("Starbucks", -5)), "one of two conditions is enough for ANY")

	allDef := anyDef
	allDef.MatchMode = MatchAll
	assert.False(t, Scoped(allDef, spend("Starbucks", -5)), "ALL requires every condition")
	assert.True(t, Scoped(allDef, spend("Starbucks", -5000)), "both conditions hold")
}

// Amount comparisons are on magnitude: "$600 out" and "$600 in" both exceed 500,
// which is what a user writing "amount greater than 500" means.
func TestAmountComparisonsUseMagnitude(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldAmount, OpGreaterThan, "500")},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	assert.True(t, Scoped(def, spend("out", -600)))
	assert.True(t, Scoped(def, spend("in", 600)))
	assert.False(t, Scoped(def, spend("small", -400)))

	typeDef := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldType, OpEquals, TypeSpend)},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	assert.True(t, Scoped(typeDef, spend("out", -1)))
	assert.False(t, Scoped(typeDef, spend("in", 1)))
}

func TestBlankOperators(t *testing.T) {
	blank := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldReference, OpIsBlank, "")},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	assert.True(t, Scoped(blank, Candidate{Payee: "no reference"}))
	assert.False(t, Scoped(blank, Candidate{Reference: "has one"}))
}

// The first rule in evaluation order wins, which is the whole reason sort_order
// exists: overlapping rules are normal and the user reorders them.
func TestSuggestIsFirstMatchWins(t *testing.T) {
	broad := rule("All card spend", models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldPayee, OpIsNotBlank, "")},
		PercentLines: []models.BankRuleAllocationLine{alloc("500", 100)},
	})
	narrow := rule("Starbucks", models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldPayee, OpContains, "starbucks")},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	})

	s := Suggest([]models.BankRule{broad, narrow}, spend("Starbucks #1234", -5))
	require.NotNil(t, s)
	assert.Equal(t, "All card spend", s.RuleName)

	// Reordered, the specific rule wins.
	s = Suggest([]models.BankRule{narrow, broad}, spend("Starbucks #1234", -5))
	require.NotNil(t, s)
	assert.Equal(t, "Starbucks", s.RuleName)
	assert.Equal(t, "429", s.AccountID)

	// A line nothing matches gets no suggestion at all.
	assert.Nil(t, Suggest([]models.BankRule{narrow}, Candidate{Payee: "Nowhere"}))
}

func TestInactiveRuleNeverMatches(t *testing.T) {
	r := rule("Off", models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldPayee, OpIsNotBlank, "")},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	})
	r.IsActive = false
	assert.False(t, Matches(r, spend("anything", -5)))
	assert.Nil(t, Suggest([]models.BankRule{r}, spend("anything", -5)))
}

func TestRuleScopedToAnotherAccountDoesNotMatch(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:          MatchAll,
		RunOn:              RunOnSpecificAccount,
		ScopeBankAccountID: "acct-2",
		Conditions:         []models.BankRuleCondition{cond(FieldPayee, OpIsNotBlank, "")},
		PercentLines:       []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	assert.False(t, Scoped(def, spend("x", -1)), "the line is on acct-1")
	other := spend("x", -1)
	other.BankAccountID = "acct-2"
	assert.True(t, Scoped(def, other))
}

func TestAllocateSplitsProportionally(t *testing.T) {
	def := models.BankRuleDefinition{
		PercentLines: []models.BankRuleAllocationLine{
			{Description: "rent", AccountID: "470", Percent: 60},
			{Description: "rates", AccountID: "471", Percent: 40},
		},
	}
	got := Allocate(def, decimal.NewFromInt(1000))
	require.Len(t, got, 2)
	assert.True(t, got[0].Amount.Equal(decimal.NewFromInt(600)), got[0].Amount.String())
	assert.True(t, got[1].Amount.Equal(decimal.NewFromInt(400)), got[1].Amount.String())
}

// A rule with a single allocation line and no figure takes the whole amount —
// how most rules are written, since the user only cares about the account.
func TestAllocateSingleLineTakesEverything(t *testing.T) {
	def := models.BankRuleDefinition{
		PercentLines: []models.BankRuleAllocationLine{{AccountID: "429"}},
	}
	got := Allocate(def, decimal.NewFromFloat(-42.5))
	require.Len(t, got, 1)
	assert.True(t, got[0].Amount.Equal(decimal.NewFromFloat(-42.5)))
}

func TestValidateRejectsRulesThatCouldNeverWork(t *testing.T) {
	base := func() models.BankRuleDefinition {
		return models.BankRuleDefinition{
			MatchMode:    MatchAll,
			Conditions:   []models.BankRuleCondition{cond(FieldPayee, OpContains, "x")},
			PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
		}
	}
	require.NoError(t, Validate(base()))

	noConditions := base()
	noConditions.Conditions = nil
	assert.ErrorIs(t, Validate(noConditions), ErrInvalidDefinition)

	badField := base()
	badField.Conditions = []models.BankRuleCondition{cond("NOPE", OpContains, "x")}
	assert.ErrorIs(t, Validate(badField), ErrInvalidDefinition)

	badOp := base()
	badOp.Conditions = []models.BankRuleCondition{cond(FieldPayee, "SMELSLIKE", "x")}
	assert.ErrorIs(t, Validate(badOp), ErrInvalidDefinition)

	missingValue := base()
	missingValue.Conditions = []models.BankRuleCondition{cond(FieldPayee, OpContains, "  ")}
	assert.ErrorIs(t, Validate(missingValue), ErrInvalidDefinition)

	notANumber := base()
	notANumber.Conditions = []models.BankRuleCondition{cond(FieldAmount, OpGreaterThan, "lots")}
	assert.ErrorIs(t, Validate(notANumber), ErrInvalidDefinition)

	// A rule that matches but codes nothing is not a rule.
	codesNothing := base()
	codesNothing.PercentLines = nil
	assert.ErrorIs(t, Validate(codesNothing), ErrInvalidDefinition)

	// ...unless it is a transfer rule, which has a target instead of an allocation.
	transfer := base()
	transfer.PercentLines = nil
	transfer.TransferTargetMode = "RECONCILE_CHOOSE"
	assert.NoError(t, Validate(transfer))

	badPercent := base()
	badPercent.PercentLines = []models.BankRuleAllocationLine{alloc("429", 140)}
	assert.ErrorIs(t, Validate(badPercent), ErrInvalidDefinition)
}

// The rule editor creates every new condition with Field "ANY_TEXT"; if Validate
// rejects it the user can never save a rule at all.
func TestValidateAcceptsAnyTextField(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldAnyText, OpContains, "invoice")},
		PercentLines: []models.BankRuleAllocationLine{alloc("200", 100)},
	}
	require.NoError(t, Validate(def))
}

// Percentage lines split the whole remainder, so they must add up to 100 — a
// 60%+30% rule would otherwise leave 10% unposted and unbalance the journal.
func TestValidateRequiresPercentsToSumTo100(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:  MatchAll,
		Conditions: []models.BankRuleCondition{cond(FieldPayee, OpIsNotBlank, "")},
		PercentLines: []models.BankRuleAllocationLine{
			alloc("470", 60),
			alloc("471", 30),
		},
	}
	assert.ErrorIs(t, Validate(def), ErrInvalidDefinition)

	def.PercentLines = []models.BankRuleAllocationLine{alloc("470", 60), alloc("471", 40)}
	assert.NoError(t, Validate(def))
}

// A rule scoped to a specific account without naming one would silently apply
// to every account, so it is rejected.
func TestValidateRejectsSpecificScopeWithoutAccount(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		RunOn:        RunOnSpecificAccount,
		Conditions:   []models.BankRuleCondition{cond(FieldPayee, OpIsNotBlank, "")},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	assert.ErrorIs(t, Validate(def), ErrInvalidDefinition)
	def.ScopeBankAccountID = "acct-1"
	assert.NoError(t, Validate(def))
}

// A SPEND rule codes money out and must not fire on an incoming refund.
func TestRuleTypeGatesDirection(t *testing.T) {
	def := models.BankRuleDefinition{
		MatchMode:    MatchAll,
		Conditions:   []models.BankRuleCondition{cond(FieldPayee, OpContains, "starbucks")},
		PercentLines: []models.BankRuleAllocationLine{alloc("429", 100)},
	}
	spendRule := rule("Starbucks spend", def)
	assert.True(t, Matches(spendRule, spend("Starbucks", -5)))

	receiveRule := rule("Starbucks receive", def)
	receiveRule.RuleType = "RECEIVE"
	assert.False(t, Matches(receiveRule, spend("Starbucks", -5)))
	assert.True(t, Matches(receiveRule, spend("Starbucks", 5)))
}

// Fixed amounts come off the top, percentages split what is left, and the split
// must always sum back to the transaction total.
func TestAllocateFixedThenPercentOfRemainder(t *testing.T) {
	def := models.BankRuleDefinition{
		FixedLines:   []models.BankRuleAllocationLine{{AccountID: "100", Amount: 100}},
		PercentLines: []models.BankRuleAllocationLine{{AccountID: "470", Percent: 100}},
	}
	got := Allocate(def, decimal.NewFromInt(500))
	require.Len(t, got, 2)
	assert.True(t, got[0].Amount.Equal(decimal.NewFromInt(100)), got[0].Amount.String())
	assert.True(t, got[1].Amount.Equal(decimal.NewFromInt(400)), got[1].Amount.String())
}

// Rounding residue is absorbed by the last line so the allocations sum to the
// total exactly.
func TestAllocateAbsorbsRoundingResidue(t *testing.T) {
	def := models.BankRuleDefinition{
		PercentLines: []models.BankRuleAllocationLine{
			{AccountID: "a", Percent: 33.33},
			{AccountID: "b", Percent: 33.33},
			{AccountID: "c", Percent: 33.34},
		},
	}
	got := Allocate(def, decimal.NewFromInt(100))
	sum := decimal.Zero
	for _, a := range got {
		sum = sum.Add(a.Amount)
	}
	assert.True(t, sum.Equal(decimal.NewFromInt(100)), sum.String())
}
