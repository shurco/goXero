// Package bankrules evaluates Xero-style bank rules against statement lines.
//
// A rule is a set of conditions plus the coding to apply when they match. Xero
// evaluates the rules for an account top-down and lets the user reorder them,
// and the first rule that matches wins — that ordering is the whole reason
// `bank_rules.sort_order` exists, because overlapping rules are normal
// ("Starbucks" before "all card spend").
//
// Nothing here touches the database: the caller loads the rules, hands over the
// statement lines, and gets back the suggestions to show in the reconcile
// inbox. Applying a suggestion is a separate, explicit user action, exactly as
// in Xero — a rule never posts anything on its own.
package bankrules

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

// Condition fields. These are the values stored in
// `BankRuleDefinition.Conditions[].Field`.
const (
	FieldPayee        = "PAYEE"
	FieldDescription  = "DESCRIPTION"
	FieldReference    = "REFERENCE"
	FieldAmount       = "AMOUNT"
	FieldType         = "TYPE"
	FieldChequeNumber = "CHEQUENUMBER"
	// FieldAnyText matches payee, description, reference or cheque number —
	// the rule editor's "Any text field".
	FieldAnyText = "ANY_TEXT"
)

// anyTextField lists the fields an ANY_TEXT condition is tested against.
var anyTextField = []string{FieldPayee, FieldDescription, FieldReference, FieldChequeNumber}

// Condition operators.
const (
	OpContains         = "CONTAINS"
	OpNotContains      = "NOTCONTAINS"
	OpEquals           = "EQUALS"
	OpNotEquals        = "NOTEQUALS"
	OpStartsWith       = "STARTSWITH"
	OpEndsWith         = "ENDSWITH"
	OpGreaterThan      = "GREATERTHAN"
	OpGreaterThanEqual = "GREATERTHANOREQUAL"
	OpLessThan         = "LESSTHAN"
	OpLessThanEqual    = "LESSTHANOREQUAL"
	OpIsBlank          = "ISBLANK"
	OpIsNotBlank       = "ISNOTBLANK"
)

// Match modes: every condition must hold, or any one of them.
const (
	MatchAll = "ALL"
	MatchAny = "ANY"
)

// RunOn values — which accounts the rule applies to.
const (
	RunOnAllAccounts     = "ALL_BANK_ACCOUNTS"
	RunOnSpecificAccount = "SPECIFIC_ACCOUNT"
)

// Money direction, used by the TYPE condition.
const (
	TypeSpend   = "SPEND"
	TypeReceive = "RECEIVE"
)

// Candidate is one statement line reduced to the fields rules can test.
type Candidate struct {
	Payee        string
	Description  string
	Reference    string
	ChequeNumber string
	ContactName  string
	Amount       decimal.Decimal
	// BankAccountID is the ledger account the line belongs to; a rule can be
	// scoped to one account via ScopeBankAccountID.
	BankAccountID string
}

// Direction reports whether the line is money out or money in.
func (c Candidate) Direction() string {
	if c.Amount.IsNegative() {
		return TypeSpend
	}
	return TypeReceive
}

// ErrInvalidDefinition is returned by Validate for a rule that could never
// match or could never code anything — better to reject it when the user
// saves than to have it silently do nothing.
var ErrInvalidDefinition = errors.New("invalid bank rule definition")

