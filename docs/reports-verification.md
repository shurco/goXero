# Reports verification — goXero's report suite against the Xero reference

**Verifier:** independent worker (`term_c5605554-325f-41d8-b2de-93a4541610c2`, task `task_9c23b0ca3811`,
dispatch `ctx_5dee8c1d2903`). Nothing in this repository was built by this worker; every claim below
was re-measured against the running app in `/Users/shurco/orca/workspaces/goXero/bonefish`
(API `:8080`, web `:5173`) and the shared database `bonefish-postgres-1`, over organisation
`6823b27b-c48f-4099-bb27-4202a4f496a2`.

**Period under test:** FY 2026 — 1 January 2026 to 31 December 2026; as-at reports as at
31 December 2026. Every report was fetched with the period passed explicitly, never by accepting a
default (§1.1 lists the exact query string per report).

**Nothing was adjusted on either side to make a figure agree.** Where a figure differs it is
reported as measured. No product code was changed; no migration was run; no database was reset;
`docs/xero-reference/**` was not edited.

**Every figure in this document was re-measured at 18:20-18:30 and again at 18:35-18:50 on 11 Sep
2026**, against the same running server — the shared dev database drifts, so nothing is quoted from
memory. Three things did not survive the re-measurement unchanged, and all three are in this document
as findings rather than silently corrected numbers:

* the hub's catalogue counts (§3), which this worker had recorded wrong;
* the General Ledger Detail's per-line `Balance` column (§1.9), which is genuinely unstable
  call-to-call;
* the honesty check's method and counts (§4). This document earlier claimed 14 pages and 661 tokens,
  and on re-measurement that figure could not be reproduced from any script this worker still holds —
  so the check was rebuilt from scratch to compare each page against **the URL that page reports
  having fetched**, read from the browser's own resource timings. §4 now reports 17 pages and 1,691
  money tokens, and records something the earlier pass could not have seen: `/app/reports/sales-tax`
  fetches `/api/v1/reports/bas`, not `/api/v1/reports/sales-tax`.

Any reader can reproduce every number here with the commands in §0. Two earlier honesty results were
thrown away rather than reported — one compared each page against a payload fetched for the FY period
while the page rendered its own default period, the other used a matcher that could not read the API's
`"91.75%"` strings. §4 names both, with the counts they produced, because a clean-looking zero that
came from a broken check is worse than no zero at all.

---

## Verdict

| Outcome | Reports |
|---|---|
| **Reproduces Xero to the cent** | **2** — Aged Receivables · Aged Payables |
| **Reproduces Xero except one difference of exactly 250.00, traced to a single named journal** | **4** — Trial Balance · Profit and Loss · Balance Sheet · Bank Summary (090 itself is exact) |
| **Differs from Xero beyond the 250.00 — structurally or in value** | **3** — General Ledger Detail · Account Transactions · Journal Report |
| **Differs from Xero fundamentally — a different measure, not a layout** | **1** — Cash Summary |
| **No Xero capture to compare; internally consistent, and every figure derivable from the reference agrees** | **6** — Sales Tax / BAS · Executive Summary · Budget Summary · Form 1120 · Aged Receivables by Contact · Aged Payables by Contact |
| **Not a report (deliberate and classified — §5.4)** | **1** — `/reports/invoice-summary` |
| **Front-end stubs that honestly say they are not wired to the API** | **4** — business-snapshot · health · visualisations · short-term-cash-flow |

Read the third row literally: those three reproduce every *total* Xero's file shows except the ones
named in §1.16, but their **line-level presentation is not deterministic** — General Ledger Detail's
per-line balance changes between calls of the same URL (§1.9). That is recorded as finding 5 below.

That accounts for all **21** route entries the router registers under `/api/v1/reports`: the **17**
advertised paths (BAS and Sales Tax are two paths for one body), the **2** router-only aliases
(`profit-loss`, `general-ledger` — §5.3), the **1** deliberate non-report (`invoice-summary`), and the
index itself. The Xero-compatible path `/api.xro/2.0/Reports/Form1120` is registered outside
`/api/v1` and is byte-identical to `form-1120` (§5.5). The **4** front-end-only stubs are not in the
API at all, and `/app/reports/bas` is the reverse — a path the API serves with no front-end route for
it.

### Headline numbers

* **Xero's Trial Balance total is 42,595.46 / 42,595.46.** goXero reproduces **24 of Xero's 25
  accounts to the cent**. The 25th, `400 Advertising`, is **9,907.05 against Xero's 9,657.05 — exactly
  +250.00**, and goXero carries **one account Xero's Trial Balance does not**: `091 Business Savings
  Account` at **−250.00**. Those are the two legs of one journal: the 250.00 SPEND on 2026-03-05 that
  §7(a) describes (journal 52, `savings demo`, "Office Chair", Dr 400 / Cr 091). On a net-per-account
  basis goXero's Trial Balance would read 42,845.46 / 42,845.46 — Xero's 42,595.46 plus that 250.00.
* **The rendered Trial Balance prints no 42,595.46 at any point.** Its Total row reads
  `108392.54 / 108392.54 / 44148.91 / 44148.91`; the first pair is the **gross movement** total and
  the second the YTD total. Both are internally exact (§8(b)) but **neither is the same measure as
  Xero's Trial Balance total**, which is net-per-account. No cell anywhere in the report shows the
  net-per-account figure a Xero reader would compare against.
* **The General Ledger Detail grand total is 111,263.08 / 111,263.08 in Xero and 108,392.54 /
  108,392.54 in goXero — a gap of 2,870.54 on each side.** That is **3,120.54** of gross ledger lines
  the import does not carry (§1.16 item-by-item) **less the 250.00** the contamination adds.
  **Every one of the 3,120.54 is a Dr/Cr wash pair inside a single account**, so **not one of them can
  move any account balance any report prints**. The 111,263.08-vs-42,595.46 question in
  `reconciliation.md` line 147 is not a gap at all: the two numbers are on different bases (gross
  against net-per-account) — §1.16.
* **`reconciliation.md` §4, §5 and §6 contain four claims the reference files do not support** —
  §1.16 and §1.10 record them, and the reference was left untouched.
* **No report has a blank table, an error, a spinner that never resolves, or a "Not available" badge
  on a report the API serves.** All **26** navigations that correspond to a real page — the hub plus
  every one of the **25** directories under `web/src/routes/app/reports` — render (23 at the first
  pass, plus the two by-contact pages created at 18:22:34); 4 stubs state honestly that they are not
  wired to the API. **One extra path the API advertises, `/app/reports/bas`, is a 404** and is
  recorded as finding 6 (§3).
* **Every one of the 1,691 money tokens rendered across the 17 API-backed report pages was checked
  against the very request that page made — 0 are unbacked** (§4). A negative control confirms the
  matcher would have flagged a fabricated figure.
* **One figure in the whole check is not reproducible call-to-call**: the General Ledger Detail's
  per-line `Balance` column, which changed between two captures of the same URL (§1.9). Every
  *total*, balance, count and percentage in every report is reproduced exactly on re-fetch; the
  report-body hashes of the three `journalFeed` reports are the only unstable identifier (§5.7).
* **Integrity re-derived from the database: debits = credits = 108,392.54 over 431 lines in 157
  journals; the Trial Balance Total row equals both the sum of its own rendered rows and the ledger
  aggregate; assets 21,073.01 = liabilities 13,056.28 + equity 8,016.73; each bank account closes at
  opening + received − spent** (§6).

¹ These two are in the API index and answer with their own ReportIDs. At the start of this check they
had **no front-end route**; the two pages were created at 18:22:34, mid-verification, and both routes
now render. End state recorded in §3.
² Budget Summary correctly renders Xero's layout with no rows and no Total row over zero accounts, and
says in its own ReportTitles that no budget is stored.
³ Form 1120 has no Xero capture in `docs/xero-reference/`; it was checked for envelope, arithmetic and
internal consistency only.

### The six findings that are not arithmetic

1. **Cash Summary is a different report from Xero's** (§1.6, §8(f)). goXero sections by the *immediate*
   counterpart account (`internal/repository/report.go:721-733`); Xero's Cash Summary looks through
   AR/AP to the invoice/bill coding. The Income section is empty (0.00 against Xero's 21,054.73 Sales),
   only 2 of Xero's 16 expense lines land in the same section with the same value, Historical Adjustment
   moves section *and* sign, and Xero's Fixed Assets line has no counterpart. The report's own caveat
   disclaims only the comparative columns and Tax Movements — not this.
2. **Sales Tax / BAS states a false premise** (§1.11). The trailing row says *"no posted journal line
   carries a tax amount"*; `gl_journal_lines` carries **33 lines with a non-zero `tax_amount`
   totalling 74.16** and **122 lines with a `tax_type`**, and `invoice_line_items` carries **85 lines
   with a non-zero `tax_amount` totalling 4,777.98**. The rendered net columns are correct; the
   sentence explaining why the tax columns are empty is not. **And the tree has since been corrected
   while the server has not:** at 18:22:34 `internal/handlers/report_render.go:1003-1006` was
   rewritten to the accurate *"…are left empty: no tax rate carries a tax amount on every one of its
   posted lines … Some lines do record a tax amount; they are not summed here because the rest of
   their rate's lines do not."*, but the API on :8080 (binary built 17:49:01) **still returns the old
   false sentence** — see the drift note in §1.11. The finding stands against the running app, and the
   app is running code that no longer exists in the tree.
3. **Two API-served, index-advertised reports had no front-end page — fixed during this check**
   (§3). For the whole of the first pass `/reports/aged-receivables-by-contact` and
   `/reports/aged-payables-by-contact` returned the SvelteKit 404 while
   `/api/v1/reports/aged-receivables-by-contact` served a full report. **At 18:22:34, mid-check,
   another worker added both `+page.svelte` files and both routes now render.** It is recorded as a
   finding because it was true for the entire first pass, and because a fix landing mid-verification
   is exactly the kind of drift a verification report has to name rather than paper over.
4. **Two byte-identical aliases are registered, unadvertised and undeclared** (§5.3):
   `/reports/profit-loss` and `/reports/general-ledger` return bodies identical to their canonicals and
   say nowhere in their own output that they are aliases. The dispatch's rule holds them to a
   self-declaration they do not make. The parity test knows both and the index is checked against the
   router in both directions, so this is a documentation gap in the *report*, not a hole in the test.

5. **The General Ledger Detail's `Balance` column is not deterministic** (§1.9, §5.7). Two calls of
   `GET /api/v1/reports/general-ledger-detail?fromDate=2026-01-01&toDate=2026-12-31`, 18 minutes
   apart on the same running server, printed **different balances for the same line**: the
   `Development work - per hour rate` row of journal 1595 showed **−28,966.43** in one capture and
   **−28,555.76** in the other, from the same credits (434.18 and 410.67) in the same account on the
   same date. Root cause: `ORDER BY a.code, j.journal_date, j.journal_id`
   (`internal/repository/report.go:874`) has **no tiebreaker on `l.line_id`**, and the balance is
   accumulated in the order returned (`:991`). **No total moves** — closing balances, account
   totals and the 108,392.54 grand total are SQL aggregates and are identical in both captures. It
   is the per-line attribution, and the row order, that is unstable. The same query backs Account
   Transactions and Journal Report, where it permutes rows without changing any figure (they print no
   running balance).

6. **A path the index advertises has no browser route** (§3 finding 2; §4). `GET
   /api/v1/reports/bas` serves a full report and the index lists `/reports/bas`, but
   `/app/reports/bas` is a SvelteKit 404 — no such directory exists. Nothing on the site links it;
   the page named *"Sales Tax Report"* fetches that endpoint from `/app/reports/sales-tax`. So a
   client that takes the index's `Path` field literally and opens it in a browser gets a 404 for a
   report the server is happy to return. Described, not fixed.

### The derived-figure question (migration 00023)

**Exactly one owner today, and no tuned number survives.** Migration 00023 forced 090 to 7,430.22 with
a derived journal posting ±8,654.01 against 840. Migration 00024 **deletes that journal** (Up, lines
1270-1272) and replaces it with journal `2a56a21a-8a5a-5ae5-b69b-ab29a94f8217`, number 1603,
2026-06-21, source MANUALJOURNAL, reference "Conversion Balance", **090 +4,130.98 / 840 −4,130.98**.
There is **no second owner**: a scan of every `gl_journal_lines` row returns **0 rows anywhere in the
database with a net amount of ±8,654.01**, and **0 rows on 090 or 840 dated on or before 2025-12-31**.
00023's derived journal is recreated only in 00024's *Down* (journal at 2080-2084, its lines at 2086-2092). 090's 7,430.22 is
now the sum of 26,923.45 received − 19,493.23 spent, both of which come from the captured bank
transactions, not from a tuned opening figure. §2.

---

## 0. How every figure below was produced

### 0.1 Token

```sh
curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@demo.local","password":"admin123"}' \
| python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' > /tmp/verif.token
```

`admin@demo.local` / `admin123` is the demo login documented in `README.md` lines 84-85. The response
field holding the access token is `token` (not `accessToken`). The token is short-lived: it was issued
again at 17:40 and had expired by 18:41, at which point every call began answering
`{"ErrorNumber":401,…"invalid or expired token"}`. **Re-run the login above before trusting any fetch
in this document** — an expired token returns a 401 body that parses as JSON and would silently look
like an empty payload to a careless script. One check in §4 was re-run for exactly this reason.

### 0.2 Wrapper

```sh
cat > /tmp/gx.sh <<'EOS'
#!/bin/zsh
TOK=$(cat /tmp/verif.token)
ORG=6823b27b-c48f-4099-bb27-4202a4f496a2
curl -s -H "Authorization: Bearer $TOK" -H "Xero-Tenant-Id: $ORG" "http://localhost:8080$1"
EOS
chmod +x /tmp/gx.sh
```

### 0.3 Fetching every report with the period explicit

```sh
/tmp/gx.sh "/api/v1/reports/trial-balance?date=2026-12-31&fromDate=2026-01-01"
/tmp/gx.sh "/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/balance-sheet?date=2026-12-31"
/tmp/gx.sh "/api/v1/reports/bank-summary?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/aged-receivables?date=2026-12-31"
/tmp/gx.sh "/api/v1/reports/aged-payables?date=2026-12-31"
/tmp/gx.sh "/api/v1/reports/aged-receivables-by-contact?date=2026-12-31"
/tmp/gx.sh "/api/v1/reports/aged-payables-by-contact?date=2026-12-31"
/tmp/gx.sh "/api/v1/reports/account-transactions?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/general-ledger-detail?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/journal-report?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/budget-summary?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/bas?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/executive-summary?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports/form-1120?fromDate=2026-01-01&toDate=2026-12-31"
/tmp/gx.sh "/api/v1/reports"
/tmp/gx.sh "/api/v1/reports/invoice-summary"
/tmp/gx.sh "/api/v1/organisation"
```

### 0.4 Database

```sh
docker exec bonefish-postgres-1 psql -U goxero -d goxero -c '<sql>'
```

### 0.5 Browser

```sh
orca tab create --url "http://localhost:5173/app/reports"    # -> browserPageId d2fa6f46-…
orca goto  --page d2fa6f46-788c-440b-8d08-a24fa828b513 --url http://localhost:5173/app/reports/<slug>
orca eval  --page d2fa6f46-788c-440b-8d08-a24fa828b513 --expression '(()=>({url:location.href,text:document.body.innerText,main:document.querySelector("main").innerText}))()'
```

Every `orca` command in this report carries `--page d2fa6f46-788c-440b-8d08-a24fa828b513`, a tab this
worker created. Before every read the tab list was re-read and the page's URL compared with the URL
requested; the two disagree only for `/app/reports/cash-flow` and `/app/reports/dashboards`, which
redirect by design (§3).

### 0.6 The honesty check (§4) — asking each page what it fetched

```sh
orca eval --page $PAGE --expression '(()=>({url:location.href,
  hrefs:performance.getEntriesByType("resource").map(e=>e.name).filter(u=>u.indexOf("/api/v1/reports")>=0),
  text:document.querySelector("main")?document.querySelector("main").innerText:""}))()'
