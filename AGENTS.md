# goXero — Agent Guide

> Concise map of the repo for AI/automation agents. Humans should start with
> [`README.md`](./README.md); this file tells agents *where to act* and *how to
> verify changes*.

## Big picture

* Go 1.23 backend (Fiber v3, pgx/v5, goose migrations, slog) — API lives under
  `/api/*` (SPA helpers + auth) and `/api/v1/*` (accounting resources, Xero-like envelope).
* SvelteKit 2 + Svelte 5 runes frontend in `web/`, package manager is **Bun**.
* Postgres 16 dev & test services in `compose.dev.yml`.

## Module map

| Path                      | Responsibility                                           | Nested `AGENTS.md`                       |
|---------------------------|----------------------------------------------------------|-------------------------------------------|
| `cmd/server/`             | Fiber entrypoint, ErrorHandler wiring                    | —                                         |
| `internal/config/`        | Env-driven config, strict parsing, prod JWT guard        | —                                         |
| `internal/database/`      | pgxpool bootstrap                                        | —                                         |
| `internal/handlers/`      | Fiber handlers + shared `helpers.go` (parseID, httpError) | [`internal/handlers/AGENTS.md`](internal/handlers/AGENTS.md) |
| `internal/middleware/`    | JWT auth + tenant resolver                               | —                                         |
| `internal/models/`        | Domain DTOs mirroring Xero payloads                      | —                                         |
| `internal/repository/`    | pgx data access, invoice totals math                     | [`internal/repository/AGENTS.md`](internal/repository/AGENTS.md) |
| `internal/bankfeed/`      | Open Banking provider abstraction (GoCardless BAD, …)    | [`internal/bankfeed/AGENTS.md`](internal/bankfeed/AGENTS.md) |
| `internal/testutil/`      | `pgtestdb` + goose migrator → `*pgxpool.Pool` for tests   | —                                         |
| `internal/router/`        | Route registration & middleware order                    | —                                         |
| `internal/logger/`        | slog helpers                                             | —                                         |
| `migrations/`             | Goose SQL schema + seed; `fixtures/` = optional dev data | [`migrations/fixtures/README.md`](migrations/fixtures/README.md) |
| `web/`                    | SvelteKit frontend                                       | [`web/AGENTS.md`](web/AGENTS.md)          |

## Agent rules

1. **Single source of truth for helpers.** New UUID/error helpers go in
   `internal/handlers/helpers.go`; do not reintroduce `common.go` or
   repository-level `itoa` wrappers. `paginationFromQuery(c)` is the only way
   to read `?page` / `?pageSize`. For envelope/body boilerplate use the
   generics (`envelopeList`, `envelopeOne`, `rawList`, `rawOne`, `bindBody`,
   `parseYMD`, `errInvalidPayload`) — don't inline `c.JSON(fiber.Map{...})`
   or repeat `c.Bind().Body` + bespoke 400 wiring in handlers.
2. **No leaked internals.** Two layers cooperate:
   * `httpError(err)` in handlers masks domain errors (`ErrNotFound`, …) as
     safe `4xx`/`5xx`.
   * `cmd/server/main.go` `ErrorHandler` is the catch-all: any non-`*fiber.Error`
     is logged and returned as `500 internal server error`. Middleware and
     repositories therefore just return raw errors — masking is centralised.
3. **Tenant authorisation.** `middleware.Tenant` calls
   `UserRepository.HasOrganisationAccess` — a valid JWT alone is **not** enough
   to access an organisation. Any new per-org endpoint must sit behind this
   middleware (routes are wired in `internal/router`).
4. **Cross-tenant writes are refused at the repo.** E.g. `Payment.Create`
   verifies `invoice_id` belongs to `orgID` before inserting.
5. **No N+1.** When loading lists keyed by joins (user ⇄ organisations, invoice
   ⇄ contact) add a repository method that does the JOIN rather than looping
   in handlers. Invoice list/get already populates `Contact.Name` this way.
6. **Invoice status is a whitelist** (`internal/handlers/invoice.go`).
7. **Config parsing is strict.** Errors from `strconv.Atoi` / `time.ParseDuration`
   must be propagated, never swallowed. In `APP_ENV=production` the JWT secret
   must differ from the default.
