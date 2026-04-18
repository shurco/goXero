# web/ — Agent Guide

SvelteKit 2 + Svelte 5 (runes) frontend. Managed with **Bun**; do not add
`package-lock.json` or run `npm`.

## Layout

* `src/lib/api.ts` — fetch wrapper that injects `Authorization` + `Xero-Tenant-Id`
  headers and redirects to `/login` on 401. Only clients that are actually
  used by a page live here (`authApi`, `orgApi`, `accountApi`, `contactApi`,
  `itemApi`, `invoiceApi`, `paymentApi`, `bankTransactionApi`,
  `manualJournalApi`, `bankFeedApi`). Unused endpoints are intentionally
  **not** pre-wired — add a slim client next to the page that first needs
  it rather than growing a speculative SDK.
* `src/lib/stores/session.ts` — persisted session (token, email, tenants).
* `src/lib/types.ts` — mirrors Go DTOs from `internal/models`.
* `src/lib/utils/format.ts` — currency/date/status helpers.
* `src/lib/components/TopNav.svelte` — Xero-style top navigation bar
  (organisation picker + primary sections with dropdowns + quick-add +
  user menu). Owns the "Add a new organisation" modal that POSTs to
  `/api/organisations`. All its popups close on outside click, `Escape`
  and on SPA navigation.
* `src/lib/components/NavDropdown.svelte` — dropdown menu used by `TopNav`.
  The top-level trigger is a real `<a href>` (click → navigate to the
  section overview, Xero-style); the menu opens on **hover** with a 150 ms
  close delay and also closes on click-outside, `Escape` and on SPA
  navigation. Each section supplies an `isActive(url)` matcher so that
  Sales vs Purchases can coexist on `/app/invoices?type=ACCPAY`-style URLs
  and the highlighted pill always matches the section currently in view.
* `src/lib/components/ComingSoon.svelte` — full-card "Under development"
  placeholder; use it for entirely stubbed routes that are not yet wired to
  the API.
* `src/lib/components/ModuleHeader.svelte`, `ReportView.svelte` — shared UI
  primitives for module pages and reports (title + subtitle + actions slot,
  and a pre-styled report viewer respectively).
* `src/routes/login/`, `src/routes/register/` — public pages.
* `src/routes/app/**` — authenticated SPA shell with Xero-like sections:
  - `app/` — Home (Business Overview bank cards).
  - `app/sales/` — Sales overview; `invoices/` and `items/` are kept at the
    top level for backwards-compat and linked from the Sales menu.
  - `app/purchases/` — Purchases overview; `bills/` and `bills/new/`
    redirect to `/app/invoices?type=ACCPAY`.
  - `app/accounting/` — Accounting hub with `bank-accounts/`, `bank-rules/`,
    `fixed-assets/`, `manual-journals/` and `settings/` child routes.
  - `app/bank-feeds/` — Open-Banking connection manager (see backend
    `/api/v1/bank-feeds/*`).
  - `app/bank-transactions/` — list of `BankTransaction`s with an optional
    `?accountId=` filter, linked from the Home bank-account tiles.
  - `app/settings/` — Xero-style global Settings hub (General, Sales,
    Purchases, Accounting, Tax, Contacts, Projects). Accessible from the
    avatar menu. Rows that don't yet have a backing API carry an "Under
    development" tag. Not matched by any top-level section's `isActive`,
    so visiting `/app/settings*` does not falsely highlight a main pill.
  - `app/reports/` — Reports hub; individual report viewers are
    `balance-sheet/`, `profit-and-loss/`, `sales-tax/`, etc. and all consume
    `ReportView.svelte`, which calls `/api/v1/reports/:slug` directly via
    `fetch` (no dedicated `reportApi` client).
  - `app/tax/`, `app/contacts/` — thin hubs matching the Xero menu layout.

## Conventions

* Use Svelte 5 runes (`$state`, `$derived`, `$effect`). No `export let` unless a
  legacy component demands it.
* **Guard dynamic params.** `$page.params.id` is typed `string | undefined`;
  always narrow before hitting the API (see
  `routes/app/invoices/[id]/+page.svelte`).
* **Accessibility.** Every `<label>` inside a form must carry `for=` and its
  paired control must have a matching `id=`. `bun run check` fails on
  regressions (`a11y_label_has_associated_control`).
* **Styling.** Tailwind utility classes + semantic helpers declared in
  `src/app.css` (`.btn-primary`, `.input`, `.card`, `.badge-*`,
  `.topbar-nav-item*`, `.nav-dropdown*`). Prefer these over ad-hoc class soup.
  The brand palette is a Xero-style navy/blue (`brand-500` = `#2c6cb0`).
* **Type imports.** Use `import type { … }` for DTOs to keep the runtime bundle
  small.

## Verification

```bash
cd web
bun run check   # svelte-check — 0 errors, 0 warnings
bun run build   # production build (optional for UI-only tweaks)
```