```

The script that drives all 16 pages, captures the rendered `main`, curls the **exact URL each page
reports having fetched**, and diffs the money tokens is `/tmp/verify/honest3.py` (capture) +
`/tmp/verify/honest5.py` (comparison). The token regex is
`(?<![\w.,])(\(?-?\d{1,3}(?:,\d{3})*\.\d{2}\)?%?|-?\d+\.\d{2}%?)(?![\w.,])`; normalisation strips
thousands separators, maps `(1,234.56)` to `-1234.56`, and drops a trailing `%` before comparing the
absolute value against every string anywhere in the payload.

### 0.7 The route sweep (§3)

The route list is taken from the filesystem, not typed, so a page that exists cannot be missed:

```sh
ls -d web/src/routes/app/reports/*/     # 25 directories
```

`/tmp/verify/sweep2.py` and `/tmp/verify/sweep3.py` then, for each route, run `orca goto`, poll
`orca eval` in a wait-until-landed loop, and **re-read `orca tab list --json` and compare the landed
URL against the requested one before accepting the read** — the guard that caught the one bad capture
described above. 26 navigations for the hub plus 25 directories, plus a 27th for `/app/reports/bas`
(the API advertises it; no directory exists). Raw output: `/tmp/verify/sweep3.log`,
`/tmp/verify/sweep3.json`.

---

## 1. THE DATASET CHECK

### 1.1 The query string used for each report, and the parameters each endpoint actually reads

| Report | Query string used | Parameters the handler reads (`internal/handlers/report.go`) | Defaults if omitted |
|---|---|---|---|
| Trial Balance | `?date=2026-12-31&fromDate=2026-01-01` | **`date`** (the as-at date **and** the period end), **`fromDate`** | `date`=today; `fromDate`=first day of the month containing `date` |
| Profit and Loss | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate`, `compareFromDate`, `compareToDate` | `toDate`=today; `fromDate`=1 January of that year |
| Balance Sheet | `?date=2026-12-31` | `date`, `compareDate` | `date`=today |
| Bank Summary | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate` | `toDate`=today; `fromDate`=first of that month |
| Cash Summary | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate` | `toDate`=today; `fromDate`=financial-year start |
| Aged Receivables / Payables | `?date=2026-12-31` | `date`, `contactID` \| `ContactID` | `date`=today |
| Aged … by Contact | `?date=2026-12-31` | `date`, `contactID` \| `ContactID` | `date`=today |
| Account Transactions | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate`, `accountID` \| `accountIDs` | `toDate`=today; `fromDate`=financial-year start |
| General Ledger Detail (+ `/general-ledger`) | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate` | `toDate`=today; `fromDate`=financial-year start |
| Journal Report | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate` | `toDate`=today; `fromDate`=first of that month |
| BAS / Sales Tax | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate` | `toDate`=today; `fromDate`=first of that month |
| Executive Summary | `?fromDate=2026-01-01&toDate=2026-12-31` | **`date`** (as-at **and** period end), **`fromDate`** | `date`=today; `fromDate`=first of that month |
| Budget Summary | `?fromDate=2026-01-01&toDate=2026-12-31` | **`date`** only | `date`=today |
| Form 1120 | `?fromDate=2026-01-01&toDate=2026-12-31` | `toDate`, `fromDate` | `toDate`=today; `fromDate`=financial-year start |
| `invoice-summary` | *(no parameters)* | none — not a report handler | — |

**Three endpoints silently ignore part of the query string**, which the report titles give away:

* **Trial Balance** reads `date`, never `toDate`. Passing `?toDate=2026-12-31` alone would leave the
  period end at today. This is the behaviour `internal/handlers/report.go:70-88` describes.
* **Executive Summary** reads `date` (`report.go:397`), never `toDate`. Fetched with
  `?fromDate=2026-01-01&toDate=2026-12-31`, its ReportTitles still read
  *"For the period ending 11 September 2026"* — `fromDate` was honoured, `toDate` was not.
* **Budget Summary** reads `date` only (`report.go:435`). Its ReportTitles read
  *"For the year to 11 September 2026"* — the whole year window in the query string was ignored.

Every table below is presented with Xero's figure as printed in `docs/xero-reference/`, goXero's
figure as served, and the difference. **Difference sign convention: goXero − Xero.**

### 1.2 Trial Balance — `docs/xero-reference/trial-balance.txt` vs `/api/v1/reports/trial-balance?date=2026-12-31&fromDate=2026-01-01`

Xero's capture prints, for each account, a single signed figure on the side the account balances:
for Revenue / Direct Costs / Expense that is the **net movement** for the year; for Bank / Current
Asset / Fixed Asset / Current Liability it is the **balance carried as at the report date**. goXero
prints four columns: gross Debit movement, gross Credit movement, YTD Debit, YTD Credit. The
apples-to-apples comparison is therefore Xero's figure against goXero's **net movement** (Dr − Cr)
for the P&L accounts and goXero's **YTD** column for the balance-sheet accounts.

| Account | Xero (net / balance) | goXero (net / YTD) | Difference | Verdict |
|---|---:|---:|---:|---|
| 200 Sales | 29,539.18 Cr | 29,539.18 Cr | 0.00 | match |
| 300 Purchases | 775.98 Dr | 775.98 Dr | 0.00 | match |
| **400 Advertising** | **9,657.05 Dr** | **9,907.05 Dr** | **+250.00** | **DIFFERS** |
| 404 Bank Fees | 30.00 Dr | 30.00 Dr | 0.00 | match |
| 408 Cleaning | 1,110.00 Dr | 1,110.00 Dr | 0.00 | match |
| 412 Consulting & Accounting | 87.00 Dr | 87.00 Dr | 0.00 | match |
| 420 Entertainment | 1,553.60 Dr | 1,553.60 Dr | 0.00 | match |
| 425 Freight & Courier | 105.50 Dr | 105.50 Dr | 0.00 | match |
| 429 General Expenses | 166.28 Dr | 166.28 Dr | 0.00 | match |
| 445 Light, Power, Heating | 335.82 Dr | 335.82 Dr | 0.00 | match |
| 449 Motor Vehicle Expenses | 654.36 Dr | 654.36 Dr | 0.00 | match |
| 453 Office Expenses | 862.48 Dr | 862.48 Dr | 0.00 | match |
| 461 Printing & Stationery | 94.41 Dr | 94.41 Dr | 0.00 | match |
| 469 Rent | 3,273.66 Dr | 3,273.66 Dr | 0.00 | match |
| 473 Repairs and Maintenance | 1,896.70 Dr | 1,896.70 Dr | 0.00 | match |
| 489 Telephone & Internet | 236.37 Dr | 236.37 Dr | 0.00 | match |
| 493 Travel - National | 433.24 Dr | 433.24 Dr | 0.00 | match |
| 090 Business Bank Account | 7,430.22 Dr | 7,430.22 Dr (YTD) | 0.00 | match |
| 610 Accounts Receivable | 9,194.51 Dr | 9,194.51 Dr (YTD) | 0.00 | match |
| 710 Office Equipment | 923.79 Dr | 923.79 Dr (YTD) | 0.00 | match |
| 720 Computer Equipment | 3,774.49 Dr | 3,774.49 Dr (YTD) | 0.00 | match |
| 800 Accounts Payable | 8,386.76 Cr | 8,386.76 Cr (YTD) | 0.00 | match |
| 801 Unpaid Expense Claims | 115.95 Cr | 115.95 Cr (YTD) | 0.00 | match |
| 820 Sales Tax | 422.59 Cr | 422.59 Cr (YTD) | 0.00 | match |
| 840 Historical Adjustment | 4,130.98 Cr | 4,130.98 Cr (YTD) | 0.00 | match |
| **091 Business Savings Account** | **not in Xero** | **250.00 Cr (YTD)** | **extra account** | **DIFFERS** |

**How much of Xero's Trial Balance the import reproduced: 24 of Xero's 25 accounts, to the cent.**
One account is 250.00 out (`400 Advertising`) and there is one extra account (`091`, −250.00). Both
are the legs of journal 52 (§7(a)) — the shared dev database's 2026-03-05 contamination, not the
import. On the net-per-account basis Xero uses, goXero reads **42,845.46 / 42,845.46** against
Xero's **42,595.46 / 42,595.46**: the difference is exactly the 250.00, on both sides, once.

#### The Total row, and why it prints no 42,595.46

Rendered Total row: `Total | 108392.54 | 108392.54 | 44148.91 | 44148.91`. Xero's Trial Balance
prints `Total 42,595.46 42,595.46 -`. `42595.46` appears **0 times** in the payload.

The three bases, each computed from the same ledger:

| Basis | goXero | Xero | Note |
|---|---:|---:|---|
| Gross movement (Σ Dr, Σ Cr across accounts) | 108,392.54 | 111,263.08 | Xero's figure is its **GL Detail** grand total over 27 accounts |
| Net-per-account (Σ of each account's net on its own side) | 42,845.46 | 42,595.46 | **Xero's Trial Balance total**; the 250.00 gap is the contamination |
| YTD column total (as rendered) | 44,148.91 | — | Xero prints no equivalent |

So the report's Total row is **not a wrong figure** — it equals the sum of the rows above it, and it
equals the ledger (§6) — but **it is not the measure Xero's Trial Balance total is**. A reader
comparing 108,392.54 with 42,595.46 is comparing two different quantities. The report's own
ReportTitles explains the Debit/Credit-vs-YTD column split but says nothing about the Total row being
gross. See §8(b).

### 1.3 Profit and Loss — `docs/xero-reference/profit-and-loss.txt` vs `/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-12-31`

| Line | Xero | goXero | Difference |
|---|---:|---:|---:|
| Sales | 29,539.18 | 29,539.18 | 0.00 |
| Total Trading Income | 29,539.18 | 29,539.18 | 0.00 |
| Purchases | 775.98 | 775.98 | 0.00 |
| Total Cost of Sales | 775.98 | 775.98 | 0.00 |
| Gross Profit | 28,763.20 | 28,763.20 | 0.00 |
| **Advertising** | **9,657.05** | **9,907.05** | **+250.00** |
| Bank Fees | 30.00 | 30.00 | 0.00 |
| Cleaning | 1,110.00 | 1,110.00 | 0.00 |
| Consulting & Accounting | 87.00 | 87.00 | 0.00 |
| Entertainment | 1,553.60 | 1,553.60 | 0.00 |
| Freight & Courier | 105.50 | 105.50 | 0.00 |
| General Expenses | 166.28 | 166.28 | 0.00 |
| Light, Power, Heating | 335.82 | 335.82 | 0.00 |
| Motor Vehicle Expenses | 654.36 | 654.36 | 0.00 |
| Office Expenses | 862.48 | 862.48 | 0.00 |
| Printing & Stationery | 94.41 | 94.41 | 0.00 |
| Rent | 3,273.66 | 3,273.66 | 0.00 |
| Repairs and Maintenance | 1,896.70 | 1,896.70 | 0.00 |
| Telephone & Internet | 236.37 | 236.37 | 0.00 |
| Travel - National | 433.24 | 433.24 | 0.00 |
| **Total Operating Expenses** | **20,496.47** | **20,746.47** | **+250.00** |
| **Net Profit** | **8,266.73** | **8,016.73** | **−250.00** |

Every line matches except Advertising, and the two summary lines carry that single 250.00. Note that
this report **nets** within an account — Freight & Courier prints 105.50 and Office Expenses 862.48,
exactly as Xero prints them, although the underlying account carries gross wash pairs (§1.9). The
P&L's 250.00 is the same contamination, not a second difference.

### 1.4 Balance Sheet — `docs/xero-reference/balance-sheet.txt` vs `/api/v1/reports/balance-sheet?date=2026-12-31`

| Line | Xero | goXero | Difference |
|---|---:|---:|---:|
| Business Bank Account | 7,430.22 | 7,430.22 | 0.00 |
| **Business Savings Account (091)** | **not in Xero** | **−250.00** | **extra** |
| Accounts Receivable | 9,194.51 | 9,194.51 | 0.00 |
| Office Equipment | 923.79 | 923.79 | 0.00 |
| Computer Equipment | 3,774.49 | 3,774.49 | 0.00 |
| Inventory (630) | — (nil in Xero, not a row) | 0.00 | 0.00 |
| **Total Assets** | **21,323.01** | **21,073.01** | **−250.00** |
| Accounts Payable | 8,386.76 | 8,386.76 | 0.00 |
| Historical Adjustment | 4,130.98 | 4,130.98 | 0.00 |
| Sales Tax | 422.59 | 422.59 | 0.00 |
| Unpaid Expense Claims | 115.95 | 115.95 | 0.00 |
| Tracking Transfers (877) | — (nil in Xero, not a row) | 0.00 | 0.00 |
| **Total Liabilities** | **13,056.28** | **13,056.28** | 0.00 |
| **Total Equity / Current Year Earnings** | **8,266.73** | **8,016.73** | **−250.00** |
| **Net Assets** | **8,266.73** | **8,016.73** | **−250.00** |

The identity holds on both sides: **21,073.01 = 13,056.28 + 8,016.73** (goXero) and
**21,323.01 = 13,056.28 + 8,266.73** (Xero). The single 250.00 flows through Assets and Equity and
cancels; liabilities are untouched. Confirmed independently from the database in §6.

### 1.5 Bank Summary — `docs/xero-reference/bank-summary.txt` vs `/api/v1/reports/bank-summary?fromDate=2026-01-01&toDate=2026-12-31`

| Account | Basis | Xero | goXero | Difference |
|---|---|---:|---:|---:|
| Business Bank Account (090) | Opening | − (nil) | 0.00 | same meaning |
| | Cash Received | 26,923.45 | 26,923.45 | 0.00 |
| | Cash Spent | 19,493.23 | 19,493.23 | 0.00 |
| | Closing Balance | 7,430.22 | 7,430.22 | 0.00 |
| **Business Savings Account (091)** | Opening / Received / Spent / Closing | **not in Xero** | 0.00 / 0.00 / 250.00 / **−250.00** | **extra row** |
| **Total** | Received / Spent / Closing | 26,923.45 / 19,493.23 / **7,430.22** | 26,923.45 / **19,743.23** / **7,180.22** | **+250.00 / −250.00** |

Account 090 reproduces Xero **exactly, all four columns**. The only difference is the extra 091 row
and the total it drags with it, again the contamination.

### 1.6 Cash Summary — `docs/xero-reference/cash-summary.txt` vs `/api/v1/reports/cash-summary?fromDate=2026-01-01&toDate=2026-12-31`

**This report does not reproduce Xero's sectioning for every line. It differs for most lines, not
just Historical Adjustment.** The task asked which section each of Xero's lines falls into; that is
the table, and it is mostly "a different section, a different amount, or absent".

| Xero section | Xero line | Xero amount | Where goXero puts it | goXero amount |
|---|---|---:|---|---:|
| Income | Sales | 21,054.73 | **nowhere — the account does not appear in the report at all** | — |
| Income | Total Income | 21,054.73 | Income (empty) | **0.00** |
| Less Expenses | Advertising | 5,500.00 | Less Expenses / Advertising (400) | **250.00** |
| Less Expenses | Bank Fees | 30.00 | Less Expenses / Bank Fees (404) | 30.00 ✔ same section, same value |
| Less Expenses | Cleaning | 1,110.00 | **nowhere** | — |
| Less Expenses | Consulting & Accounting | 58.00 | **nowhere** | — |
| Less Expenses | Entertainment | 1,553.60 | Less Expenses / Entertainment (420) | **53.60** |
| Less Expenses | General Expenses | 46.19 | Less Expenses / General Expenses (429) | 46.19 ✔ same section, same value |
| Less Expenses | **Historical Adjustment** | **(4,130.98)** | **Plus Other Cash Movements / Historical Adjustment (840)** | **+4,130.98** — different section **and** sign |
| Less Expenses | Light, Power, Heating | 235.50 | **nowhere** | — |
| Less Expenses | Motor Vehicle Expenses | 654.36 | Less Expenses / Motor Vehicle Expenses (449) | **274.36** |
| Less Expenses | Office Expenses | 700.37 | Less Expenses / Office Expenses (453) | **168.87** |
| Less Expenses | Printing & Stationery | 94.41 | Less Expenses / Printing & Stationery (461) | **67.16** |
| Less Expenses | Rent | 3,273.66 | **nowhere** | — |
| Less Expenses | Repairs and Maintenance | 1,745.61 | Less Expenses / Repairs and Maintenance (473) | **64.20** |
| Less Expenses | Telephone & Internet | 186.37 | Less Expenses / Telephone & Internet (489) | **101.62** |
| Less Expenses | Travel - National | 209.68 | Less Expenses / Travel - National (493) | **177.44** |
| Less Expenses | Total Expenses | 11,266.77 | Total Expenses | **1,233.44** |
| | Surplus (Deficit) | 9,787.96 | Surplus (Deficit) | **−1,233.44** |
| Plus Other Cash Movements | Fixed Assets | (2,728.29) | **nowhere** | — |
| Plus Other Cash Movements | — | — | Accounts Receivable (610) | 22,792.47 |
| Plus Other Cash Movements | — | — | Accounts Payable (800) | −18,371.23 |
| Plus Other Cash Movements | — | — | Unpaid Expense Claims (801) | −64.40 |
| Plus Other Cash Movements | — | — | Sales Tax (820) | −74.16 |
| Plus Other Cash Movements | Total | (2,728.29) | Total Other Cash Movements | **8,413.66** |
| Plus Tax Movements | Tax Collected | 1,844.56 | **blank**, declared absent in ReportTitles | — |
| Plus Tax Movements | Tax Paid | (1,474.01) | **blank** | — |
| Plus Tax Movements | Net Tax Movements | 370.55 | **blank** | — |
| | Net Cash Movement | 7,430.22 | Net Cash Movement | **7,180.22** |
| Summary | Opening Balance | − (nil) | 0.00 | same meaning |
| Summary | Plus Net Cash Movement | 7,430.22 | 7,180.22 | **−250.00** |
| Summary | Cash Balance | 7,430.22 | **7,180.22** | **−250.00** |

**Only two of Xero's 16 expense lines land in the same section with the same value** (Bank Fees,
General Expenses). One Xero line (Sales) has no goXero counterpart anywhere. Xero's Fixed Assets line
has none either, and goXero adds four lines Xero does not have.

**Root cause, from the code.** `internal/repository/report.go:721-733` classifies the counterpart side
of every journal that touched a bank account by the **immediate** counterpart account's `type`:

```sql
WHERE j.organisation_id = $1 AND a.type <> $4        -- $4 = BANK
  AND j.journal_date BETWEEN $2 AND $3
  AND EXISTS (SELECT 1 FROM gl_journal_lines bl JOIN accounts ba ON ba.account_id = bl.account_id
               WHERE bl.journal_id = j.journal_id AND ba.type = $4)
```

Money received from a customer lands on **610 Accounts Receivable**, so it is reported as an "Other
Cash Movement" instead of as Sales; money paid to a supplier lands on **800 Accounts Payable**. Xero's
Cash Summary looks *through* AR/AP to the invoice or bill coding and reports the cash against the
income or expense account, which is why its Income section is full and its Other Cash Movements hold
only Fixed Assets.

**The report's own caveat does not cover this.** Its ReportTitles disclaim only *"the Yearly average
(YTD), Variance and Variance for Variance columns"* and *"Tax Movements"*. Income, Less Expenses and
Plus Other Cash Movements are presented as complete. Described, not fixed.

### 1.7 Aged Receivables and Aged Payables

#### Aged Receivables Summary — `docs/xero-reference/aged-receivables-summary.txt` vs `/api/v1/reports/aged-receivables?date=2026-12-31`

| Contact | Xero Total | goXero Total | Buckets | Verdict |
|---|---:|---:|---|---|
| Basket Case | 914.55 | 914.55 | 3 Months | match |
| Bayside Club | 234.00 | 234.00 | 3 Months | match |
| City Limousines | 1,191.83 | 1,191.83 | 3 Months 703.63 / Older 488.20 | match |
| DIISR - Small Business Services | 270.63 | 270.63 | Older | match |
| Marine Systems | 396.00 | 396.00 | 3 Months | match |
| Ridgeway University | 6,187.50 | 6,187.50 | 3 Months | match |
| **Total** | **9,194.51** | **9,194.51** | 3 Months 8,435.68 / Older 758.83 | match |
| Percentage of total | 91.75% / 8.25% / 100.00% | 91.75% / 8.25% / 100.00% | | match |

**Every amount cell, every bucket and the whole Percentage-of-total row match.** The only visible
differences are presentational: Xero prints `-` where goXero prints `0.00`, and Xero's capture titles
the report "Aged Receivables Summary" while goXero's `ReportName` is "Aged Receivables".

#### Aged Payables Summary — `docs/xero-reference/aged-payables-summary.txt` vs `/api/v1/reports/aged-payables?date=2026-12-31`

| Contact | Xero | goXero | Buckets | Verdict |
|---|---:|---:|---|---|
| Bayside Club | 130.00 | 130.00 | 3 Months | match |
| Bayside Wholesale | 840.00 | 840.00 | 3 Months | match |
| Capital Cab Co | 242.00 | 242.00 | 3 Months | match |
| Central Copiers | 163.56 | 163.56 | Older | match |
| Net Connect | 54.13 | 54.13 | 3 Months | match |
| PC Complete | 2,132.51 | 2,132.51 | 2 Months | match |
| PowerDirect | 108.60 | 108.60 | 3 Months | match |
| SMART Agency | 4,500.00 | 4,500.00 | 3 Months 2,500.00 / Older 2,000.00 | match |
| Swanston Security | 59.54 | 59.54 | Older | match |
| Xero | 31.39 | 31.39 | 3 Months | match |
| Young Bros Transport | 125.03 | 125.03 | 3 Months | match |
| **Total Aged Payables** | **8,386.76** | **8,386.76** | 2 Months 2,132.51 / 3 Months 4,031.15 / Older 2,223.10 | match |
| Expense Claims claimant | 115.95 (**"Adam Michkevich"**) | 115.95 (**"Xero Demo"**) | 3 Months | **name differs, amount matches** |
| **Total Expense Claims** | **115.95** | **115.95** | | match |
| **Total** | **8,502.71** | **8,502.71** | 2 Months 2,132.51 / 3 Months 4,147.10 / Older 2,223.10 | match |
| Percentage of total | 25.08% / 48.77% / 26.15% / 100.00% | 25.08% / 48.77% / 26.15% / 100.00% | | match |

**Every amount cell, both blocks (Aged Payables, Expense Claims) and the 8,502.71 total match,
including the Percentage-of-total row.** The single differing cell is the claimant's **name**.

#### The Expense Claims claimant question — settled

The two Xero captures disagree with each other:

| Source | Claimant it prints |
|---|---|
| `docs/xero-reference/aged-payables-summary.txt` | **Adam Michkevich** |
| `docs/xero-reference/general-ledger-detail.txt` lines 494, 548 | **Xero Demo** |
| `migrations/data/xero/expense-claims.csv` line 2 (column `contact`) | **Xero Demo** |
| `docs/xero-reference/journal-report.txt` "Posted By" | **Adam Michkevich** on 668 rows, "System Generated" on 83 |

goXero prints **"Xero Demo"**. That is what its own data holds and what its renderer is specified to
print: `internal/repository/report.go:637` takes the label from
`COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.email, 'Unassigned')` — the
organisation's **users** table. The row is:

```
expense_claim_id 817c904c-7d9e-5843-b581-7e5188d821a6  status AUTHORISED  amount_due 115.95  reporting_date 2026-09-10
user: Xero Demo  xero.demo@demo.local
```

Xero's own General Ledger Detail capture labels the same claim "Xero Demo"; only the Aged Payables
capture says "Adam Michkevich" (who is the Xero org's posting user, and who also exists as a
**contact** in goXero's `contacts` table but is not the linked user). **Verdict: not an arithmetic
difference; the Xero reference disagrees with itself; goXero faithfully prints the name its own
database holds, and that name appears in one of the two Xero captures.** The reference was left
untouched.

#### The by-contact variants — `/api/v1/reports/aged-receivables-by-contact?date=2026-12-31`, `…/aged-payables-by-contact?date=2026-12-31`

These are **not** aliases of the summaries: each returns its own `ReportID`
(`AgedReceivablesByContact` / `AgedPayablesByContact`) and its own `ReportName`, and it blocks the
individual documents under a section per contact (`INV-0026 10 September 2026`, `SM0195 21 July 2026`,
…). Every per-contact subtotal equals the corresponding row of the summary, and the grand totals equal
the summaries' totals — Receivables **9,194.51**, Payables **8,386.76**. There is no Xero capture of
either variant in `docs/xero-reference/`, so only internal consistency could be checked, and it holds.
Both are advertised in the API index. **Neither had a front-end route for the whole of the first
pass** — `/app/reports/aged-receivables-by-contact` and `aged-payables-by-contact` returned the
SvelteKit 404 while the API served them — and **both routes were created at 18:22:34, mid-check**;
they now render (§3, finding 1).

### 1.8 Account Transactions — `docs/xero-reference/account-transactions.txt` vs `/api/v1/reports/account-transactions?fromDate=2026-01-01&toDate=2026-12-31`

Xero's capture is **per-account blocks**: each account opens with an `Opening Balance` row, carries a
`Running Balance` column, a `Gross` and a `Tax` column, and closes with a `Closing Balance` row.
goXero returns a **single flat Section** — header `Date | Source | Reference | Description | Account |
Debit | Credit`, **431 rows** — plus one Total row `108392.54 | 108392.54`. There is no per-account
block, no opening balance, no running balance, no gross column and no tax column.

| Account | Xero rows | Xero Debit | Xero Credit | goXero rows | goXero Debit | goXero Credit | Difference |
|---|---:|---:|---:|---:|---:|---:|---:|
| 800 Accounts Payable | 65 | 18,963.37 | 27,350.13 | 59 | 18,667.30 | 27,054.06 | **−6 rows / −296.07 / −296.07** |
| 610 Accounts Receivable | *(GL Detail figure)* | 34,195.38 | 25,000.87 | 53 | 33,091.18 | 23,896.67 | **−1,104.20 / −1,104.20** |
| every other account | — | — | — | — | — | — | ties to §1.9 to the cent |

The two **value** differences are the missing credit-note allocations of §1.16 items 3 and 4 — four
rows in AP (270.63 ×2, 25.44 ×2) and six in AR (541.25 ×2, 541.25 ×2, 21.70 ×2). Each is a wash pair
inside one account, so the **account balance is unaffected**; only gross columns and row counts move.
The **row-count** delta in AP is 6, of which 4 are the allocation rows above; the remaining 2 rows
are not accounted for by this decomposition and are stated as measured rather than explained. The
*value* delta of exactly −296.07 is fully explained, and it is the figure that can affect a report.

*Ordering note:* Account Transactions is built by `journalFeed` (§5.7), so its **row order** among
lines that share an account and date inside one journal is not stable between calls. No figure moves
— this report prints Debit and Credit per line and has no running-balance column — but two calls of
the same URL can return the same 431 rows in a different order.

*Caveat on the Xero side:* `account-transactions.txt` is a **truncated capture** — 109 lines, whose
Accounts Receivable section stops mid-list at row 30 with no total. The Xero figures quoted for 610
are the ones in `general-ledger-detail.txt`, which covers the same period in full; the truncated file
cannot be quoted for AR.

### 1.9 General Ledger Detail — `docs/xero-reference/general-ledger-detail.txt` vs `/api/v1/reports/general-ledger-detail?fromDate=2026-01-01&toDate=2026-12-31`

Xero's file has **27 `##<Account>` sections** and a grand `Total 111,263.08 111,263.08 - (422.59)`.
goXero returns **26 Section blocks**, each ending in a `Closing Balance` SummaryRow, and
**no grand Total row at all**. Per-account Dr/Cr:

| Account | Xero Dr | Xero Cr | goXero Dr | goXero Cr | Difference (each side) |
|---|---:|---:|---:|---:|---:|
| Accounts Payable (800) | 18,963.37 | 27,350.13 | 18,667.30 | 27,054.06 | **−296.07** |
| Accounts Receivable (610) | 34,195.38 | 25,000.87 | 33,091.18 | 23,896.67 | **−1,104.20** |
| Advertising (400) | 9,657.05 | — | 9,907.05 | — | **+250.00** |
| Freight & Courier (425) | 115.50 | 10.00 | 115.50 | 10.00 | 0.00 |
| Office Expenses (453) | 1,135.98 | 273.50 | 1,135.98 | 273.50 | 0.00 |
| Sales (200) | 1,019.95 | 30,559.13 | 1,019.95 | 30,559.13 | 0.00 |
| Sales Tax (820) | 2,122.03 | 2,544.62 | 2,122.03 | 2,544.62 | 0.00 |
| Unpaid Expense Claims (801) | 64.40 | 180.35 | 64.40 | 180.35 | 0.00 |
| Business Bank Account (090) | 26,923.45 | 19,493.23 | 26,923.45 | 19,493.23 | 0.00 |
| **Inventory (630)** | **320.00** | **320.00** | **no section** | **no section** | **−320.00 each side** |
| **Tracking Transfers (877)** | **1,400.27** | **1,400.27** | **no section** | **no section** | **−1,400.27 each side** |
| **Business Savings Account (091)** | **no section** | **no section** | **0.00** | **250.00** | **extra section** |
| the remaining 16 sections (404, 408, 412, 420, 429, 445, 449, 461, 469, 473, 489, 493, 710, 720, 840, and Purchases 300) | Xero's own figures | | identical | | 0.00 on both sides — exact |

goXero's report carries **26 account sections**: the 25 accounts it shares with Xero's 27 (Xero's
Inventory and Tracking Transfers have **no section** here because they carry no lines in goXero) plus
its extra 091. **22 of those 25 shared accounts match Xero's Dr and Cr to the cent, both columns.**

*Differing (4 values, 3 accounts + 1 extra):* Accounts Payable −296.07 on each side, Accounts
Receivable −1,104.20 on each side, Advertising +250.00 on the debit side, and the extra 091 at
0.00 Dr / 250.00 Cr. **The three account differences are exactly the four items decomposed in §1.16
plus the contamination pair.** Note that 425, 453 and 200 match on
**gross** here even though Xero's Trial Balance nets them — because Xero's GL Detail prints gross and
so does goXero's; the two reports are on the same basis and agree.

#### The `Balance` column is not stable — two calls of one URL print different figures

This is the one place in the whole check where **a figure a report prints changes between two calls
of the identical URL.**

**Mechanism.** Every line-level report is built by `journalFeed`
(`internal/repository/report.go:863-876`), whose ordering clause is
`ORDER BY a.code, j.journal_date, j.journal_id` (**:874**). That key does **not** uniquely identify a
row: two lines of the *same journal* that post to the *same account* on the *same date* tie, and
there is **no tiebreaker on `l.line_id`** — the physical heap order decides. The General Ledger
Detail's `Balance` column is then accumulated **in that order** by a second pass
(`internal/repository/report.go:987-993`): `running = running.Add(line.Debit).Sub(line.Credit)`.

**The ties that exist in this organisation's ledger** (`gl_journal_lines`, org
`6823b27b-…f496a2`) — the query reproduces with:

```sql
SELECT j.journal_number, a.code, j.journal_date, count(*) n,
       string_agg(l.net_amount::text,' , ' ORDER BY l.net_amount) amounts
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id
  JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'
 GROUP BY 1,2,3 HAVING count(*)>1 ORDER BY 3;
```

| Journal | Account | Date | Lines tied | Amounts | Can the printed `Balance` change? |
|---|---|---|---|---:|---|
| 1570 | 493 Travel - National | 2026-07-11 | 2 | 15.61 , 16.63 | **yes** |
| 1570 | 820 Sales Tax | 2026-07-11 | 2 | 1.29 , 1.37 | **yes** |
| 1579 | 200 Sales | 2026-07-14 | 2 | −500.00 , −47.80 | **yes** |
| 1586 | 200 Sales | 2026-08-01 | 2 | −525.00 , −250.00 | **yes** |
| 1592 | 200 Sales | 2026-08-11 | 2 | −200.00 , −19.95 | **yes** |
| 1595 | 200 Sales | 2026-09-10 | 2 | −434.18 , −410.67 | **yes** |
| 1580 | 200 Sales | 2026-07-13 | 3 | −400.00 , −400.00 , −400.00 | no — the amounts are equal |
| 1585 | 200 Sales | 2026-07-30 | 2 | −500.00 , −500.00 | no — the amounts are equal |

**8 tie groups, 17 lines; 12 of those lines sit in the 6 groups whose amounts differ**, and for those
12 the printed `Balance` depends on which order the database happens to return.

**The difference, observed.** Two captures of
`GET /api/v1/reports/general-ledger-detail?fromDate=2026-01-01&toDate=2026-12-31`, taken 18 minutes
apart on the same running server with the same token, differ in exactly **6 rows**, in three swapped
pairs — `INV-0008` (400.00 / 400.00, equal amounts), `INV-0026` (410.67 / 434.18) and account 493
(15.61 / 16.63). The last two are the ones that move a printed figure:

| Capture | Row | Debit | Credit | `Balance` printed |
|---|---|---:|---:|---:|
| A — `/tmp/verify/raw/general-ledger-detail.json`, 17:40:37 | Sales (200) 2026-09-10 INV-0026 "Project team meeting…" | 0.00 | 410.67 | **−28,532.25** |
| A | Sales (200) 2026-09-10 INV-0026 "Development work - per hour rate" | 0.00 | 434.18 | **−28,966.43** |
| B — `/tmp/verify/final/gl-detail.json`, 17:59:11 | Sales (200) 2026-09-10 INV-0026 "Development work - per hour rate" | 0.00 | 434.18 | **−28,555.76** |
| B | Sales (200) 2026-09-10 INV-0026 "Project team meeting…" | 0.00 | 410.67 | **−28,966.43** |

So the row `Development work - per hour rate` printed **−28,966.43** in capture A and **−28,555.76**
in capture B. **The arithmetic, both ways** (the balance carried into journal 1595 on account 200 is
−28,121.58, implied by either capture and identical in both):

* order 434.18 first: −28,121.58 − 434.18 = **−28,555.76**, then −28,555.76 − 410.67 = **−28,966.43** (capture B)
* order 410.67 first: −28,121.58 − 410.67 = **−28,532.25**, then −28,532.25 − 434.18 = **−28,966.43** (capture A)

and the Account 493 pair (opening 0.00, debits 15.61 and 16.63):

* 15.61 first: 0.00 + 15.61 = **15.61**, then + 16.63 = **32.24** (capture A)
* 16.63 first: 0.00 + 16.63 = **16.63**, then + 15.61 = **32.24** (capture B)

**What this does and does not move.** The **closing balance is identical either way** (−28,966.43 and
32.24), and so are the account's Debit, Credit and Closing figures and the report's **108,392.54**
grand total — those are aggregates computed in SQL (`internal/repository/report.go:936-947`), not
accumulated in this order. What moves is **the balance attributed to an individual line**, and the
row order itself. Not one *total* anywhere is affected, so nothing in §1.2-1.7, §1.14, §1.16 or §6
changes.

**Two consequences for this report.** (1) The body hash of
`/api/v1/reports/general-ledger-detail` is not a stable identifier of the report — see §5.7; the
hashes in §5.1 for the three `journalFeed` reports are **observations at a timestamp**, both values
recorded. (2) The same is true of Account Transactions (§1.8) and Journal Report (§1.10) for row
order only, since neither prints a running balance.

**Root cause, for the fix — not applied here:** `internal/repository/report.go:874`. Adding
`l.line_id` (or `l.gross_amount`, or any unique column) to the ordering key makes the report
deterministic. Recorded, not fixed: this is a different task.

#### The grand-total question (`reconciliation.md` line 147)

`reconciliation.md` line 147 says the General Ledger's grand total is **111,263.08** while the Trial
Balance total is **42,595.46**, and §5 attributes the whole gap to accounts 630 and 877. Measured
directly from the database and from the files:

| Quantity | Value | Source |
|---|---:|---|
| Xero GL Detail grand total (Dr / Cr) | 111,263.08 / 111,263.08 | `general-ledger-detail.txt`, last line |
| Xero Trial Balance total | 42,595.46 / 42,595.46 | `trial-balance.txt`, `Total` row |
| **goXero** GL Detail grand total | **108,392.54 / 108,392.54** | §1.14, coordinator's figure **reproduced** |
| goXero positive sum of `net_amount` | 108,392.54 | DB |
| goXero negative sum of `net_amount` | −108,392.54 | DB |
| goXero `gl_journal_lines` line count | **431** | DB |
| goXero `gl_journals` journal count | **157** | DB |
| contamination included in the above | **250.00** | §7(a), journal 52 |
| **goXero's own total** | **108,142.54** | 108,392.54 − 250.00 |
| **gap to Xero's grand total, each side** | **3,120.54** | 111,263.08 − 108,142.54 |

`reconciliation.md` §2 claims, for the journal-report spine, **1,280 journal lines** and a sum of
debits = credits = **42,595.46**. `journal-report.txt` contains **751 data rows** and its own grand
`Total` row reads **194,720.04 / 194,720.04**; the live database holds **431** lines. Neither of the
§2 numbers is what either artifact shows. The reference was left untouched; this is recorded as a
discrepancy.

### 1.10 Journal Report — `docs/xero-reference/journal-report.txt` vs `/api/v1/reports/journal-report?fromDate=2026-01-01&toDate=2026-12-31`

| | Xero's capture | goXero |
|---|---|---|
| Structure | 264 `ID <n>` journal blocks (IDs 388–651), each with its own `Total` row | **one flat 431-row table**, no journal blocks, no per-journal Total |
| Header | `Date / Journal ID / Account Code / Account / Debit / Credit / Posted Date / Posted By` | `Date / Source / Reference / Account / Debit / Credit` |
| Data rows | **751** | **431** |
| Grand total | **194,720.04 / 194,720.04** | **no grand Total row** |
| Journals represented | 264 | 157 |
| Posted-by / source | "Adam Michkevich" 668 rows, "System Generated" 83 | a `Source` column (`BANKTRANSACTION`, `INVOICE`, …) |

*Ordering note:* built by `journalFeed`, so the **order** of the 431 rows is not stable between calls
(§1.9, §5.7). No figure is at risk — the report prints Date, Source, Reference, Account, Debit and
Credit with no running balance — but two calls of the same URL can permute the lines of one journal.

Per-account **nets** agree (modulo the 400 +250.00 and the extra 091 of §7(a)) — the level at which
the report is arithmetically right. What is missing is the grouping: a reader cannot see a journal,
only its lines, and cannot see a journal-level total, only account-level ones. That is a structural
difference, not a wrong figure.

### 1.11 Sales Tax / BAS — `/api/v1/reports/bas` and `/api/v1/reports/sales-tax` vs `gl_journal_lines`

There is **no Xero capture of Sales Tax or BAS** in `docs/xero-reference/`, so the two rendered net
figures were re-derived from the database instead.

| Rendered line | goXero | Re-derived from the DB | vs Xero's own equivalent | Verdict |
|---|---:|---|---|---|
| Net Sales | 29,539.18 | account 200 by tax type: `OUTPUT` −29,375.43 over **35 lines** + `OUTPUT2` −163.75 over **5 lines** = **29,539.18** | Xero's Sales is 29,539.18 | **exact match** |
| Net Purchases | 21,522.45 | `gl_journal_lines` by `tax_type`: `INPUT` **on the trading accounts only** (68 Dr lines 19,472.35 − 2 Cr lines 273.50) = 19,198.85 over **70 lines**; `NONE` 1,833.60 − 10.00 = 1,823.60 over **8 lines**; `OUTPUT2` 500.00, the single `OUTPUT2` line on account 453, over **1 line**; total **21,522.45** | Xero's Cost of Sales 775.98 + Operating Expenses 20,496.47 = **21,272.45** | **+250.00** — the §7(a) contamination, which sits in Advertising |

*Scoping note on the `INPUT` subtotal* (so the numbers above reproduce exactly): the whole `INPUT`
population is **73 lines netting 23,897.13** — 71 debit lines totalling 24,170.63 and 2 credit lines
totalling 273.50. The **3 debit lines totalling 4,698.28 on the capital accounts (710 Office Equipment
923.79, 720 Computer Equipment 1,804.50 + 1,969.99)** are excluded, because Xero's purchases boxes put
capital purchases in a box of their own, so excluding them is what makes the line comparable with
21,272.45. Including them would give 23,897.13 here and a Net Purchases of 26,220.73, which is not what
either report prints. The report's own rendering is unaffected by this choice — it is the *derivation*
in this table that is scoped. Measured with:

```sql
SELECT a.type, l.tax_type, count(*) FILTER (WHERE l.net_amount>0) dr_lines,
       sum(l.net_amount) FILTER (WHERE l.net_amount>0) dr,
       count(*) FILTER (WHERE l.net_amount<0) cr_lines,
       sum(-l.net_amount) FILTER (WHERE l.net_amount<0) cr
  FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id
  JOIN accounts a ON a.account_id=l.account_id
 WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2' GROUP BY 1,2;
```

**Both net columns are arithmetically correct for goXero's own ledger and tie to it to the cent**
(21,522.45 also equals goXero's own P&L purchases + net operating expenses to the cent). Net Sales is
Xero's figure exactly; Net Purchases carries the same single 250.00 as every other report.

**The tax columns are blank and the report explains why with a false statement.** The trailing row
read from `internal/handlers/report_render.go:1003-1004` says:

> *"Tax Collected, Tax Paid and Net Tax are not represented: no posted journal line carries a tax
> amount, so there is nothing to report rather than a zero to print."*

Measured against the database:

| Claim | Measured |
|---|---|
| "no posted journal line carries a tax amount" | **`gl_journal_lines` has 33 lines with a non-zero `tax_amount`, totalling 74.16** (all `tax_type = INPUT`, all from `BANKTRANSACTION` journals) |
| — | and **122 lines carry a non-empty `tax_type`** |
| — | and **`invoice_line_items` has 87 lines, 85 of them with a non-zero `tax_amount`, totalling 4,777.98** |

**The import is not wrong; the report's stated premise is.** The branch fires because the rate-level
check in `internal/repository/report.go:777-840` requires *every* line of a side to carry a tax amount
(`measuredSales > 0 && measuredSales == salesLines`) before it will print a rate, and no side satisfies
that — which is a different, narrower condition than "no line carries a tax amount". A reader is told
there is no tax data when there is. Described, not fixed.

#### The running server is serving code that is no longer in the tree

**This is the most important line in this section.** The sentence the API returns today is the one
quoted above. The sentence in the source today is not.

| | Text | When |
|---|---|---|
| `GET /api/v1/reports/bas?fromDate=2026-01-01&toDate=2026-12-31`, trailing row | *"Tax Collected, Tax Paid and Net Tax are **not represented: no posted journal line carries a tax amount**, so there is nothing to report rather than a zero to print."* | fetched 18:26 |
| `internal/handlers/report_render.go:1003-1006`, `taxColumnsCaveat` | *"Tax Collected, Tax Paid and Net Tax are **left empty: no tax rate carries a tax amount on every one of its posted lines**, and this report will not total part of a rate. **Some lines do record a tax amount**; they are not summed here because the rest of their rate's lines do not."* | file mtime **18:22:34** |

The tree's version is **accurate** — it says exactly what the database shows (33 lines with a
`tax_amount`, 122 with a `tax_type`) — so the defect described above has been **fixed in source during
this verification**; the string was rewritten while the check was running. **The API server has not been
restarted** (`/tmp/goxero-server-shurco`, started 17:49:01, 20 minutes before the edit), so **:8080 is
still answering with the old text.** `grep -rn "no posted journal line carries a tax amount" internal/`
now returns **nothing**.

The dispatch for this task says the API server "is already serving the current code" and forbids
restarting it. That was true when the check began and stopped being true at 18:22:34. **Every
behavioural claim in this document is a claim about the process on :8080**; where the tree and the
process differ, it is named. This is the only difference found: the strings that other reports render
(Cash Summary's caveat at `report_render.go:890`, the Trial Balance header note at `:146`, Budget
Summary's *"No budget is stored"* at `:1090`) are present in the tree at those lines **and** returned by
the API, unchanged.

### 1.12 Budget Summary — `/api/v1/reports/budget-summary?fromDate=2026-01-01&toDate=2026-12-31`

| Check | Result |
|---|---|
| Renders Xero's layout? | Yes — header `Account | Budget` |
| Rows over zero accounts? | **No rows at all** |
| A Total row? | **None** — verified: no `Total` row appears anywhere in the payload |
| States in its own ReportTitles that no budget is stored? | **Yes** — ReportTitles includes *"No budget is stored: this organisation has no budget in goXero (there is no budget table in the schema)…"* |
| `date` parameter read? | `date` only (`report.go:435`); the `fromDate`/`toDate` passed were ignored and the title says "For the year to 11 September 2026" |
| Does `reports-catalog.ts` no longer promise a budget? | **Correct** — "Budget Manager" and "Budget Variance" both carry `href: null` (`web/src/lib/reports-catalog.ts:41` and `:57`), and "Budget Summary"'s description (`:51-53`) reads *"goXero stores no budget, so this report says so instead of printing one."* |

**This report passes.** A report that honestly says it has nothing to show, and prints no Total over
nothing, is correct behaviour.

### 1.13 Form 1120 — `/api/v1/reports/form-1120?fromDate=2026-01-01&toDate=2026-12-31` and `/api.xro/2.0/Reports/Form1120`

No Xero capture exists in `docs/xero-reference/`, so only envelope, period and internal consistency
were checked.

| Check | Result |
|---|---|
| Period honoured? | **Yes** — rendered title reads "From 1 January 2026 To 31 December 2026" |
| Parameters read | `toDate`, `fromDate` (defaults today / financial-year start); 400s if `toDate < fromDate` (`report_form1120.go:50-74`) |
| Xero-compatible path | `/api.xro/2.0/Reports/Form1120` returns a **byte-identical** body to `/api/v1/reports/form-1120` (sha `1fac974845b8d70d`) — the documented Xero path works |

### 1.14 The GL grand total, re-measured from the database

```sh
docker exec bonefish-postgres-1 psql -U goxero -d goxero -c \
"SELECT count(*) AS lines, sum(net_amount) AS net,
        sum(CASE WHEN net_amount>0 THEN net_amount ELSE 0 END) AS pos,
        sum(CASE WHEN net_amount<0 THEN -net_amount ELSE 0 END) AS neg
   FROM gl_journal_lines l JOIN gl_journals j ON j.journal_id=l.journal_id
  WHERE j.organisation_id='6823b27b-c48f-4099-bb27-4202a4f496a2'"
```

| | value |
|---|---:|
| positive sum of `net_amount` | **108,392.54** |
| negative sum of `net_amount` | **108,392.54** |
| line count | **431** |
| journal count | **157** |

This **reproduces the coordinator's 108,392.54 exactly**. Subtracting the 250.00 of contamination
described in §7(a) gives the import's own total **108,142.54**, and the gap to Xero's grand total is
**111,263.08 − 108,142.54 = 3,120.54**, itemised next.

### 1.15 The reference's own counts against `reconciliation.md` §2, §4, §5 and §6

| §2 / §5 claim | Measured | Verdict |
|---|---|---|
| "1,280 journal lines" | `journal-report.txt` has **751** data rows; the DB has **431** | **claim unsupported** |
| "sum of debits = sum of credits = 42,595.46" | `journal-report.txt` grand Total = **194,720.04**; DB = **108,392.54** | **claim unsupported** |
| "Accounts whose reproduced total differs: 0 of 25" (§4) | DB reproduces 24 of 25 on a net basis, and exactly 25 of 25 once the 250.00 contamination is excluded | holds for the import |
| "Sum of reproduced debits 42,595.46 / credits 42,595.46" (§4) | the DB's ledger total is 108,392.54 gross; 42,845.46 net-per-account | different basis, see §1.2 |
| §5: 630 Inventory "6 lines" | Xero has 6 rows in 630; goXero has **0** | missing |
| §5: 877 Tracking Transfers "10 lines" | Xero has 10 rows in 877; goXero has **0** | missing |
| §5: Accounts Receivable "59 lines" | Xero's file shows **59** rows in 610 | matches |
| §5: Accounts Payable "65 lines" | Xero's file shows **65** rows in 800 | matches |
| §5: Sales Tax "109 lines" | Xero's file shows **109** rows in 820 | matches |
| §5 prose: "It holds **543 ledger lines** in 27 account sections" | the file holds **461** date-prefixed ledger lines across 27 sections — and **§5's own table sums to 461** (85+40+1+3+2+6+3+4+2+2+3+3+17+3+3+3+4+19+59+6+1+2+65+5+109+1+10) | **the prose contradicts its own table** |

All **27** per-account line counts in §5's table match the file exactly (verified section by section);
it is only the "543" in the prose that does not. The file accounts for every line: 461 ledger lines
(the ones that begin with a date), 5 preamble lines, 27 `Total <account>` rows, 27 `Net movement` rows
and 27 `##<account>` headers — 547 non-blank lines in all, of which **not one is unaccounted for**, so
there is no hidden 82 lines anywhere in the file.

### 1.16 The 3,120.54, item by item

Each item below was measured from `general-ledger-detail.txt` and confirmed absent from the database.
**Every one is a Dr/Cr pair inside a single account — a wash.** For each: what it is, what it costs
the GL Detail, and whether it can move any figure a report prints.

| # | Account | Xero Dr | Xero Cr | goXero | Where it comes from in the reference | **Can it move a printed figure?** |
|---|---:|---:|---:|---|---|---|
| 1 | **630 Inventory** | 320.00 | 320.00 | 0 / 0 — account present, **0 lines** | 6 `Inventory Opening Balance` rows (journal 651) | **No.** Debit 320.00 and credit 320.00 in the same account, same journal. Net effect on 630 is nil, so no balance, no P&L line and no balance-sheet line moves. It moves only the GL Detail's per-account gross columns, the GL Detail grand total, and the journal-report line count. |
| 2 | **877 Tracking Transfers** | 1,400.27 | 1,400.27 | 0 / 0 — account present, **0 lines** | 10 `Credit Note Allocation` rows (journal-report lines 509-518) | **No** — same reason. A balanced wash inside 877. |
| 3 | **610 Accounts Receivable** | −1,104.20 vs Xero | | goXero is 1,104.20 **lower on each side** | 6 allocation rows: 541.25 Dr+Cr, 541.25 Dr+Cr, 21.70 Dr+Cr | **No.** Three wash pairs inside 610. 610's balance is unchanged; only its gross Dr/Cr columns and the grand total shrink. |
| 4 | **800 Accounts Payable** | −296.07 vs Xero | | goXero is 296.07 **lower on each side** | 4 allocation rows: 270.63 Dr+Cr, 25.44 Dr+Cr | **No.** Two wash pairs inside 800. Same reasoning. |
| | **Total** | | | | **320.00 + 1,400.27 + 1,104.20 + 296.07 = 3,120.54** | |

**Confirmed from the database** that goXero holds none of these:

```sh
docker exec bonefish-postgres-1 psql -U goxero -d goxero -c \
"SELECT count(*) FROM gl_journals WHERE reference ILIKE '%allocat%' OR source_type ILIKE '%allocat%'"
# -> 0
```
and accounts 630 and 877 exist with zero `gl_journal_lines` rows.

**Therefore: not one of the 3,120.54 can move any balance that any report prints.** All four items
affect only (a) the General Ledger Detail's per-account gross Dr/Cr columns and its grand total,
(b) the General Ledger Detail's and Account Transactions' line counts, and (c) the Journal Report's
line count and grand total. No Trial Balance figure, no P&L figure, no Balance Sheet figure, no Bank
Summary figure, no Aged figure moves because of them.

#### The 111,263.08-versus-42,595.46 gap in `reconciliation.md` line 147 is not a missing posting

The two numbers are on **different bases**:

* Xero's **Trial Balance** total (42,595.46) is the sum of **each account's net on its own side** —
  debits and credits inside one account cancel before totalling.
* Xero's **General Ledger Detail** grand total (111,263.08) is the sum of **gross Dr and gross Cr** —
  nothing cancels.

Measured on goXero's own ledger, the same two bases give **42,845.46** and **108,392.54**. The
difference between them, 65,547.08 on the goXero side and 68,667.62 on Xero's, is entirely the wash
volume (gross pairs, of which the 3,120.54 is the part the import omits). **It is a basis difference,
not a gap in the import.** `reconciliation.md` §5's sentence attributing the whole 68,667.62 to
accounts 630 and 877 is wrong by a wide margin: 630 and 877 contribute only 1,720.27 of it.

---

## 2. THE DERIVED-FIGURE CHECK — migration 00023's forced 7,430.22

**Question:** migration 00023 forces the Business Bank Account to 7,430.22 through one derived
opening-balance journal against account 840. Identify that journal and the row that owns the opening
balance **today**; say whether any later migration added a second owner.

**Answer: one owner today, and no tuned number survives. There is no second owner.**

Migration `00023_xero_reference_chart_of_accounts.sql` (lines 530-550) derives the figure and says so
in a comment: *"the captured transactions move it by −1223.79, so the balance it must already have
carried is 7,430.22 − (−1223.79) = 8654.01"*. It inserts a journal with lines `('090', +8654.01)` and
`('840', −8654.01)`.

Migration `00024_xero_reference_source_documents.sql` **deletes that journal** — inside `-- +goose Up`,
lines 1270-1272:

```sql
DELETE FROM gl_journals … OR journal_id = uuid_generate_v5(uuid_ns_url(), 'goxero/xero-ref/opening-balance/journal');
```

and recreates it only in the **Down** block (the ledger delete at 2047, the journal at 2080-2084, its two lines at 2086-2092).

What owns 090's opening balance **today**, measured from the live database:

| | |
|---|---|
| journal_number | **1603** |
| journal_id | **`2a56a21a-8a5a-5ae5-b69b-ab29a94f8217`** |
| journal_date | **2026-06-21** |
| source_type | `MANUALJOURNAL` |
| reference | **"Conversion Balance"** |
| lines | **090 Business Bank Account +4,130.98** / **840 Historical Adjustment −4,130.98** |
| created | 17:30:46 on 2026-09-11 (migration 00024) |

This is Xero's own journal **ID 388 "Conversion Balance"** (`reconciliation.md`: *"ID 388 Conversion
Balance (21 Jun 2026): Dr 090 Business Bank Account 4,130.98 / Cr 840 Historical Adjustment
4,130.98"*), reproduced as a captured source document rather than a tuned number.

**No second owner.** Three independent probes, all returning zero or one:

| Probe | Result |
|---|---|
| `SELECT count(*) FROM gl_journal_lines WHERE abs(abs(net_amount) − 8654.01) < 0.001` | **0** — the derived figure appears nowhere in the database |
| `SELECT count(*) FROM gl_journal_lines l JOIN gl_journals j … JOIN accounts a … WHERE a.code IN ('090','840') AND j.journal_date <= '2025-12-31'` | **0** — nothing pre-dates the year; there is no separate opening-balance row |
| journals whose `reference` matches `%opening%` or `%conversion%`, or whose `journal_number` is 1603 | **exactly 1** (journal 1603 above) |
| 090 lines with `net_amount = 4130.98` | **exactly 1** — one row owns it |

**090's 7,430.22 is now earned, not asserted:** it is `0.00 opening + 26,923.45 received − 19,493.23
spent`, both legs coming from the captured bank transactions (§6). The number is right for the right
reason.

*One residual observation, stated but not a second owner:* journal 1603 is *itself* the mechanism that
makes Xero's 840 = 4,130.98 appear, so 090 and 840 are still mutually determined by a single row. That
is exactly Xero's own structure (Xero's journal 388 does the same), so it is not a defect — but it
does mean 090's opening balance and 840's balance can only ever be wrong together.

---

## 3. THE RENDER CHECK

Driven in a real browser with the `orca` CLI, every command carrying `--page
d2fa6f46-788c-440b-8d08-a24fa828b513` — a tab this worker created (`orca tab create --url …`) and
whose URL was re-read from `orca tab list --json` **before every read** and compared with the URL
requested. The sweep enumerates the routes from the filesystem rather than from a list this worker
typed (`ls -d web/src/routes/app/reports/*/`), so it cannot miss a page that exists:
**the hub plus all 25 directories under `web/src/routes/app/reports` = 26 routes**, run at 18:41-18:48
on 11 Sep 2026. `/app/reports/bas` is a 27th navigation — the API advertises that path (§5.1) so it was
tried even though no directory exists for it. **The two by-contact routes were created at 18:22:34,
during this verification**, and are recorded in both states below.

**The drift guard earned its keep.** On the first pass `/app/reports/bank-summary` was captured with
`tab_url_before = http://localhost:5173/app/reports/balance-sheet` — the previous page, still on
screen because the navigation had not settled — and its byte count was identical to the balance
sheet's. **That capture is discarded, not reported.** The route was re-read on its own with a
wait-until-landed loop; the clean read returns
`Bank Summary · Demo Company (Global) · From 1 September 2026 To 11 September 2026` with
090 `1,342.39 / 7,108.18 / 1,020.35 / 7,430.22`, 091 `−250.00 / 0.00 / 0.00 / −250.00`, Total
`1,092.39 / 7,108.18 / 1,020.35 / 7,180.22` — internally consistent
(`1,342.39 + 7,108.18 − 1,020.35 = 7,430.22`). The table below records only clean reads.

