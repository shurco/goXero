-- +goose Up
-- +goose StatementBegin
-- The chart's control accounts get their Xero system-account roles.
--
-- WHY THIS EXISTS.  Application code used to name the accounts it posts to by
-- account CODE: "820" for the sales tax account, "610"/"800" for the two
-- document control accounts.  A code is the organisation's own label for an
-- account and Xero lets it be anything, so code "820" is a sales tax account in
-- this chart and an arbitrary expense in the next one.  Every one of those
-- literals is now gone from the code, and the accounts are resolved by the role
-- Xero itself uses: Account.SystemAccount.
--
-- THE ROLES ARE XERO'S OWN, VERBATIM.  The enum is taken from Xero's published
-- OpenAPI description of the Account resource -- xero_accounting.yaml,
-- components.schemas.Account.properties.SystemAccount, whose description reads
-- "If this is a system account then this element is returned. See System Account
-- types."  The strings below are copied from it, not paraphrased: in particular
-- the role for an unpaid expense claim is UNPAIDEXPCLM and the role for a
-- historical adjustment is HISTORICAL, not the longer spellings they invite.
--
--     DEBTORS  CREDITORS  BANKCURRENCYGAIN  GST  GSTONIMPORTS  HISTORICAL
--     REALISEDCURRENCYGAIN  RETAINEDEARNINGS  ROUNDING  TRACKINGTRANSFERS
--     UNPAIDEXPCLM  UNREALISEDCURRENCYGAIN  WAGEPAYABLES  CIS*  ""
--
-- HOW EACH ROW BELOW WAS DERIVED, so that none of it is a guess:
--
--   * 820 Sales Tax -> GST is MEASURED, not inferred.  The account's own
--     description in the capture says "Xero has been designed to use only one
--     sales tax account to track sales taxes on income and expenses", and its
--     balance for the captured period is 422.59 -- the figure Xero's Sales Tax
--     report prints as Net Tax and as Tax Collected less Tax Paid
--     (docs/xero-reference/general-ledger-detail.txt lines 387 and 499; the
--     report's own capture agrees).  An account whose balance IS the sales tax
--     liability is the sales tax account.  This is the role the write path
--     reads, and tagging it is what removes the "820" literal from
--     internal/repository/gl.go.
--
--   * 801 Unpaid Expense Claims -> UNPAIDEXPCLM is Xero's own name for the
--     role: the string "Unpaid Expense Claims" is the account Xero's default
--     chart carries for UNPAIDEXPCLM, and this chart's 801 is that account,
--     copied from that chart.  It is the control account the Cash Summary looks
--     through for expense-claim payments (docs/cash-summary-xero-rule.md), so
--     the role is what that report resolves.
--
--   * 840 Historical Adjustment -> HISTORICAL, 860 Rounding -> ROUNDING,
--     877 Tracking Transfers -> TRACKINGTRANSFERS, 960 Retained Earnings ->
--     RETAINEDEARNINGS, 497 Bank Revaluations -> BANKCURRENCYGAIN,
--     498 Unrealised Currency Gains -> UNREALISEDCURRENCYGAIN and
--     499 Realised Currency Gains -> REALISEDCURRENCYGAIN are the same kind of
--     identity: in each case the account name is verbatim the name Xero's
--     default chart gives the account holding that role, and the account's
--     position in this chart agrees (Historical Adjustment, Rounding, Tracking
--     Transfers and the rest are Xero's own system accounts, which is why this
--     chart carries them at all -- an ordinary chart does not acquire an
--     account called "Tracking Transfers").
--
--     These seven are TAGGED BUT NOT READ: no code resolves them today, so
--     tagging them changes no figure in any report.  They are tagged because
--     the tag is the fact, and because a chart whose roles are declared is the
--     precondition for any later code reading one -- the alternative is a
--     future reader reintroducing a code literal because the role was missing.
--     If a later measurement shows one of these seven is wrong, only the tag is
--     wrong; nothing depends on it yet.
--
--   * 610 Accounts Receivable -> DEBTORS and 800 Accounts Payable -> CREDITORS
--     are already set by 00009 and 00023 and are re-asserted here only so that
--     this migration leaves the whole set of roles it depends on in place.
--
-- WHAT IS DELIBERATELY NOT TAGGED.  The remaining current liabilities of this
-- chart -- 825 Employee Tax Payable, 826 Superannuation Payable, 830 Income Tax
-- Payable, 835 Revenue Received in Advance, 850 Suspense, 855 Clearing Account,
-- 880 Owner A Drawings, 881 Owner A Funds Introduced -- are left untagged.  Two
-- of Xero's roles (WAGEPAYABLES, GSTONIMPORTS) could be argued onto 825 and
-- none, but arguing is not evidence: this chart came from a Xero demo
-- organisation that does not run payroll, there is no payroll data in the
-- capture to measure against, and a role asserted without evidence is exactly
-- the kind of claim this migration exists to avoid.  An untagged account is
-- readable as untagged, which is the honest state.
--
-- No figure in this migration is computed.  It writes one column on rows that
-- already exist and touches no other table.

UPDATE accounts SET system_account = 'DEBTORS'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '610';
UPDATE accounts SET system_account = 'CREDITORS'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '800';

UPDATE accounts SET system_account = 'GST'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '820';
UPDATE accounts SET system_account = 'UNPAIDEXPCLM'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '801';

UPDATE accounts SET system_account = 'HISTORICAL'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '840';
UPDATE accounts SET system_account = 'ROUNDING'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '860';
UPDATE accounts SET system_account = 'TRACKINGTRANSFERS'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '877';
UPDATE accounts SET system_account = 'RETAINEDEARNINGS'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '960';

UPDATE accounts SET system_account = 'BANKCURRENCYGAIN'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '497';
UPDATE accounts SET system_account = 'UNREALISEDCURRENCYGAIN'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '498';
UPDATE accounts SET system_account = 'REALISEDCURRENCYGAIN'
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND code = '499';

-- Self-check: the four roles the code reads must have resolved to exactly one
-- account each.  A chart that tagged two accounts with one role would make the
-- resolution below arbitrary, and this migration is the place that says so
-- rather than a report quietly picking one.
DO $$
DECLARE role text; n int;
BEGIN
  FOREACH role IN ARRAY ARRAY['DEBTORS','CREDITORS','GST','UNPAIDEXPCLM'] LOOP
    SELECT count(*) INTO n FROM accounts
     WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2' AND system_account = role;
    IF n <> 1 THEN
      RAISE EXCEPTION 'role % resolves to % accounts, expected exactly 1', role, n;
    END IF;
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Back to the two roles 00009/00023 set, and nothing else.
UPDATE accounts SET system_account = NULL
 WHERE organisation_id = '6823b27b-c48f-4099-bb27-4202a4f496a2'
   AND system_account NOT IN ('DEBTORS', 'CREDITORS');
-- +goose StatementEnd