8. **Duplicate domain errors.** Unique-violation `23505` surfaces as
   `repository.ErrAlreadyExists` — see `UserRepository.Create`.
9. **Frontend a11y:** every `<label>` in a form must pair with an `id` on its
   control — `bun run check` enforces this.
10. **Fixtures:** `./scripts/migrate up` applies only `migrations/*.sql`.
    `./scripts/migrate dev up` runs core first, then `migrations/fixtures` tracked
    in table `goose_fixtures`.
11. **GL posting is centralised.** `internal/repository/gl.go` owns every journal
    posting (invoices, credit notes, bank transactions, manual journals). Any new
    transactional resource must post through a helper there and balance to zero.
    When the dedicated tax control account (code `820`) is absent, tax is folded
    into the line `NetAmount` so the journal still balances.
12. **Reports are read-only aggregates over `gl_journal_lines`.** Extend
    `ReportRepository` rather than re-querying source tables; handlers live in
    `internal/handlers/report.go`.
13. **Polymorphic endpoints** (`attachments`, `history`) use the `attachmentSubjectMap`
    lookup; add new subjects there and to the router (see `internal/router`).

## API surface (v1)

Public & session helpers (non-v1):

* `POST /api/auth/login`, `POST /api/auth/register`, `GET /api/auth/me`.
* `POST /api/auth/refresh` — exchange a refresh token for a new access/refresh
  pair (rotation + reuse-detection). Public; body `{ refreshToken }`.
* `POST /api/auth/logout` — revoke the current refresh token; add
  `?everywhere=true` with a valid JWT to revoke every session for the user.
* `GET /api/organisations` — orgs the current user belongs to.
* `POST /api/organisations` — create a new organisation and link the caller
  as `ADMIN` (requires `{ "name": "…" }`; `baseCurrency` defaults to `USD`).

All under `/api/v1/*`, JWT + `Xero-Tenant-Id` required.