| Route | Renders | Spinner stuck | Blank table | Error | "Not available" badge on an API-served report | Notes |
|---|---|---|---|---:|---|---|
| `/app/reports` (hub) | ✅ | 0 | no | no | n/a | 60 `reports-catalog.ts` entries: **20 carry an `href`** (17 distinct report URLs — `sales-tax`, `cash-summary` and `trial-balance` each appear twice), **40 rendered "NOT AVAILABLE"**. Measured on the page: 40 badge strings, 17 distinct report links |
| `/app/reports/trial-balance` | ✅ | 0 | no | no | no | table populated from the API |
| `/app/reports/profit-and-loss` | ✅ | 0 | no | no | no | |
| `/app/reports/balance-sheet` | ✅ | 0 | no | no | no | |
| `/app/reports/bank-summary` | ✅ | 0 | no | no | no | |
| `/app/reports/cash-summary` | ✅ | 0 | no | no | no | renders; content disagrees with Xero (§1.6) |
| `/app/reports/aged-receivables` | ✅ | 0 | no | no | no | |
| `/app/reports/aged-payables` | ✅ | 0 | no | no | no | |
| `/app/reports/general-ledger-detail` | ✅ | 0 | no | no | no | |
| `/app/reports/account-transactions` | ✅ | 0 | no | no | no | |
| `/app/reports/journal-report` | ✅ | 0 | no | no | no | |
| `/app/reports/general-ledger` | ✅ | 0 | no | no | no | the router-only alias of `general-ledger-detail` (§5.3); its page heading is "General Ledger" and the body's own title is "General Ledger Detail" |
| `/app/reports/sales-tax` | ✅ | 0 | no | no | no | |
| `/app/reports/bas` | **❌ 404** | n/a | n/a | **404 as a page** | n/a | **the API serves `/reports/bas` and the index advertises it, but no `web/src/routes/app/reports/bas/` directory exists**, so SvelteKit returns *"404 / Page not found / This address doesn't exist or was moved."* Nothing on the site links to it — `reports-catalog.ts` links `/app/reports/sales-tax` instead, and that page fetches this endpoint. See finding 2 below |
| `/app/reports/executive-summary` | ✅ | 0 | no | no | no | |
| `/app/reports/budget-summary` | ✅ | 0 | no | no | no | honest empty state (§1.12) |
| `/app/reports/form-1120` | ✅ | 0 | no | no | no | |
| `/app/reports/bank-reconciliation` | ✅ | 0 | no | no | no | not an API report |
| `/app/reports/uncoded-statement-lines` | ✅ | 0 | no | no | no | not an API report |
| `/app/reports/cash-flow` | ✅ | 0 | no | no | no | **redirects** to `/app/reports/cash-summary` |
| `/app/reports/dashboards` | ✅ | 0 | no | no | no | **redirects** to `/app` |
| `/app/reports/business-snapshot` | ✅ | 0 | no | no | **honest stub** | "UNDER DEVELOPMENT / This section is not yet wired to the API" |
| `/app/reports/health` | ✅ | 0 | no | no | **honest stub** | same |
| `/app/reports/visualisations` | ✅ | 0 | no | no | **honest stub** | same |
| `/app/reports/short-term-cash-flow` | ✅ | 0 | no | no | **honest stub** | same |
| `/app/reports/aged-receivables-by-contact` | ❌ → ✅ | 0 | no | **404 at first pass** | no | **no route existed at 18:02; `+page.svelte` appeared at 18:22:34 and the route now renders — see below** |
| `/app/reports/aged-payables-by-contact` | ❌ → ✅ | 0 | no | **404 at first pass** | no | same |