// Validate checks a rule definition before it is stored.
func Validate(def models.BankRuleDefinition) error {
	switch def.MatchMode {
	case "", MatchAll, MatchAny:
	default:
		return fmt.Errorf("%w: MatchMode must be ALL or ANY", ErrInvalidDefinition)
	}
	if len(def.Conditions) == 0 {
		return fmt.Errorf("%w: at least one condition is required", ErrInvalidDefinition)
	}
	for i, cond := range def.Conditions {
		if !knownField(cond.Field) {
			return fmt.Errorf("%w: condition %d: unknown field %q", ErrInvalidDefinition, i+1, cond.Field)
		}
		if !knownOperator(normaliseOperator(cond.Operator)) {
			return fmt.Errorf("%w: condition %d: unknown operator %q", ErrInvalidDefinition, i+1, cond.Operator)
		}
		op := normaliseOperator(cond.Operator)
		if needsValue(op) && strings.TrimSpace(cond.Value) == "" {
			return fmt.Errorf("%w: condition %d: a value is required for %s", ErrInvalidDefinition, i+1, cond.Operator)
		}
		if op == OpGreaterThan || op == OpLessThan ||
			op == OpGreaterThanEqual || op == OpLessThanEqual {
			if _, err := decimal.NewFromString(strings.TrimSpace(cond.Value)); err != nil {
				return fmt.Errorf("%w: condition %d: %q is not an amount", ErrInvalidDefinition, i+1, cond.Value)
			}
		}
	}
	// A rule has to do something: code a line, or move it to another account.
	if def.TransferTargetMode == "" && len(def.FixedLines) == 0 && len(def.PercentLines) == 0 {
		return fmt.Errorf("%w: add an allocation line or a transfer target", ErrInvalidDefinition)
	}
	if def.RunOn == RunOnSpecificAccount && strings.TrimSpace(def.ScopeBankAccountID) == "" {
		return fmt.Errorf("%w: a rule scoped to one account needs ScopeBankAccountID", ErrInvalidDefinition)
	}
	explicitPercent := 0.0
	for i, line := range def.PercentLines {
		if line.Percent < 0 || line.Percent > 100 {
			return fmt.Errorf("%w: allocation %d: percent must be between 0 and 100", ErrInvalidDefinition, i+1)
		}
		explicitPercent += line.Percent
	}
	// Percentage lines split the whole remainder, so together they must add up
	// to 100. The lone 0% line is the "whatever is left" shorthand and is exempt.
	loneRemainder := len(def.PercentLines) == 1 && def.PercentLines[0].Percent == 0
	if len(def.PercentLines) > 0 && !loneRemainder && !almostEqual(explicitPercent, 100) {
		return fmt.Errorf("%w: percentage allocations must add up to 100", ErrInvalidDefinition)
	}
	return nil
}

// Matches reports whether `c` satisfies the rule. An inactive rule never
// matches; use Scoped to also honour ScopeBankAccountID.
func Matches(rule models.BankRule, c Candidate) bool {
	if !rule.IsActive {
		return false
	}
	// In Xero a SPEND rule only codes money out and a RECEIVE rule only money
	// in — the rule's type is not decorative. Transfer rules are directionless
	// here because a transfer has no single direction on one line.
	switch strings.ToUpper(strings.TrimSpace(rule.RuleType)) {
	case TypeSpend:
		if c.Direction() != TypeSpend {
			return false
		}
	case TypeReceive:
		if c.Direction() != TypeReceive {
			return false
		}
	}
	return Scoped(rule.Definition, c)
}

// Scoped is Matches for a bare definition, for callers that already filtered
// by active state.
func Scoped(def models.BankRuleDefinition, c Candidate) bool {
	// A rule scoped to one account only matches that account's lines.
	if def.ScopeBankAccountID != "" && def.ScopeBankAccountID != c.BankAccountID {
		return false
	}
	if len(def.Conditions) == 0 {
		return false
	}
	any := false
	for _, cond := range def.Conditions {
		if conditionMatches(cond, c) {
			any = true
			continue
		}
		if def.MatchMode != MatchAny {
			return false
		}
	}
	return any
}

