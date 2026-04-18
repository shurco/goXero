# internal/repository — Agent Guide

Thin pgx/v5 data-access layer. One file per aggregate, plus `repository.go`
(`Repositories` struct aggregator) and the sentinel errors in `errors.go`.

## Conventions

* **Sentinel errors** — return `ErrNotFound`, `ErrAlreadyExists`, `ErrForbidden`
  so handlers can map them via `handlers.httpError`. Don't expose
  `pgx.ErrNoRows` upwards.
* **SQL construction** — build WHERE clauses with a `strings.Builder` + numbered
  parameters (`"$" + strconv.Itoa(len(args))`). Do **not** add a helper like
  `itoa`; it was removed as redundant.
* **Multi-row loaders** — reuse the `queryMany` pattern (see
  `organisation.go#queryMany`) instead of duplicating `rows.Next()` loops.
* **Avoid N+1** — expose JOIN-backed list methods (e.g.
  `OrganisationRepository.ListForUser`) instead of asking callers to loop over
  ids.
* **Writes in transactions** — anything that mutates multiple rows (e.g.
  invoices + line items, payments + invoice totals) must run inside a
  `pool.BeginTx` block and defer `tx.Rollback`.
* **Money safety** — subtract against totals using
  `GREATEST(column - $n, 0)` so accumulated drift can never push balances
  negative (see `payment.go`).
* **Tenant ownership in writes** — any mutation that references an entity by id
  must check it belongs to the passed `orgID`. Example: `PaymentRepository.Create`
  `SELECT EXISTS` of `invoice_id` scoped to `organisation_id` before the insert.
* **Unique-violation mapping** — wrap pgconn `23505` via the shared
  `isUniqueViolation(err)` helper (`repository.go`) and return
  `ErrAlreadyExists`. Do not re-implement the SQLSTATE check per repository.
* **Column lists** — when the same `SELECT` shape is reused across `GetBy*`
  methods, extract it once (e.g. `userSelect` + `scanUser` in `user.go`)
  instead of repeating the column list inline.
* **NULLable columns** — project them with `COALESCE(col,'')` so we scan into
  `string` without hitting `cannot scan NULL`. See `organisationColumns`.
* **Base currency defaults** — when the caller omits `CurrencyCode` on
  Prepayments/Overpayments/RepeatingInvoices, resolve it via the shared
  `orgBaseCurrency(ctx, tx, orgID)` helper (`accounting_extras.go`) so the
  value matches the organisation's configured currency (falling back to USD
  only when the org itself is missing one). Never hard-code currency codes on
  write paths.
* **GL journals** — Payment voids call `postPaymentReversal(ctx, tx, orgID,
  paymentID)` which just deletes the sourced journal — reversal doesn't need
  the invoice id or amount because the existing journal already has them.

## Invoice totals

`recalculateTotals` in `invoice.go` is the single source of truth for totals
math. Tested in `invoice_test.go`. If a new line-amount type is introduced it
must be handled there and the tests extended.

## Tests

* **Unit:** `invoice_test.go` — `recalculateTotals` (no database).
* **Integration:** `integration_test.go` — `testutil.NewPool` + pgtestdb / goose
  on `migrations/`. Start: `docker compose -f compose.dev.yml up -d pgtestdb`.
  Skip DB only: `PGTESTDB_SKIP=1 go test ./internal/repository/...`
* Assertions: `github.com/stretchr/testify` (`require` / `assert`).