**No route the site links to failed the stated test.** Every clean read rendered a populated `main`
with a populated table where a table belongs; none showed an error, a blank table, a never-resolving
spinner (no `loading`/`spinner` text remained on any page after the load settled), or a "Not available"
badge on a report the API serves. **One route in the sweep did fail, and it is the exception that
proves the rule: `/app/reports/bas` 404s** — a path the API advertises and serves with a full report,
for which the front end has no page at all. Nothing links to it, so no user can reach it by clicking;
it is reachable only by typing the URL. It is recorded as finding 2 below rather than as a broken
report, because the report it would have shown is reachable at `/app/reports/sales-tax`, which renders
the same body under a title the body itself carries. The four stubs honestly say they are not wired to the API and **no API serves them**,
so per the dispatch they are correct behaviour.

### Three render findings (described, not fixed)

1. **`/app/reports/aged-receivables-by-contact` and `/app/reports/aged-payables-by-contact` were
   advertised in the API index and served by the API, but 404'd in the browser — and have been fixed
   during this verification.** At 18:02 both paths were listed in `GET /api/v1/reports` (rows 15 and
   16) and both answered a full Reports envelope from `/api/v1/reports/aged-*-by-contact`, while the
   SvelteKit error page returned *"404 / Page not found / This address doesn't exist or was moved."*
   — no `+page.svelte` existed. **At 18:22:34, mid-verification, another worker created**
   `web/src/routes/app/reports/aged-receivables-by-contact/+page.svelte` and
   `web/src/routes/app/reports/aged-payables-by-contact/+page.svelte` (251 and 257 bytes). **Both
   routes were re-read after that and now render**: *"Aged Receivables by Contact · Demo Company
   (Global) · As at 11 September 2026"* with a populated CONTACT / < 1 MONTH / … / TOTAL table and
   per-contact `Total <contact>` rows, and the same for payables. **End state: no gap.** The finding
   is recorded because the front-end gap existed for the whole of the first pass, and because the
   fix landed while the check was running.
