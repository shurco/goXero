# internal/handlers — Agent Guide

Fiber v3 handlers. They depend on `internal/repository` for persistence and
`internal/middleware` for auth / tenant context.

## Conventions

* **Errors** — always wrap repository/domain errors with `httpError(err)` from
  `helpers.go`. It maps sentinel errors to 404/409/403 and masks anything else as
  `500 internal server error` (and logs the original). Never return
  `err.Error()` directly to clients.
* **Routing parameters** — parse path UUIDs with `parseID(c, "id")`; optional
  body UUIDs via `parseOptionalUUID(str, "label")`. Both return 400 Fiber errors
  on malformed input. Do **not** call `uuid.Parse` directly inside handlers.
  Parse `YYYY-MM-DD` strings via `parseYMD` for a single, consistent error
  message.
* **Tenant + id in one call** — the `orgID, id, err := tenantAndID(c)` helper
  collapses the ubiquitous `OrganisationIDFrom + parseID(":id")` pair. Use it
  in every `GET /:id`, `PUT /:id`, `DELETE /:id` handler.
* **204 replies** — use `return noContent(c)` instead of
  `c.SendStatus(fiber.StatusNoContent)`; keeps the style uniform.
* **Body decoding** — use `bindBody[T](c)` which returns `*T` + a pre-formatted
  `errInvalidPayload` on decode errors. Don't repeat `c.Bind().Body(&x)` +
  bespoke 400 wiring in every handler.
* **Response shape** — Xero-compatible GET endpoints wrap the payload in the
  canonical `APIResponse` envelope via `envelopeList[T]` / `envelopeOne[T]`.
  POST/PUT endpoints (where Xero returns the resource without the outer
  envelope) use `rawList` / `rawOne`. Handlers that emit paginated lists return
  `fiber.Map{Key: list, "Pagination": p}` directly.
* **Status whitelisting** — invoice status updates must go through
  `validInvoiceStatuses` in `invoice.go`. Add new states by extending that map
  and the `InvoiceStatus*` constants in `internal/models/invoice.go`.
* **Tenant context** — always read `middleware.OrganisationIDFrom(c)` /
  `middleware.UserIDFrom(c)` inside authenticated handlers; never trust
  client-supplied tenant ids. The tenant middleware has already verified
  membership, so the org id on the context is safe to use in queries.
* **Pagination** — read `?page` / `?pageSize` via `paginationFromQuery(c)`.
  Rolling your own `strconv.Atoi` on those query params is a DRY violation and
  will be rejected in review.

## Tests

`helpers_test.go` covers error masking + optional UUID parsing; extend here when
adding helpers. Repository-bound handlers stay integration-test territory and
live next to `internal/repository` when added.

## Route map (`/api/v1`)

Snapshot of the Xero-compatible surface wired in `internal/router/router.go`.
New handlers must be added here so the SPA's `web/src/lib/api.ts` stays in sync.

### Auth / Organisation

* `POST /api/auth/login`, `/register`, `GET /api/auth/me`
* `POST /api/auth/refresh` — opaque refresh token exchange (rotation +
  reuse-detection; revokes the whole family on reuse of a revoked token).
* `POST /api/auth/logout` — revoke current refresh token; with a valid JWT
  and `?everywhere=true` revoke every refresh token for the user.
* `GET /api/organisations` — list the current user's orgs
* `GET /api/v1/organisation` — Xero-shaped current org
* `GET /api/v1/users` — list users of the active org

### Core accounting resources

* `Accounts`      — `GET/POST/PUT /api/v1/accounts[/{id}]`
* `TaxRates`      — `GET/POST/PUT/DELETE /api/v1/tax-rates[/{id}]`
* `Contacts`      — `GET/POST/PUT /api/v1/contacts[/{id}]`
* `ContactGroups` — `GET/POST/PUT/DELETE /api/v1/contact-groups[/{id}]`,
  plus `/contacts[/{contactId}]` sub-resource.
