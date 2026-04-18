# goXero

**Disclaimer:** This repository is **not affiliated with, endorsed by, or connected in any way** to [Xero Limited](https://www.xero.com) or the official Xero product. Naming and UI patterns are **inspired only** by Xero’s public documentation and familiar workflows; all trademarks belong to their respective owners.

### Why this project exists

Over the last few years, prices for comparable cloud accounting software have gone up several times, while the slice of functionality I actually use has not kept pace. I was tired of paying for unused surface area, so I started **goXero** as a **free, open-source** alternative you can self-host and adapt to your own needs.

---

A **Xero.com-style accounting platform** — open-source clone built on top of the
[Xero Accounting API data model](https://developer.xero.com/documentation/api/accounting/overview).

* **Backend** — Go 1.23+ with [Fiber v3](https://github.com/gofiber/fiber),
  [pgx/v5](https://github.com/jackc/pgx) for Postgres, and
  [goose](https://github.com/pressly/goose) for migrations.
* **Frontend** — SvelteKit 2 + Svelte 5 (runes) with TailwindCSS, Xero-inspired
  teal palette, clean sidebar layout, dashboards and CRUD flows; **Bun** for
  install and scripts (no npm).
* **Multi-tenant**: every resource is scoped by `OrganisationID`, resolved from
  the `Xero-Tenant-Id` HTTP header (same convention as Xero) **and** validated
  against the authenticated user's `organisation_users` membership — a valid
  JWT alone is not enough to access a tenant.

---

## 🌳 Project layout

```
goxero/
├── cmd/
│   └── server/         # Fiber v3 HTTP server entrypoint
├── scripts/
│   ├── migrate           # goose migrations (up/down/status/create; needs goose CLI)
│   └── _helper           # shared bash helpers for scripts
├── internal/
│   ├── config/         # Env-driven configuration
│   ├── database/       # pgx pool bootstrap
│   ├── handlers/       # Fiber handlers (auth, invoices, contacts, …)
│   ├── middleware/     # JWT auth + tenant resolver
│   ├── models/         # Domain structs (mirrors Xero response shapes)
│   ├── repository/     # pgx data-access layer
│   ├── router/         # Route registration & middleware wiring
│   └── logger/         # slog helpers
├── migrations/         # goose SQL migrations (+ fixtures/ for dev-only data)
├── web/                # SvelteKit frontend (Tailwind)
│   ├── src/
│   │   ├── lib/        # API client, stores, types, utils
│   │   └── routes/     # /login, /register, /app/*
│   └── …
├── compose.dev.yml     # Postgres dev & test services
├── AGENTS.md           # Agent-facing map of the repo
├── Makefile            # Convenience commands
└── .env.example
```

## 🚀 Quick start

### 1 · Start Postgres

```bash
make docker-up
```

### 2 · Configure & migrate

```bash
cp .env.example .env
go install github.com/pressly/goose/v3/cmd/goose@latest   # once: standalone goose CLI
make migrate            # ./scripts/migrate up — schema + demo seed only
```

Optional **second tenant + draft invoice** (real UUIDs) for local dev / integration tests:

```bash
./scripts/migrate dev up   # core up, then migrations/fixtures (see migrations/fixtures/README.md)
```

Seed creates:

| What           | Value                                       |
|----------------|---------------------------------------------|
| Organisation   | Demo Company (Global) — `6823b27b-c48f-4099-bb27-4202a4f496a2` |
| Admin email    | `admin@demo.local`                          |
| Admin password | `admin123`                                  |
| Accounts       | A simplified default Chart of Accounts      |
| Tax rates      | Tax-exempt / Sales / Purchases              |
| Contacts       | 3 demo customers & suppliers                |
| Items          | `CONS-01`, `WIDGET`                         |

### 3 · Run the backend

```bash
make run       # listens on :8080
```

### 4 · Run the frontend

Requires [Bun](https://bun.sh/) on `PATH`.

```bash
make web-install   # bun install in web/
make web-dev       # http://localhost:5173 with proxy to :8080
```

Open [http://localhost:5173](http://localhost:5173) and sign in with
`admin@demo.local` / `admin123`.

---

## 🧩 API surface

`goXero` exposes two groups of endpoints:

### Plain JSON (helpers for the SPA)

| Method | Path                  | Purpose                                     |
|--------|-----------------------|---------------------------------------------|
| POST   | `/api/auth/register`  | Create a user + issue JWT                   |
| POST   | `/api/auth/login`     | Email/password login → JWT + tenants        |
| GET    | `/api/auth/me`        | Current user + accessible organisations     |
| GET    | `/api/organisations`  | All organisations the user can access       |
| GET    | `/health`             | Liveness probe                              |

### Xero-compatible Accounting API

All under `/api/v1/…`, authenticated with `Authorization: Bearer <JWT>` **and**
`Xero-Tenant-Id: <OrganisationID>` (exactly as Xero does).

| Resource           | Endpoints                                                                                                                                                 |
|--------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------|
| Organisation         | `GET /organisation`                                                                                                                                       |
| Accounts             | `GET/POST /accounts` · `GET/PUT/DELETE /accounts/:id`                                                                                                     |
| Tax rates            | `GET/POST /tax-rates` · `GET/PUT/DELETE /tax-rates/:id`                                                                                                   |
| Contacts             | `GET/POST /contacts` · `GET/PUT /contacts/:id`                                                                                                            |
| Contact groups       | `GET/POST /contact-groups` · `GET/PUT/DELETE /contact-groups/:id` · `PUT /contact-groups/:id/contacts` · `DELETE /contact-groups/:id/contacts/:contactId` |
| Items                | `GET/POST /items` · `GET/PUT /items/:id`                                                                                                                  |
| Invoices             | `GET/POST /invoices` · `GET/PUT/DELETE /invoices/:id` · `GET /invoices/:id/payments`                                                                      |
| Credit notes         | `GET/POST /credit-notes` · `GET/PUT/DELETE /credit-notes/:id` · `PUT /credit-notes/:id/allocations`                                                       |
| Payments             | `GET/POST /payments` · `GET /payments/:id` · `DELETE /payments/:id` *(void)*                                                                              |
| Prepayments          | `GET/POST /prepayments` · `GET/PUT /prepayments/:id`                                                                                                      |
| Overpayments         | `GET/POST /overpayments` · `GET/PUT /overpayments/:id`                                                                                                    |
| Bank transactions    | `GET/POST /bank-transactions` · `GET/DELETE /bank-transactions/:id`                                                                                       |
| Bank transfers       | `GET/POST /bank-transfers` · `GET /bank-transfers/:id`                                                                                                    |
| Manual journals      | `GET/POST /manual-journals` · `GET/DELETE /manual-journals/:id`                                                                                           |
| Journals (GL feed)   | `GET /journals`                                                                                                                                           |
| Quotes               | `GET/POST /quotes` · `GET/PUT/DELETE /quotes/:id`                                                                                                         |
| Purchase orders      | `GET/POST /purchase-orders` · `GET/PUT/DELETE /purchase-orders/:id`                                                                                       |
| Repeating invoices   | `GET/POST /repeating-invoices` · `GET/PUT/DELETE /repeating-invoices/:id`                                                                                 |
| Batch payments       | `GET/POST /batch-payments` · `GET /batch-payments/:id`                                                                                                    |
| Linked transactions  | `GET/POST /linked-transactions` · `GET/PUT/DELETE /linked-transactions/:id`                                                                               |
| Employees            | `GET/POST /employees` · `GET/PUT/DELETE /employees/:id`                                                                                                   |
| Receipts             | `GET/POST /receipts` · `GET/PUT /receipts/:id`                                                                                                            |
| Expense claims       | `GET/POST /expense-claims` · `GET/PUT /expense-claims/:id`                                                                                                |
| Currencies           | `GET/POST /currencies`                                                                                                                                    |
| Branding themes      | `GET/POST /branding-themes`                                                                                                                               |
| Tracking             | `GET/POST /tracking-categories` · `GET/PUT/DELETE /tracking-categories/:id` · `PUT /tracking-categories/:id/options`                                      |
| Users                | `GET /users`                                                                                                                                              |
| Bank feeds           | `GET /bank-feeds/providers` · `GET /bank-feeds/institutions` · `GET/POST /bank-feeds/connections` · `GET/DELETE /bank-feeds/connections/:id` · `POST /bank-feeds/connections/:id/finalize` · `POST /bank-feeds/connections/:id/sync` · `PUT /bank-feeds/accounts/:feedAccountId` · `GET /bank-feeds/statement-lines` · `POST /bank-feeds/statement-lines/:id/import` · `POST /bank-feeds/statement-lines/:id/ignore` |
| Attachments          | `GET /:subject/:id/attachments` · `POST /:subject/:id/attachments/:fileName` · `GET /:subject/:id/attachments/:attachmentId`                              |
| History              | `GET /:subject/:id/history` · `PUT /:subject/:id/history`                                                                                                 |
| Reports              | `GET /reports/trial-balance` · `/profit-and-loss` · `/balance-sheet` · `/aged-receivables` · `/aged-payables` · `/bank-summary` · `/cash-summary` · `/executive-summary` · `/budget-summary` · `/bas` · `/journal-report` · `/invoice-summary` |

`:subject` accepts `invoices`, `credit-notes`, `bank-transactions`, `contacts`,
`accounts`, `manual-journals`, `quotes`, `purchase-orders`, `receipts`,
`expense-claims`.

### Connecting a bank via Open Banking

goXero ships with an adapter for **GoCardless Bank Account Data** (ex-Nordigen,
free PSD2 tier covering 2 500+ EU/UK banks). Hooking it up:

1. Create an account at [bankaccountdata.gocardless.com](https://bankaccountdata.gocardless.com)
   and generate `secret_id` / `secret_key` under **User Secrets**.
2. Drop them into `.env`:

   ```
   GOCARDLESS_BAD_SECRET_ID=...
   GOCARDLESS_BAD_SECRET_KEY=...
   BANKFEED_REDIRECT_URL=http://localhost:5173/app/bank-feeds/callback
   ```

3. Restart the server — goXero discovers the adapter and exposes
   `/api/v1/bank-feeds/*` for the authenticated tenant.
4. Consent flow:

   ```bash
   # 1) pick a bank
   curl "$API/bank-feeds/institutions?provider=gocardless_bad&country=GB"

   # 2) create a connection (returns AuthURL — redirect the user there)
   curl -X POST $API/bank-feeds/connections \
        -H "Content-Type: application/json" \
        -d '{"Provider":"gocardless_bad","InstitutionID":"SANDBOXFINANCE_SFIN0000"}'

   # 3) after the user returns, finalise to discover accounts
   curl -X POST $API/bank-feeds/connections/<id>/finalize

   # 4) pull transactions (idempotent — re-run as often as you like)
   curl -X POST $API/bank-feeds/connections/<id>/sync

   # 5) review the staging inbox, then import the rows you want posted
   curl "$API/bank-feeds/statement-lines?status=NEW"
   curl -X POST $API/bank-feeds/statement-lines/<lineId>/import
   ```

Statement lines are deduped by `(FeedAccountID, ProviderTxID)`, so re-syncing
never double-counts. Rows you don't want posted can be hidden via
`/bank-feeds/statement-lines/<lineId>/ignore`. Adding another aggregator
(Plaid, TrueLayer, Salt Edge) is purely a new `Provider` implementation under
`internal/bankfeed/` — no schema or handler changes.

`GET` list responses follow Xero's envelope (`{ Id, Status, ProviderName,
DateTimeUTC, Payload: { <Resource>: [...], Pagination? } }`). `POST`/`PUT` return
the raw `{ <Resource>: [...] }` payload to match Xero.

**Reports** are computed on the fly from General Ledger journal lines
(`gl_journal_lines` table, populated centrally by `internal/repository/gl.go`)
— every posted invoice, credit note, bank transaction and manual journal writes
balanced entries there. When the dedicated tax control account (code `820`) is
missing from a tenant's chart of accounts, the tax portion is folded into the
revenue/expense line so the journal still balances to zero.

### Example: listing invoices

```bash
curl "http://localhost:8080/api/v1/invoices?type=ACCREC&status=AUTHORISED" \
     -H "Authorization: Bearer $TOKEN" \
     -H "Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2"
```

### Example: creating an invoice

```bash
curl -X POST http://localhost:8080/api/v1/invoices \
  -H "Authorization: Bearer $TOKEN" \
  -H "Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2" \
  -H "Content-Type: application/json" \
  -d '{
    "Type": "ACCREC",
    "ContactID": "<contact-uuid>",
    "Date": "2026-04-18",
    "DueDate": "2026-05-18",
    "LineAmountTypes": "Exclusive",
    "Status": "DRAFT",
    "LineItems": [
      { "Description": "Consulting", "Quantity": 10, "UnitAmount": 150, "AccountCode": "200" }
    ]
  }'
```

---

## 🖥️ Frontend

The SvelteKit app mirrors Xero's familiar information architecture:

* **Dashboard** — KPI tiles (receivable, received, drafts, overdue) + invoices-by-status breakdown + recent invoices table.
* **Invoices** — tabbed listing (`ACCREC` / `ACCPAY`), filters, search, pagination, detailed view with approve / void / record payment actions.
* **Create invoice** — multi-line form with account + item picker, live totals and Draft / Approve actions.
* **Contacts** — tabs (All / Customers / Suppliers), search, create-contact modal.
* **Chart of accounts** — grouped by Assets / Liabilities / Equity / Revenue / Expenses.
* **Products & services** — item catalogue with sales/purchase pricing.
* **Payments** — ledger of applied payments.
* **Reports** — rollup derived from `reports/invoice-summary`.

Styling: custom **teal-first brand palette** (`brand-*`) plus Inter font.
Components use Tailwind utilities with a small set of semantic classes
(`.btn-primary`, `.card`, `.badge-paid`, …) in `src/app.css`.

---

## 🔧 Developer commands

```bash
make help                  # full list
make docker-up             # start Postgres
make migrate               # ./scripts/migrate up (core only)
./scripts/migrate dev up   # core + SQL fixtures (extra org & invoice)
make migrate-down          # rollback last
make migrate-reset         # roll back all migrations
make migrate-status        # list status
make migrate-create NAME=x # new migration (goose create)
make run                   # start Fiber server
make build                 # compile binaries to ./bin
make test                  # go test ./... (needs pgtestdb on :5433 unless PGTESTDB_SKIP=1)
make vet                   # go vet ./...
make web-install           # bun install in web/
make web-dev               # vite dev server
make web-build             # production bundle
make web-check             # svelte-check (types + a11y)
```

### Configuration safety net

* `config.Load()` fails fast when `SERVER_PORT`, `DB_PORT`, `JWT_ACCESS_TTL`,
  etc. are malformed — no more silent fallbacks.
* Running with `APP_ENV=production` **and** the stock `JWT_SECRET` aborts the
  boot so insecure defaults never reach a production deployment.

---

## 📐 Assumptions made while building

The task asked for *"a clone of xero.com based on the Accounting API docs"*
which is very large. Sensible defaults were chosen and documented below:

1. **Scope** — the Accounting API core resources are implemented (Organisation,
   Users, Accounts, Tax rates, Contacts + Contact groups, Items, Invoices
   + line items, Credit notes + allocations, Payments, Bank transactions, Bank
   transfers, Manual journals, Journals (read-only GL feed), Quotes, Purchase
   orders, Currencies, Branding themes, Tracking categories + options,
   Attachments, History records) and the core **Reports** (Trial Balance,
   Profit & Loss, Balance Sheet, Aged Receivables / Payables, Bank Summary,
   plus the dashboard-friendly Invoice Summary). OAuth2 consent screens and
   webhooks are still out of scope.
2. **Authentication** — JWT-based login/register is implemented in place of the
   full Xero OAuth2/OIDC flow (which requires a certified provider).
3. **Tenant resolution** — the Xero `Xero-Tenant-Id` header is honoured so real
   Xero SDKs can point at goXero with minimal friction.
4. **Decimals** — monetary fields use `NUMERIC(18,4)` and the Go
   `shopspring/decimal` type (same precision Xero uses).
5. **Recalculation** — invoice totals are recomputed server-side from line
   items on every create. For `Exclusive` pricing the backend sums
   `qty × price − discount`, tax comes from the payload. `Inclusive` / `NoTax`
   paths preserve the supplied totals (mirrors Xero's behaviour).
6. **Demo data** — the seed migration `00009_seed_demo.sql` populates a demo
   organisation, admin user, chart of accounts, tax rates, contacts and items
   so the UI has something to display right after `make migrate`.
7. **Frontend** — SvelteKit 2 + Svelte 5 runes under `web/`, **Bun** for
   `install` / `run`; Vite dev proxy for `/api` so both sides run
   concurrently without CORS hassle.
8. **Styling** — custom Tailwind palette rather than pulling a component
   library, to stay closer to Xero's own clean aesthetic.

---

## Disclaimer and limitation of liability

This project is **free / open-source software**, provided **“as is”** and **without warranties of any kind**, whether express or implied, including but not limited to implied warranties of merchantability, fitness for a particular purpose, title, and non-infringement.

**No liability:** To the fullest extent permitted by applicable law, the maintainers, contributors, and copyright holders **disclaim all liability** for any direct, indirect, incidental, special, exemplary, or consequential damages, or any loss of profits, data, goodwill, or business interruption, arising out of or in connection with the use or inability to use this software — even if advised of the possibility of such damages.

**Not professional advice:** goXero is **not** certified or audited accounting, tax, payroll, or legal software. Nothing in this repository constitutes financial, tax, legal, or regulatory advice. **You alone** are responsible for the accuracy of your books, filings, integrations, backups, security, and compliance with the laws and professional standards that apply to you and your organisation.

**Use at your own risk:** Deploying goXero for production or regulated workloads is entirely **your decision** and **your responsibility**, including obtaining any licences, insurance, or professional review you may require.

PRs welcome :)