// Suggest walks the rules in the order given (already sorted by sort_order) and
// returns the suggestion of the first one that matches — Xero's "first match
// wins". A nil result means no rule applied.
func Suggest(rules []models.BankRule, c Candidate) *models.BankRuleSuggestion {
	for _, rule := range rules {
		if !Matches(rule, c) {
			continue
		}
		s := &models.BankRuleSuggestion{BankRuleID: rule.BankRuleID, RuleName: rule.Name}
		if n := len(rule.Definition.FixedLines) + len(rule.Definition.PercentLines); n > 0 {
			first := firstAllocation(rule.Definition)
			s.AccountID = first.AccountID
			s.TaxType = first.TaxRateID
		}
		if rule.Definition.ContactMode == "FIXED" || rule.Definition.ContactMode == "" {
			s.ContactID = rule.Definition.ContactID
		}
		return s
	}
	return nil
}

func firstAllocation(def models.BankRuleDefinition) models.BankRuleAllocationLine {
	if len(def.FixedLines) > 0 {
		return def.FixedLines[0]
	}
	if len(def.PercentLines) > 0 {
		return def.PercentLines[0]
	}
	return models.BankRuleAllocationLine{}
}

// Allocation is one resolved split of a statement line's amount.
type Allocation struct {
	Description string
	AccountID   string
	TaxType     string
	Region      string
	Amount      decimal.Decimal
	Percent     float64
}

// Allocate resolves the rule's allocation lines against a transaction total.
//
// Xero's rule editor lets you mix the two: fixed amounts come off the top, and
// percentage lines split what is left. A rule with a single allocation line and
// neither amount nor percent takes the whole transaction — which is how most
// rules are written, since the user only cares about the account.
func Allocate(def models.BankRuleDefinition, total decimal.Decimal) []Allocation {
	lines := make([]models.BankRuleAllocationLine, 0, len(def.FixedLines)+len(def.PercentLines))
	lines = append(lines, def.FixedLines...)
	lines = append(lines, def.PercentLines...)
	if len(lines) == 0 {
		return nil
	}
	// A lone line with no amount and no percent swallows everything.
	if len(lines) == 1 && lines[0].Amount == 0 && lines[0].Percent == 0 {
		return []Allocation{allocationOf(lines[0], total)}
	}

	out := make([]Allocation, 0, len(lines))
	remaining := total
	for _, l := range def.FixedLines {
		amount := decimal.NewFromFloat(l.Amount)
		remaining = remaining.Sub(amount)
		out = append(out, allocationOf(l, amount))
	}

	allocated := decimal.Zero
	for i, l := range def.PercentLines {
		// A single percentage line with no figure means "whatever is left".
		if len(def.PercentLines) == 1 && l.Percent == 0 {
			out = append(out, allocationOf(l, remaining))
			continue
		}
		var amount decimal.Decimal
		if i == len(def.PercentLines)-1 {
			// The last line absorbs the rounding residue so the split sums to
			// the total and the resulting journal still balances to zero.
			amount = remaining.Sub(allocated)
		} else {
			amount = remaining.Mul(decimal.NewFromFloat(l.Percent)).Div(decimal.NewFromInt(100))
		}
		allocated = allocated.Add(amount)
		a := allocationOf(l, amount)
		a.Percent = l.Percent
		out = append(out, a)
	}
	return out
}

func allocationOf(l models.BankRuleAllocationLine, amount decimal.Decimal) Allocation {
	return Allocation{
		Description: l.Description,
		AccountID:   l.AccountID,
		TaxType:     l.TaxRateID,
		Region:      l.Region,
		Amount:      amount,
	}
}

// ---------------------------------------------------------------------------
// Condition evaluation
// ---------------------------------------------------------------------------

func conditionMatches(cond models.BankRuleCondition, c Candidate) bool {
	op := normaliseOperator(cond.Operator)
	// "Any text field" is a disjunction over the free-text fields: the user is
	// saying "the payee, the reference or the description mentions this".
	if strings.EqualFold(strings.TrimSpace(cond.Field), FieldAnyText) {
		for _, f := range anyTextField {
			if fieldMatches(c, f, op, cond.Value) {
				return true
			}
		}
		return false
	}
	return fieldMatches(c, cond.Field, op, cond.Value)
}