2. **`/app/reports/bas` 404s while the API serves it and the index advertises it.** `GET
   /api/v1/reports/bas` answers a full Reports envelope (`ReportID BASReport`, `ReportName`
   *"BAS / Sales Tax Report"*, period in its titles), and the index's row for `/reports/bas` is not
   one of the dead paths §5.1 looked for. But `web/src/routes/app/reports/` has **no `bas`
   directory**, so `http://localhost:5173/app/reports/bas` returns *"404 / Page not found / This
   address doesn't exist or was moved."* — there is no `<main>` element on it at all (`innerText`
   length 0, against 495 for `/app/reports/sales-tax`). **Nothing links to it**: `reports-catalog.ts`
   points both *"Sales Tax Report"* entries (:358, :399) at `/app/reports/sales-tax`, which is the
   page that fetches `/api/v1/reports/bas` (§4). **So the path is a dead URL that no user can reach
   by clicking, and the report is served — but a client that follows the index's own `Path` field
   into a browser gets a 404.** This is a front-end/back-end path mismatch, not a report defect;
   it is described and **not fixed**.
3. **Two of the four honest stubs are linked from the top navigation.**
   `web/src/lib/components/TopNav.svelte:65-68` links *"Short-term cash flow"* →
   `/app/reports/short-term-cash-flow` and *"Business snapshot"* → `/app/reports/business-snapshot`,
   both of which are the under-development stubs. The dispatch's rule — *"a page that honestly says a
   report is not available and is linked to nothing is correct"* — holds for the pages themselves;
   these two are reachable from global navigation, and the reports hub does not link any of the four.