* `Items`         — `GET/POST/PUT /api/v1/items[/{id}]`
* `Invoices`      — `GET/POST/PUT/DELETE /api/v1/invoices[/{id}]`
  * `GET /api/v1/invoices/{id}/payments`
  * `POST /api/v1/invoices/{id}/email`
  * `GET /api/v1/invoices/{id}/online-invoice`
* `Payments`      — `GET/POST/DELETE /api/v1/payments[/{id}]`
* `CreditNotes`   — `GET/POST/PUT/DELETE /api/v1/credit-notes[/{id}]`
  + `PUT /api/v1/credit-notes/{id}/allocations`
* `BankTransactions` — `GET/POST/DELETE /api/v1/bank-transactions[/{id}]`
* `BankTransfers`    — `GET/POST /api/v1/bank-transfers[/{id}]`
* `ManualJournals`   — `GET/POST/DELETE /api/v1/manual-journals[/{id}]`
* `Journals`         — `GET /api/v1/journals` (read-only)
* `Quotes`           — `GET/POST/PUT/DELETE /api/v1/quotes[/{id}]`
* `PurchaseOrders`   — `GET/POST/PUT/DELETE /api/v1/purchase-orders[/{id}]`
* `TrackingCategories` — `GET/POST/PUT/DELETE /api/v1/tracking-categories[/{id}]`
  + `PUT /api/v1/tracking-categories/{id}/options`
* `Currencies`       — `GET/POST /api/v1/currencies`
* `BrandingThemes`   — `GET/POST /api/v1/branding-themes`

### Extra accounting resources (migration 00013)

* `Prepayments`        — `GET/POST/DELETE /api/v1/prepayments[/{id}]`
* `Overpayments`       — `GET/POST/DELETE /api/v1/overpayments[/{id}]`
* `RepeatingInvoices`  — `GET/POST/PUT/DELETE /api/v1/repeating-invoices[/{id}]`
* `BatchPayments`      — `GET/POST/DELETE /api/v1/batch-payments[/{id}]`
* `LinkedTransactions` — `GET/POST/DELETE /api/v1/linked-transactions[/{id}]`
* `Employees`          — `GET/POST/PUT/DELETE /api/v1/employees[/{id}]`
* `Receipts`           — `GET/POST/PUT/DELETE /api/v1/receipts[/{id}]`
* `ExpenseClaims`      — `GET/POST/PUT/DELETE /api/v1/expense-claims[/{id}]`

### Polymorphic sub-resources

For any `{subject} ∈ {invoices, credit-notes, bank-transactions, contacts,
accounts, receipts, expense-claims, manual-journals, quotes, purchase-orders}`:

* `GET /api/v1/{subject}/{id}/attachments`
* `POST /api/v1/{subject}/{id}/attachments/{fileName}` (raw body)
* `GET /api/v1/{subject}/{id}/attachments/{attachmentId}` (binary download)
* `GET /api/v1/{subject}/{id}/history`
* `PUT /api/v1/{subject}/{id}/history` (`HistoryRecords:[{Details}]`)

### Reports (Xero `ReportsEnvelope` shape)

All reports accept optional `date`, `fromDate`, `toDate`, `periods`,
`timeframe`, `trackingCategoryID` / `trackingOptionID` query parameters where
they make sense.

* `GET /api/v1/reports` — discovery
* `GET /api/v1/reports/trial-balance`
* `GET /api/v1/reports/profit-and-loss`
* `GET /api/v1/reports/balance-sheet`
* `GET /api/v1/reports/aged-receivables`, `/aged-payables`
* `GET /api/v1/reports/bank-summary`, `/cash-summary`
* `GET /api/v1/reports/executive-summary`, `/budget-summary`
* `GET /api/v1/reports/bas`, `/journal-report`
* `GET /api/v1/reports/invoice-summary` (SPA-only convenience shape)