// fieldMatches evaluates one field/operator/value triple.
func fieldMatches(c Candidate, field, op, want string) bool {
	value, err := fieldValue(c, field)
	if err != nil {
		return false
	}
	want = strings.TrimSpace(want)
	lv := strings.ToLower(value)
	lw := strings.ToLower(want)
	amount := isAmountField(field)

	switch op {
	case OpContains:
		return strings.Contains(lv, lw)
	case OpNotContains:
		return !strings.Contains(lv, lw)
	case OpEquals, "": // Xero's default operator is "equals"
		if amount {
			cmp, ok := compareAmounts(value, want)
			return ok && cmp == 0
		}
		return lv == lw
	case OpNotEquals:
		if amount {
			cmp, ok := compareAmounts(value, want)
			return ok && cmp != 0
		}
		return lv != lw
	case OpStartsWith:
		return strings.HasPrefix(lv, lw)
	case OpEndsWith:
		return strings.HasSuffix(lv, lw)
	case OpGreaterThan:
		cmp, ok := compareAmounts(value, want)
		return ok && cmp > 0
	case OpGreaterThanEqual:
		cmp, ok := compareAmounts(value, want)
		return ok && cmp >= 0
	case OpLessThan:
		cmp, ok := compareAmounts(value, want)
		return ok && cmp < 0
	case OpLessThanEqual:
		cmp, ok := compareAmounts(value, want)
		return ok && cmp <= 0
	case OpIsBlank:
		return strings.TrimSpace(value) == ""
	case OpIsNotBlank:
		return strings.TrimSpace(value) != ""
	}
	return false
}

func isAmountField(field string) bool {
	return strings.EqualFold(strings.TrimSpace(field), FieldAmount)
}

// compareAmounts compares two decimal strings by magnitude. The bool is false
// when either side is not a decimal, so an unparseable amount never satisfies a
// comparison instead of silently matching "greater than".
func compareAmounts(value, want string) (int, bool) {
	a, errA := decimal.NewFromString(strings.TrimSpace(value))
	b, errB := decimal.NewFromString(strings.TrimSpace(want))
	if errA != nil || errB != nil {
		return 0, false
	}
	// Rules compare magnitudes: users write "Amount is greater than 500" and
	// mean a $600 payment out as much as a $600 payment in. The TYPE condition
	// is how they say which direction they meant.
	return a.Abs().Cmp(b.Abs()), true
}

func fieldValue(c Candidate, field string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(field)) {
	case FieldPayee:
		return firstNonEmpty(c.Payee, c.ContactName), nil
	case FieldDescription:
		return c.Description, nil
	case FieldReference:
		return c.Reference, nil
	case FieldChequeNumber:
		return c.ChequeNumber, nil
	case FieldAmount:
		return c.Amount.String(), nil
	case FieldType:
		return c.Direction(), nil
	}
	return "", fmt.Errorf("unknown field %q", field)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func knownField(field string) bool {
	switch strings.ToUpper(strings.TrimSpace(field)) {
	case FieldPayee, FieldDescription, FieldReference, FieldChequeNumber,
		FieldAmount, FieldType, FieldAnyText:
		return true
	}
	return false
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func knownOperator(op string) bool {
	switch op {
	case "", OpContains, OpNotContains, OpEquals, OpNotEquals, OpStartsWith, OpEndsWith,
		OpGreaterThan, OpGreaterThanEqual, OpLessThan, OpLessThanEqual, OpIsBlank, OpIsNotBlank:
		return true
	}
	return false
}

// needsValue reports whether an operator compares against a literal.
func needsValue(op string) bool {
	switch op {
	case OpIsBlank, OpIsNotBlank:
		return false
	}
	return true
}

// normaliseOperator accepts both the API's upper-case operator names and the
// lower-case, underscored spellings the rule editor writes
// ("starts_with" → STARTSWITH).
func normaliseOperator(op string) string {
	return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(op)), "_", "")
}