---

## 4. THE HONESTY CHECK

**Do rendered cells come from the API?** Yes, on all 17 API-backed report pages, and the check was
made against **the exact request each page made**, not against a URL I chose. The browser was asked for
its own list of network requests:

```sh
orca eval --page $PAGE --expression '(()=>({url:location.href,
  hrefs:performance.getEntriesByType("resource").map(e=>e.name).filter(u=>u.indexOf("/api/v1/reports")>=0),
  text:document.querySelector("main")?document.querySelector("main").innerText:""}))()'
```

Each page's rendered `main.innerText` was then matched against the payload returned by the URL in that
page's own `hrefs` list (fetched through the same `Bearer` + `Xero-Tenant-Id` header as everything
else). This matters: the pages do **not** all fetch the endpoint their route is named after (see the
table below), so a check that assumed the obvious mapping would have been comparing the wrong payload.

The tokens counted are **money tokens** — anything matching `\(?-?\d{1,3}(,\d{3})*\.\d{2}\)?%?`,
i.e. an amount or a percentage, in each page's `<main>` only. Account codes, date components, row
counts and the site chrome are not counted.

| | |
|---|---:|
| API-backed report pages rendered and checked | **17** |
| money tokens in their rendered `main` (distinct per page, summed) | **1,691** |
| distinct money tokens across all 17 pages | **662** |
| **not present in the payload that page actually fetched** | **0** |

Matching normalised the API's own formatting — thousands separators, a trailing `%`, a leading `(` for
negatives — before comparison, and the comparison is against any string anywhere in the payload, not
just `Cells[].Value`, so a title or a `ReportDate` can back a token too. **Zero unbacked figures
remain: every amount and percentage rendered on every API-backed report page is traceable to the
payload that page fetched.**

**Negative control.** The matcher is not vacuously permissive. Run against the no-query
`/reports/trial-balance` payload it reports as unbacked every one of: `12,345.67`, `42,595.46`,
`108,392.54`, `21,073.01`, `1,234.56`, `99.99`. So it would have caught a fabricated figure had there
been one, and its silence on `108,392.54` is itself consistent with §1.2 — the *default* Trial Balance
does not print that total.

**What each page actually fetched** (read from `performance.getEntriesByType('resource')`, 11 Sep 2026
18:36-18:40; `$PAGE` = `d2fa6f46-788c-440b-8d08-a24fa828b513`):

| Route | Endpoint the page requested | Was this expected? |
|---|---|---|
| `/app/reports/trial-balance` | `/api/v1/reports/trial-balance` | yes |
| `/app/reports/profit-and-loss` | `/api/v1/reports/profit-and-loss` | yes |
| `/app/reports/balance-sheet` | `/api/v1/reports/balance-sheet?compare=true` | yes (the only page with a default query param) |
| `/app/reports/bank-summary` | `/api/v1/reports/bank-summary` | yes |
| `/app/reports/cash-summary` | `/api/v1/reports/cash-summary` | yes |
| `/app/reports/aged-receivables` | `/api/v1/reports/aged-receivables` | yes |
| `/app/reports/aged-payables` | `/api/v1/reports/aged-payables` | yes |
| `/app/reports/aged-receivables-by-contact` | `/api/v1/reports/aged-receivables-by-contact` | yes |
| `/app/reports/aged-payables-by-contact` | `/api/v1/reports/aged-payables-by-contact` | yes |
| `/app/reports/account-transactions` | `/api/v1/reports/account-transactions` | yes |
| `/app/reports/general-ledger-detail` | `/api/v1/reports/general-ledger-detail` | yes |
| `/app/reports/general-ledger` | `/api/v1/reports/general-ledger` | yes — the §5.3 router-only alias; its body is named "General Ledger Detail" |
| `/app/reports/journal-report` | `/api/v1/reports/journal-report` | yes |
| `/app/reports/sales-tax` | **`/api/v1/reports/bas`** | **no — see below** |
| `/app/reports/executive-summary` | `/api/v1/reports/executive-summary` | yes |
| `/app/reports/budget-summary` | `/api/v1/reports/budget-summary` | yes |
| `/app/reports/form-1120` | `/api/v1/reports/form-1120` | yes |

**One route is labelled one way and fetches another, and it declares that it does.**
`/app/reports/sales-tax` (`web/src/routes/app/reports/sales-tax/+page.svelte`) sets
`title="Sales Tax Report"` but `endpoint="/api/v1/reports/bas"` (line 6). The body it then renders is
not silently mislabelled: the payload's `ReportName` and `ReportTitles[0]` are both **"BAS / Sales Tax
Report"**, and the rendered page shows that string above the table, right under its own "Sales Tax
Report" heading. There is no `bas` route under `/app/reports`, so `/reports/bas` and the page named
for Sales Tax are one front-end page, and the body names both. This is the same alias pair §5.3
judges on the API side; on the front end it is the *page* title that is narrower than the report's own
name, not the reverse. It is recorded here rather than charged as a failure, because the rendered
report announces itself in its own title.

Two of those pages — `/app/reports/aged-receivables-by-contact` and `aged-payables-by-contact` — did
not exist when this check began (they 404'd, §3); their `+page.svelte` files appeared at 18:22:34 on
11 Sep 2026 and were honesty-checked on the spot: 15 and 17 money tokens respectively, **0 unbacked**
(both figures reproduce in the run above). Nothing in them has been taken on trust from that late
arrival. `/app/reports/general-ledger` was added to the set last, at 18:40, so that the count covers
**every** page that carries an `endpoint=` prop — 17 of them, all 17 listed in the table above.

**Two earlier attempts at this check, and why they were thrown away.** Both are recorded because both
produced a number that looked like a result.

1. **Matched against a payload fetched for the FY period while the page rendered its default period.**
   The pages send no period except the balance sheet's `compare=true`, so they render *today's* window
   (as at 11 September 2026; 1-11 September 2026 for the ranged reports), while the payloads on disk in
   `/tmp/verify/final/*.json` had been fetched for 1 January - 31 December 2026. Measured, with the
   `%`-aware matcher used above: Bank Summary 4 unbacked (`1020.35`, `1092.39`, `1342.39`, `7108.18`
   — all period artefacts: the FY payload's Opening is 0.00 and Received 26,923.45 against 1,342.39
   and 7,108.18 for 1-11 September); Trial Balance 23 unbacked (the movement columns cover 1-11
   September on screen and the whole year in the payload). Both are **0** against the payload the page
   actually fetched.
2. **A matcher that did not strip a trailing `%`.** The first version normalised a payload string with
   `float(v.replace(',',''))`, which raises on the API's `"91.75%"`. That is what flagged Aged
   Receivables' `Percentage of total` row (`100.00`, `91.75`, `8.25`) as unbacked — with the payload in
   the page's own hands and the `%` stripped, all three are present verbatim (`"100.00%"`, `"91.75%"`,
   `"8.25%"`).

Neither attempt says anything about honesty. Both say the check has to use the page's own request, the
page's own period, and the API's own formatting.

**One caveat on the "0 unbacked" result.** It says the pages print nothing the API did not send. It
does not say the API is right, and in one place it is not: the Sales Tax / BAS page's trailing row in
the *running* build still reads *"no posted journal line carries a tax amount"* while the tree's
`taxColumnsCaveat` has said otherwise since 18:22:34 — that is finding 2 and §1.11, and it is a
serving-stale-code problem, not a renderer problem.

**Are there hardcoded figures, dates, organisation names or "as at" strings in the renderer?**

Three greps, all run over `web/src/routes/app/reports/**` and
`web/src/lib/components/ReportView.svelte`.

```sh
# A. figures, dates, organisation names, "as at"
grep -rnE "[0-9]{1,3},[0-9]{3}(\.[0-9]{2})?|as at|As at|Demo Company|20[0-9]{2}-[0-9]{2}-[0-9]{2}" \
  web/src/routes/app/reports/ web/src/lib/components/ReportView.svelte
#    -> no matches (exit 1)

# B. any numeric literal at all, filtered of CSS/SVG noise
grep -rnE "[0-9]+\.[0-9]+|'[0-9]+'|\"[0-9]+\"" \
  web/src/routes/app/reports/ web/src/lib/components/ReportView.svelte

# C. literal-currency fallbacks
grep -rnE "\?\? *'[A-Z\$]" web/src/routes/app/reports/ web/src/lib/components/ReportView.svelte
```

| Hit | file:line | Is it a hardcoded figure? |
|---|---|---|
| `Math.abs(unreconciled(r) ?? 0) > 0.004` | `web/src/routes/app/reports/bank-reconciliation/+page.svelte:117` | **No** — a floating-point tolerance for a reconciliation badge, not a report figure |
| SVG path/geometry literals (`viewBox` coordinates, `stroke-width="2"`, `r="1.5"`) | `web/src/routes/app/reports/+page.svelte:101,112,133,200,230,234,272-274` | **No** — icon geometry |
| org-currency fallback chain `l.CurrencyCode \|\| …CurrencyCode \|\| org?.BaseCurrency \|\| 'USD'` (found by reading, not by grep C — it uses `\|\|`, not `??`) | `web/src/routes/app/reports/uncoded-statement-lines/+page.svelte:22-26` (`currencyOf`, :22) | **No** — a currency-code fallback when the org has not loaded |
| `const baseCurrency = $derived(org?.BaseCurrency ?? 'USD');` | `web/src/routes/app/reports/uncoded-statement-lines/+page.svelte:80` | **No** — same |
| `const baseCurrency = $derived(org?.BaseCurrency ?? 'USD');` | `web/src/routes/app/reports/bank-reconciliation/+page.svelte:50` | **No** — same |
| `class="… py-1.5 …"` | `web/src/lib/components/ReportView.svelte:205` | **No** — a Tailwind spacing class |

Grep A returns **nothing at all** — there is no hardcoded money figure, no hardcoded date, no
organisation name and no "as at" string anywhere in a report route or in `ReportView.svelte`. Grep B
returns only SVG geometry (`viewBox`, `<path d>`, `r="1.5"`, `stroke-width="2"`), Tailwind class
decimals (`mt-0.5`, `py-1.5`), and the `0.004` tolerance — no money figure, no date. After the
filter that removes CSS/SVG noise, grep B's entire content hit list across both trees is exactly:
`bank-reconciliation/+page.svelte:117` (`0.004`) and `ReportView.svelte:205` (`py-1.5`).