| Area               | Endpoints                                                                                                                                                        |
|--------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Organisation       | `GET /organisation`                                                                                                                                              |
| Accounts           | `GET/POST /accounts`, `GET/PUT/DELETE /accounts/:id`                                                                                                             |
| Tax rates          | `GET/POST /tax-rates`, `GET/PUT/DELETE /tax-rates/:id`                                                                                                           |
| Contacts           | `GET/POST /contacts`, `GET/PUT /contacts/:id`                                                                                                                    |
| Contact groups     | `GET/POST /contact-groups`, `GET/PUT/DELETE /contact-groups/:id`, `PUT /contact-groups/:id/contacts`, `DELETE /contact-groups/:id/contacts/:contactId`           |
| Items              | `GET/POST /items`, `GET/PUT /items/:id`                                                                                                                          |
| Invoices           | `GET/POST /invoices`, `GET/PUT/DELETE /invoices/:id`, `GET /invoices/:id/payments`                                                                               |
| Credit notes       | `GET/POST /credit-notes`, `GET/PUT/DELETE /credit-notes/:id`, `PUT /credit-notes/:id/allocations`                                                                |
| Payments           | `GET/POST /payments`, `GET /payments/:id`, `DELETE /payments/:id` (void)                                                                                         |
| Bank transactions  | `GET/POST /bank-transactions`, `GET/DELETE /bank-transactions/:id`                                                                                               |
| Bank transfers     | `GET/POST /bank-transfers`, `GET /bank-transfers/:id`                                                                                                            |
| Manual journals    | `GET/POST /manual-journals`, `GET/DELETE /manual-journals/:id`                                                                                                   |
| Journals (GL)      | `GET /journals`                                                                                                                                                  |
| Quotes             | `GET/POST /quotes`, `GET/PUT/DELETE /quotes/:id`                                                                                                                 |
| Purchase orders    | `GET/POST /purchase-orders`, `GET/PUT/DELETE /purchase-orders/:id`                                                                                               |
| Currencies         | `GET/POST /currencies`                                                                                                                                           |
| Branding themes    | `GET/POST /branding-themes`                                                                                                                                      |
| Tracking           | `GET/POST /tracking-categories`, `GET/PUT/DELETE /tracking-categories/:id`, `PUT /tracking-categories/:id/options`                                               |
| Attachments        | `GET /:subject/:id/attachments`, `POST /:subject/:id/attachments/:fileName`, `GET /:subject/:id/attachments/:attachmentId`                                       |
| History            | `GET /:subject/:id/history`, `PUT /:subject/:id/history`                                                                                                         |
| Prepayments        | `GET/POST /prepayments`, `GET/PUT /prepayments/:id`                                                                                                              |
| Overpayments       | `GET/POST /overpayments`, `GET/PUT /overpayments/:id`                                                                                                            |
| Repeating invoices | `GET/POST /repeating-invoices`, `GET/PUT/DELETE /repeating-invoices/:id`                                                                                         |
| Batch payments     | `GET/POST /batch-payments`, `GET /batch-payments/:id`                                                                                                            |
| Linked transactions| `GET/POST /linked-transactions`, `GET/PUT/DELETE /linked-transactions/:id`                                                                                       |
| Employees          | `GET/POST /employees`, `GET/PUT/DELETE /employees/:id`                                                                                                           |
| Receipts           | `GET/POST /receipts`, `GET/PUT /receipts/:id`                                                                                                                    |
| Expense claims     | `GET/POST /expense-claims`, `GET/PUT /expense-claims/:id`                                                                                                        |
| Users              | `GET /users`                                                                                                                                                     |
| Bank feeds         | `GET /bank-feeds/providers`, `GET /bank-feeds/institutions`, `GET/POST /bank-feeds/connections`, `GET/DELETE /bank-feeds/connections/:id`, `POST /bank-feeds/connections/:id/finalize`, `POST /bank-feeds/connections/:id/sync`, `PUT /bank-feeds/accounts/:feedAccountId`, `GET /bank-feeds/statement-lines`, `POST /bank-feeds/statement-lines/:id/import`, `POST /bank-feeds/statement-lines/:id/ignore` |
| Reports            | `GET /reports/trial-balance`, `/profit-and-loss`, `/balance-sheet`, `/aged-receivables`, `/aged-payables`, `/bank-summary`, `/cash-summary`, `/executive-summary`, `/budget-summary`, `/bas`, `/journal-report`, `/invoice-summary` |

`:subject` for attachments/history is one of: `invoices`, `credit-notes`, `bank-transactions`, `contacts`, `accounts`, `manual-journals`, `quotes`, `purchase-orders`, `receipts`, `expense-claims`.

### Xero alignment & caveats

* Envelope for `GET` lists: `{ Id, Status, ProviderName, DateTimeUTC, Payload: { <Resource>: [...] , Pagination? } }`.
  `POST`/`PUT` responses return the raw `{ <Resource>: [...] }` to match Xero.
* `DELETE` semantics mirror Xero: invoices/payments/credit-notes/manual-journals are
  soft-voided (status transitions to `VOIDED`/`DELETED`) and GL entries are reversed.
* Reports mirror Xero's shape (`Reports[].Rows[]`) but are computed locally from
  `gl_journal_lines`; branding/tracking decorations are out of scope for v1.
* Attachment storage is DB-backed (`attachments.content BYTEA`) — fine for demo,
  swap for object storage in production.
* Idempotency keys (`Idempotency-Key` header) are not yet enforced; mutating
  requests are safe to retry only when the caller checks for duplicates.

## Verification checklist

Run before handing work back:

```bash
go vet ./...
docker compose -f compose.dev.yml up -d pgtestdb   # integration tests; optional if skipping
go test ./...             # set PGTESTDB_SKIP=1 to skip DB-backed tests only
cd web && bun run check
cd web && bun run build    # optional, needed for UI-heavy changes
```

Integration tests use [pgtestdb](https://github.com/peterldowns/pgtestdb) on
`localhost:5433` by default (`PGTESTDB_*` env vars override). Assertions use
[testify](https://github.com/stretchr/testify) project-wide in `*_test.go`.