**The `'USD'` fallbacks in grep C are the only domain literals, and none of them is ever printed as a
report figure** — each chooses a currency symbol for a bank statement line when the org object has not
loaded yet, and the org object always loads (it is fetched on mount). No report route and no part of
`ReportView.svelte` contains a money figure, a date, an organisation name or an "as at" string.

`ReportView.svelte` (221 lines) is a generic renderer: `let { title, endpoint, defaults = {}, fields = [] }: Props = $props()`, `params = $state(untrack(() => ({ ...defaults })))`, and
`report = data?.Reports?.[0] ?? data?.Payload?.Reports?.[0] ?? null`. **Its title, its dates and every
cell come from the API response; it holds no report literal of its own.**

---

## 5. THE ANTI-ALIAS CHECK

Every `Path` in `GET /api/v1/reports` was fetched and its **row body** hashed. No two endpoints return
the same report under different names except where the alias declares itself.

### 5.1 The 17 advertised paths

All 17 answer **200** and return their advertised `ReportID` in `Reports[0].ReportID`. No path is dead.

| Path | ReportID served | Body sha (16) |
|---|---|---|
| `/reports/trial-balance` | TrialBalance | `75933055e7d72a90` |
| `/reports/profit-and-loss` | ProfitAndLoss | `696154d18951e3f5` |
| `/reports/balance-sheet` | BalanceSheet | `7fa365e027285121` |
| `/reports/cash-summary` | CashSummary | `26da7ce03205bee7` |
| `/reports/bank-summary` | BankSummary | `3cf99507155c7018` |
| `/reports/aged-receivables` | AgedReceivables | `db8f27b9551a9436` |
| `/reports/aged-payables` | AgedPayables | `f4077ac6eef555df` |
| `/reports/executive-summary` | ExecutiveSummary | `5c7aae7d405d1e8d` |
| `/reports/budget-summary` | BudgetSummary | `f5877a942808b758` |
| `/reports/bas` | BASReport | **`c56e460bc6a9b2c6`** |
| `/reports/sales-tax` | SalesTaxReport | **`c56e460bc6a9b2c6`** |
| `/reports/journal-report` | JournalReport | `796bd8d86158fa8f` · `ae11d62860ee496f` † |
| `/reports/account-transactions` | AccountTransactions | `bbb5e0e6839489cc` · `2514f761096aaace` † |
| `/reports/general-ledger-detail` | GeneralLedgerDetail | `33a92bca2394af64` · `19a8e25d1794cd44` † |
| `/reports/aged-receivables-by-contact` | AgedReceivablesByContact | `396ed2d933ef39c2` |
| `/reports/aged-payables-by-contact` | AgedPayablesByContact | `e7fa7dc275b53b4d` |
| `/reports/form-1120` | Form1120 | `1fac974845b8d70d` |

*(Hash procedure — reproducible: fetch the path, take `Reports[0].Rows`, re-serialise it with
`json.dumps(rows, sort_keys=True, separators=(',',':'))`, and take the first 16 hex characters of its
SHA-256. The two identical values are shown in bold.)*

**† This hash is not a stable key, and these three are the only ones that are not.** All three are
built by `journalFeed`, whose `ORDER BY a.code, j.journal_date, j.journal_id` has no unique
tiebreaker (§5.7), so the row order — and therefore the body — changes as the heap order under the
tie changes. **Two values were observed for each path and both are printed above.** Every other hash
in this table was reproduced unchanged on re-fetch, and the two alias identities held in both
observed orders. Nothing in this table is an assertion about figure equality — that is §1.

### 5.2 The one genuine alias

**`/reports/bas` and `/reports/sales-tax` return the same row body** (sha `c56e460bc6a9b2c6`) under
two different `ReportID`s (`BASReport` / `SalesTaxReport`) and two different `ReportName`s
(`"BAS / Sales Tax Report"` / `"Sales Tax Report"`).

Per the dispatch's rule, two endpoints that are deliberately aliases may pass **only if the report
says so in its own title or a trailing row**. It does: the BAS variant's `ReportName` is **"BAS /
Sales Tax Report"**, which names both. The dispatch offered "its own title" as sufficient, and
`ReportName` is the report's title field. **This one passes — narrowly, and only in the BAS direction.**
The Sales Tax variant's name does not name BAS, and no trailing row in either body declares the
aliasing. A stricter reading would fail it; the stated rule admits it.

### 5.3 Router-only endpoints that are byte-identical aliases and are NOT in the index

| Path | Identical to | sha | In the index? | Declares itself? |
|---|---|---|---|---|
| `/api/v1/reports/profit-loss` | `/api/v1/reports/profit-and-loss` | `696154d18951e3f5` | **No** | **No** |
| `/api/v1/reports/general-ledger` | `/api/v1/reports/general-ledger-detail` | `33a92bca2394af64` ≡ `19a8e25d1794cd44` † | **No** | **No** |

Both are registered in `internal/router/router.go` (lines 329 and 347) and both return a **byte-identical**
body to their canonical route. Neither is advertised in the index and neither says anywhere in its own
output that it is an alias.

*How "identical" was established, given §5.7:* the two responses are byte-identical **and** identical
as multisets of flattened rows. Eight fetches of each of the four paths produced exactly one distinct
body per pair (8/8), and the pairs also agreed in two independent capture rounds taken during the two
different row orders observed for the same handler. Because both aliases share one handler and one
query, they can only disagree with each other if the database returns two different orders to two
back-to-back requests — which was not observed in any of the 16 paired calls. The parity test knows about them: `aliasRoutes` at
`internal/handlers/reports_parity_integration_test.go:493` maps both and the test requires the index to
agree with the router **in both directions** — which is how the index stays at 17 while the router
carries 20 report routes. **They are deliberate and tested, but they do not meet the dispatch's
self-declaration rule.** Recorded as a finding against the stated rule, not against the design intent.

### 5.4 The one non-report under the `/reports` prefix

`GET /api/v1/reports/invoice-summary` (registered at `internal/router/router.go:326` →
`invoiceHandler.Summary`) answers a **bare object, not a Reports envelope**:

```json
{"authorised":21,"draft":2,"overdue":10,"paid":44,"totalDue":"21596.93","totalInvoices":78,"totalPaid":"41163.7"}
```

**What the owning worker did:** it is **deliberately classified as a non-report** in the parity test —
`nonReportRoutes` (`internal/handlers/reports_parity_integration_test.go:502`) maps the path to
*"the invoices and sales screens' own KPI object"* and `nonReportShape` (`:509`) pins its seven keys
(`totalInvoices`, `draft`, `authorised`, `paid`, `overdue`, `totalDue`, `totalPaid`); the test asserts
at `:592-594` that the route no longer serves any of them if the shape drifts — and it is **excluded from the index** (the index's 17 rows do
not include it) and **excluded from the path list** of
`TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope`
(`internal/handlers/reports_parity_integration_test.go:527`, function body). **End state: the exception is
handled honestly and is covered in both directions** — the test asserts the index and the router
agree, so the one path that breaks the envelope cannot silently re-enter either. It is a status/KPI
object that happens to live under `/reports`, and the code says so.

### 5.5 The Xero-compatible path

`/api.xro/2.0/Reports/Form1120` (router lines 353-354) returns a body **identical** to
`/api/v1/reports/form-1120` (sha `1fac974845b8d70d`). This is a **documented Xero-compatible path** —
Xero's Reporting API answers on `GET /api.xro/2.0/Reports/<ReportID>` — and the router comment says so.
It is not an undeclared alias; it is the compatibility surface.

### 5.6 What `Path` means

The index advertises a `Path` alongside `ReportID` and `ReportName`. **Conclusion from the code and the
front end: `Path` is the API route on this server, relative to `/api/v1` — not a front-end route and
not Xero's reporting URL.**

* **From the code:** `ReportsList` (`internal/handlers/report.go:452+`) builds the entries literally as
  `"/reports/<slug>"` strings that correspond one-for-one to the `apiV1.Get("/reports/…")` registrations
  in `router.go:325-348`. Two entries (`…/aged-receivables-by-contact`, `…/aged-payables-by-contact`)
  have **no** front-end page at all (§3) yet are advertised — which would be impossible if `Path` meant
  a UI route.
* **From the front end:** nothing in `web/` reads `Path`. `web/src/lib/reports-catalog.ts` carries its
  own, separately maintained `href` values (with `href: null` for unimplemented reports), and
  `ReportView.svelte` receives its `endpoint` from the route, not from the index.
* **Every advertised `Path` answers** when fetched as `GET /api/v1<Path>` — verified one by one (§5.1).

---

### 5.7 One deviation inside a report body — a row order that is not stable

The anti-alias question is really "do two endpoints return the same report?", and the honest answer
needs a stable notion of "the same body". For three of the reports it does not exist, and that itself
is a finding.

`journalFeed` (`internal/repository/report.go:863-876`) orders by
`ORDER BY a.code, j.journal_date, j.journal_id` (**:874**). Within one journal, two lines posted to
the same account on the same date tie on that key and there is no `l.line_id` tiebreaker. It backs
**three** endpoints — General Ledger Detail, Account Transactions and Journal Report — so all three
have a body whose row order is decided by the physical heap order.

Observed directly: `/api/v1/reports/general-ledger-detail?fromDate=2026-01-01&toDate=2026-12-31`
returned sha `19a8e25d1794cd44` at 17:40:37 and 18:12:58 and sha `33a92bca2394af64` at 17:59:11 and
18:17:53 — a difference of exactly **two swapped pairs of rows** in one account, on unchanged data. The full consequence
(printed `Balance` values that differ between two calls) is §1.9.

**Does this break the anti-alias conclusion?** No. The two alias pairs are byte-identical *as
observed*, repeatedly, including in both observed orders (8/8 fetches each, plus two capture rounds).
The identity is a property of the two routes sharing one handler; the instability is a property of the
shared query and is the same for both sides of each pair.

**Does it break any figure in this report?** No. Every total, balance and count cited anywhere in
this document comes from a SQL aggregate (`internal/repository/report.go:936-947` among others) or
from a multi-set of rows, neither of which the order affects. §6 was re-derived from the database and
is order-independent.

**Root cause for a fix — not applied:** `internal/repository/report.go:874`.

---

## 6. THE INTEGRITY CHECK — re-derived independently from the database

Every quantity in this section is an aggregate or a set, so none of it depends on the row ordering
that §1.9 and §5.7 describe; all of it was reproduced unchanged on re-derivation.

All queries: `docker exec bonefish-postgres-1 psql -U goxero -d goxero -c '…'`, organisation
`6823b27b-c48f-4099-bb27-4202a4f496a2`.

### 6.1 Trial Balance debits equal credits

```sql
SELECT sum(net_amount), count(*) FROM gl_journal_lines l
  JOIN gl_journals j ON j.journal_id = l.journal_id WHERE j.organisation_id = '…';
```

| | |
|---|---:|
| sum of all `net_amount` | **0.0000** |
| lines | **431** |
| journals | **157** |
| every journal dated between | 2026-03-05 and 2026-09-11 |

**Debits equal credits exactly — to the fourth decimal, not merely rounded.**

### 6.2 The TB Total row equals the sum of its rendered rows AND the ledger aggregate

| Column | Sum of the rendered section totals | Rendered `Total` row | Identical? |
|---|---:|---:|---|
| Debit | 1,019.95 + 775.98 + 21,029.97 + 64,712.91 + 20,853.73 = **108,392.54** | **108,392.54** | ✅ |
| Credit | 30,559.13 + 0.00 + 283.50 + 43,639.90 + 33,910.01 = **108,392.54** | **108,392.54** | ✅ |
| YTD Debit | 1,019.95 + 775.98 + 21,029.97 + 21,323.01 + 0.00 = **44,148.91** | **44,148.91** | ✅ |
| YTD Credit | 30,559.13 + 0.00 + 283.50 + 250.00 + 13,056.28 = **44,148.91** | **44,148.91** | ✅ |

The Debit and Credit columns also equal the **ledger aggregate** (108,392.54 = the sum of every
positive `net_amount` and of every negative one, §6.1). **The Total row is exact on all four columns.**

Per-account: every account's `Debit`/`Credit`/YTD cell in the payload equals that account's
`sum(CASE WHEN net_amount>0 …)`/`sum(CASE WHEN net_amount<0 …)`/net across the ledger. Verified account
by account; no account disagrees.

### 6.3 Balance Sheet: assets = liabilities + equity

| Side | Accounts | Sum |
|---|---|---:|
| Assets | BANK 7,180.22 + CURRENT 9,194.51 + FIXED 4,698.28 | **21,073.01** |
| Liabilities | CURRLIAB 13,056.28 | **13,056.28** |
| Equity (current-year earnings) | −REVENUE 29,539.18 + DIRECTCOSTS 775.98 + EXPENSE 20,746.47 | **8,016.73** |

**21,073.01 = 13,056.28 + 8,016.73 ✅.** Xero's own identity holds too:
**21,323.01 = 13,056.28 + 8,266.73** — the two differ only by the 250.00 contamination, on the assets
side and the equity side, exactly as §1.4 predicts.

### 6.4 Each bank account: closing = opening + received − spent

| Account | Opening | Received | Spent | Closing (DB) | Closing = O+R−S? | Xero closing |
|---|---:|---:|---:|---:|---|---:|
| 090 Business Bank Account | 0.00 | 26,923.45 | 19,493.23 | **7,430.22** | ✅ | 7,430.22 |
| 091 Business Savings Account | 0.00 | 0.00 | 250.00 | **−250.00** | ✅ | *(not in Xero)* |

**Both bank accounts satisfy the identity exactly.** 090's closing equals Xero's to the cent.

---

## 7. THINGS ALREADY KNOWN — verified, quantified, and not charged to any report

### 7(a) The shared dev database is not pristine

**Observed counts today** (so they are on the record with timestamps, and are **not** counted against
any report):

| Table | Live count | Composition by `created_at` / `created_date_utc` |
|---|---:|---|
| `accounts` | **58** | 58 @ 16:21:50 (migration 00023) — **unchanged, matches 00023's own count** |
| `bank_transactions` | **87** | 1 @ 16:37:09 + **82** @ 17:30:46 (00024) + **4** @ 17:50:03/17:50:10 (another worker's probes) |
| `bank_statement_lines` | **104** | 101 @ 16:21:50 (00023) + **3** @ 16:36:58 (the imported statement lines) |
| `contacts` | **54** | 3 @ 12:54:22 + 21 @ 16:21:50 + 30 @ 17:30:46 |
| `gl_journals` | **157** | 1 @ 16:37:09 + 156 @ 17:30:46 |
| `gl_journal_lines` | **431** | 2 + 429 |

00023's own counts (accounts 58, bank_transactions 48, bank_statement_lines 101, contacts 24,
gl_journals 49, gl_journal_lines 98) are **what migration 00023 left at 16:21:50**; 00024 and later
activity took each table past them. **The current numbers are not 00023's numbers and must not be read
as a migration defect.**

**The 16:36:58–16:37:09 session.** Another session imported **three** statement lines (16:36:58) and
coded **one** bank transaction (16:37:09). The offending transaction is:

| | |
|---|---|
| journal_number | **52** |
| journal_date | **2026-03-05** |
| source_type | `BANKTRANSACTION` |
| reference | **"savings demo"** |
| description | "Office Chair" |
| lines | **Dr 400 Advertising 250.00** / **Cr 091 Business Savings Account 250.00** |
| created | **2026-09-11 16:37:09.120703+02** |

This is a **250.00 SPEND on 2026-03-05** and it is the sole cause of every +250.00/−250.00 difference
in §1.2-1.5. **The Xero reference has no 091 line at all** — account 091 does not appear in
`trial-balance.txt`, `balance-sheet.txt`, `bank-summary.txt` or `general-ledger-detail.txt`. **Not
counted against any report.**

**The 17:50 probe rows.** Four further `bank_transactions` rows were inserted at 17:50:03/17:50:10 by
another worker's probe: references `probe-SPEND-NZD`, `probe-RECEIVE-USD`, `probe-SPEND-USD`, and one
with a blank reference — all `total = 12.0000`, all dated 2026-09-10, all `is_reconciled = f`. **These
created no journals and are unreconciled**, and `internal/repository/report.go` does not read the
`bank_transactions` table at all (verified by grep), so they cannot move any report figure. They do
inflate the raw `bank_transactions` count above.

### 7(b) `/api/v1/reports/invoice-summary`

Answered in §5.4: **handled honestly and end-to-end.** It routes to `invoiceHandler.Summary`
(`internal/router/router.go:326`) and answers a bare object rather than a Reports envelope:
`{"authorised":21,"draft":2,"overdue":10,"paid":44,"totalDue":"21596.93","totalInvoices":78,"totalPaid":"41163.7"}`.

Its classification is deliberate and enforced, not accidental:

* **absent from the index** — the index's 17 rows do not include it;
* **named in `nonReportRoutes`** (`internal/handlers/reports_parity_integration_test.go:502`) with its
  reason, *"the invoices and sales screens' own KPI object"*, and its seven keys pinned in
  `nonReportShape` (`:509`);
* the route list in `TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope` (`:527`) is **enumerated
  from the router**, not hand-written, and the test's second direction (`:563-578`) requires every
  registered `/reports/` route to be *advertised in the index, an alias, or a named non-report* — so
  a route cannot quietly be skipped. The body branch for non-reports (`:585-596`) then asserts the
  response **has no `Reports` key** and still carries all seven documented fields, so the exemption
  cannot rot into something that answers like a report.

**End state: the one path under `/reports` that breaks the envelope is classified, documented and
tested in both directions.** It is a status/KPI object that happens to live under the `/reports`
prefix, and the code says so rather than leaving a hole in the parity sweep.

### 7(c) The index advertises four path strings — verify each answers, and decide what `Path` means

All **17** index paths were fetched (not only four) — see §5.1: **every one answers 200 with its
advertised `ReportID`**. Nothing in the index is dead **as an API path**. **`Path` carries the API
route relative to `/api/v1`**; the reasoning is in §5.6.

One qualification, added on the second pass and *not* something the first pass checked: **`Path` is an
API path, not a browser path.** `Path` values look like `/reports/bas`, and if a reader prefixed
`/app` to one and opened it, `/app/reports/bas` is a **404** (§3 finding 2, §4) — the single index path
with no SvelteKit route behind it. The other 16 all have a `/app/reports/...` page. So the index is
accurate about the API and is not, and does not claim to be, a route table for the web app.

### 7(d) Cash Summary parity

Answered in §1.6, and repeated here as the direct answer to the question asked:

> **goXero does NOT reproduce Xero's sectioning for every line.** Of Xero's ~20 lines, **two** land in
> the same section with the same value (Bank Fees 30.00, General Expenses 46.19). **One** — Historical
> Adjustment (4,130.98) — lands in a **different section and with the opposite sign** (Xero: "Less
> Expenses", −4,130.98; goXero: "Plus Other Cash Movements", +4,130.98). **Six** expense lines land in
> the same section but at **different values** (Advertising, Entertainment, Motor Vehicle, Office
> Expenses, Printing & Stationery, Repairs, Telephone, Travel). **Four** expense lines are **absent
> from goXero entirely** (Cleaning, Consulting & Accounting, Light/Power/Heating, Rent). **Sales**
> (Xero's whole Income section, 21,054.73) has **no counterpart anywhere**; goXero's Income section is
> empty at 0.00. **Fixed Assets** (Xero's whole Other Cash Movements, −2,728.29) has **no counterpart**;
> goXero puts AR/AP/Sales Tax/840 there instead. **Tax Movements** are **blank**, disclaimed in
> ReportTitles. The reported Cash Balance is **7,180.22 against Xero's 7,430.22**.

The Historical Adjustment placement is the symptom, not the disease: the disease is that goXero
classifies the **immediate counterpart account** by its `type` (`internal/repository/report.go:721-733`)
while Xero's Cash Summary looks **through** AR/AP to the invoice or bill coding. The sectioning
question the dispatch raises is answerable and the answer is no — **this is a finding, described and
not fixed.**

---

## 8. SIX THINGS TO CHECK, NOT TRUST

### 8(a) The parameter set of EVERY endpoint

`trial-balance` reads `date`, not `toDate` (`internal/handlers/report.go:70-88`). The full table is
§1.1. **Three endpoints ignore part of the query string**, each with a title that gives it away:

| Endpoint | Reads | Ignores | Evidence from the rendered title |
|---|---|---|---|
| Trial Balance | `date`, `fromDate` | **`toDate`** | — (titles show the period correctly) |
| Executive Summary | `date`, `fromDate` | **`toDate`** | fetched with `toDate=2026-12-31`; title reads **"For the period ending 11 September 2026"** |
| Budget Summary | `date` | **`fromDate`, `toDate`** | fetched for FY 2026; title reads **"For the year to 11 September 2026"** |

Two further traps worth recording: **Bank Summary** and **Journal Report** default `fromDate` to the
**first of the current month**, and **Cash Summary**, **Account Transactions**, **General Ledger
Detail** and **Form 1120** default it to the **financial-year start**. A caller who passes only
`toDate` gets a different period on each. Every report in this document was fetched with both bounds
explicit for that reason.

### 8(b) The Trial Balance prints no 42,595.46 — is the Total row presentation or a wrong figure?

Fetched with `?date=2026-12-31&fromDate=2026-01-01`, the Total row reads
`108392.54 / 108392.54 / 44148.91 / 44148.91`. `42595.46` occurs **0 times** in the payload.

**The netting arithmetic, checked myself:**

* The **five** section totals above the Total row (Revenue, Less Cost of Sales, Less Operating
  Expenses, Assets, Liabilities) sum **exactly** to the Total row on every column —
  `1,019.95 + 775.98 + 21,029.97 + 64,712.91 + 20,853.73 = 108,392.54` and
  `30,559.13 + 0.00 + 283.50 + 43,639.90 + 33,910.01 = 108,392.54`, with the YTD pair giving
  44,148.91 the same way. **The Total is not internally wrong.**
* The same 108,392.54 is what the ledger holds: `sum(net_amount)` positive = 108,392.54, negative =
  −108,392.54, over 431 lines (§6.1). **The Total equals the ledger.**
* 108,392.54 is the **gross movement** total — the sum of every debit and the sum of every credit,
  with nothing netting. Xero's Trial Balance total is the **net-per-account** total: each account's
  debits and credits cancel before the accounts are added.

**I agree it is presentation, not a wrong figure** — with one qualification the dispatch should hear:
the arithmetic is right, but **the report never prints the number a Xero user would compare against**.
Its net-per-account total is 42,845.46 (Xero's 42,595.46 plus the 250.00 contamination of §7(a)), and
that figure appears nowhere. A reader who knows Xero's Trial Balance will look for 42,595.46 and not
find it, and nothing in the payload explains that the Total row is on a different basis. The column
header says Debit/Credit/YTD; it does not say "gross" or "net".

### 8(c) Aged Receivables / Payables and the by-contact variants, cell by cell

Done in §1.7.

* **Buckets:** every bucket assignment matches Xero's — *3 Months*, *2 Months*, *Older* — and the
  bucket subtotals match. AR: 3 Months 8,435.68 / Older 758.83. AP: 2 Months 2,132.51 / 3 Months
  4,031.15 / Older 2,223.10.
* **Percentage of total row:** matches Xero exactly, to two decimals, on both reports — AR
  91.75% / 8.25% / 100.00%; AP 25.08% / 48.77% / 26.15% / 100.00%.
* **Aged Payables / Expense Claims / Total blocks:** all present, in Xero's order, with Xero's figures
  — Total Aged Payables 8,386.76, Expense Claims 115.95, **Total 8,502.71**. ✅
* **Classification of every differing cell:**

| Differing cell | Classification | Why |
|---|---|---|
| zeros printed `0.00` where Xero prints `-` | **presentation** | same value, different zero glyph; Xero's own captures use `-` throughout |
| report name "Aged Receivables" vs Xero's "Aged Receivables Summary" | **presentation** | `ReportName` wording only |
| the Expense Claims claimant's **name** | **neither** — see below | the Xero reference disagrees with itself |
| every amount, bucket and percentage | **match** | no value differences at all |

* **The Expense Claims claimant question — settled.** goXero prints **"Xero Demo"**. The two captured
  Xero files disagree with each other: `aged-payables-summary.txt` says **"Adam Michkevich"**, while
  `general-ledger-detail.txt` (lines 494, 548) and `migrations/data/xero/expense-claims.csv` (line 2,
  column `contact`) both say **"Xero Demo"**. `journal-report.txt` attributes 668 rows of "Posted By"
  to Adam Michkevich and 83 to "System Generated" — he is the org's **posting user**. The renderer
  prints the name in the organisation's users table
  (`internal/repository/report.go:637`: `COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)),''), u.email, …)`),
  and the single unpaid claim (AUTHORISED, `amount_due` 115.95) is linked to user **Xero Demo**
  (`xero.demo@demo.local`). **Verdict: not an arithmetic difference; goXero faithfully prints the name
  its own data holds, and that name appears in two of the four Xero captures including the GL Detail
  that disagrees with the Aged Payables summary.** The reference was left untouched.

### 8(d) Budget Summary

Done in §1.12. It **does** render Xero's layout, it **has no Total row over zero accounts** (there are
no rows at all and no Total), and it **states in its own ReportTitles** that no budget is stored.
`web/src/lib/reports-catalog.ts` **no longer promises a budget** — "Budget Manager" and "Budget
Variance" carry `href: null` with the comment *"No budget editor exists: `migrations/` has no budget
table…"*, and "Budget Summary"'s own description reads *"goXero stores no budget, so this report says
so instead of printing one."* **Passes.**

### 8(e) Sales Tax

Done in §1.11. **Both net columns are correct and tie to the ledger** — Net Sales 29,539.18 =
account 200's `OUTPUT` −29,375.43 over 35 lines plus `OUTPUT2` −163.75 over 5 lines (Xero's own Sales
figure exactly), and Net Purchases 21,522.45 = `INPUT` 19,198.85 + `NONE` 1,823.60 + `OUTPUT2` 500.00,
which equals goXero's own purchases + net operating expenses to the cent and is **+250.00 against
Xero's 21,272.45** — the same single contamination, not a second difference.

The tax columns are blank with a trailing sentence, and **that sentence is false**:
`gl_journal_lines` holds **33 lines with a non-zero `tax_amount` totalling 74.16** and **122 lines with
a `tax_type`**, and `invoice_line_items` holds **87 lines, 85 with a non-zero `tax_amount` totalling
4,777.98**. **Neither the report's figures nor the import is wrong; the report's stated reason for the
blank columns is.** The condition actually tested is narrower — every line of a side must carry a tax
amount before a rate is printed (`internal/repository/report.go:777-840`, the guard itself at :833).

### 8(f) The Cash Summary parity question

Answered in §7(d) and §1.6.

---

## Appendix — source types present in goXero's ledger

For completeness, the composition of the 157 journals / 431 lines every report above is built from:

| `source_type` | journals | lines | total debit |
|---|---:|---:|---:|
| `BANKTRANSACTION` | 83 | 199 | 42,535.70 |
| `INVOICE` | 65 | 204 | 60,145.24 |
| `CREDITNOTE` | 5 | 15 | 1,400.27 |
| `EXPENSECLAIM` | 3 | 11 | 180.35 |
| `MANUALJOURNAL` | 1 | 2 | 4,130.98 |

---

## Addendum — the coordinator's changes after this verification

This document is the verifier's record of the application as it stood when it ran, and its
findings are left exactly as measured. Three of them were fixed immediately afterwards, so
a reader who checks the code today will not see what this document describes. The
coordinator's account is `docs/reports-xero-parity-summary.md` §8.

1. **§1.9, §5.7 and finding 5 — the non-deterministic `Balance` column.** Fixed by adding
   `l.line_id` as the last ordering key in `journalFeed` (`internal/repository/report.go`).
   Eight back-to-back fetches of the report body now hash identically; the envelope's `Id`
   and `DateTimeUTC` still differ per request, which is by design. Regression test:
   `TestHTTP_Reports_GlDetailLineOrderIsStable` in
   `internal/handlers/reports_parity_integration_test.go` — it posts three journals of three
   lines each tied on `(account, date, journal)`, asserts the rendered order against
   `ORDER BY … l.line_id`, and fails 3/3 with the tiebreaker removed.
2. **§1.11 and §8(e) — the false Sales Tax caveat.** Fixed by rewording
   `taxColumnsCaveat` (`internal/handlers/report_render.go`). The sentence quoted in §1.11
   is gone; the report now states the narrower condition it actually tests. The counts in
   §1.11 (33 lines / 74.16, 122 lines with a `tax_type`, 85 of 87 document lines / 4,777.98)
   are unchanged and remain correct.
3. **§3 finding 2 and finding 6 — `/app/reports/bas` 404.** Fixed by a route that answers
   308 to `/app/reports/sales-tax`, rather than by rendering the report twice.

The API on :8080 was rebuilt and restarted from the tree carrying all three, so the running
server now serves the corrected caveat that §1.11 quotes as false.

**Not changed, and still open:** the three endpoints that ignore part of their query string
(§8(a)), the four unsupported claims in `docs/xero-reference/reconciliation.md` §4/§5/§6
(§1.10, §1.16 — the frozen reference was deliberately not edited), and the three parity
decisions the coordinator records as product decisions rather than defects (Trial Balance
column model, Cash Summary sectioning, Sales Tax tax columns).

> **AMENDMENT (2026-09-11 late) — what has closed since.** This list is left as the finding it
> was. Of it, three items have since been closed and one stands: the three query-string
> parameters are read (`asAtParam` for Trial Balance, Executive Summary and Budget Summary;
> `optionalDateParam` for Budget Summary's `fromDate`, pinned by
> `TestHTTP_Reports_ToDateSetsThePeriod`); the Trial Balance column model, the Cash Summary
> sectioning and the Sales Tax tax columns were closed one by one, each recorded in
> `docs/reports-xero-parity-summary.md` §5, §6 and §8; and the four claims in
> `docs/xero-reference/reconciliation.md` remain in the frozen file, recorded rather than
> applied, exactly as this note says. The product decisions are still decisions — they are
> stated in the reports themselves.
