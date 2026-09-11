# Bank balance graph — Xero parity

The graph on each bank-account card was a static inline SVG that stretched a
`viewBox` and never reacted to the pointer. It is now a real component drawn at
its measured width, with the hover tooltip Xero has, fed by a new endpoint that
serves the same series Xero plots.

Reference: `https://go.xero.com/app/!!6Sp3/manage-bank-accounts` (demo company,
read only).

## 1. The endpoint

```
GET /api/v1/statement-lines/balance-series?bankAccountId=<uuid>[&days=31]
  -> {"BalanceSeries":[{"Date":"2026-08-12","Balance":"11146.51"}, ... ]}
```

`internal/router/router.go` next to `/statement-lines/balance`;
`BankStatementHandler.BalanceSeries` (`internal/handlers/bank_statement.go`) over
`BankStatementRepository.BalanceSeries` (`internal/repository/bank_statement.go`).

The series is one point per **calendar day** over a window ending today (31 days
by default), holding the previous day's balance forward over days nothing
posted. The figure is the account's **ledger balance**, not the bank's: Xero's
graph ends on the card's "Balance in Xero" and not on its "Statement balance",
and the two are different numbers on any account with an unworked inbox.

The running total is taken over *every* journal, not only those inside the
window. That is what makes the last point equal `AccountBalance`'s
`LedgerBalance` **by construction**, including when a journal is dated after
today and would otherwise fall outside the window.

### curl

```
$ go build -o /tmp/goxero-parity-bank ./cmd/server && SERVER_PORT=18080 /tmp/goxero-parity-bank
$ TOKEN=$(curl -s -X POST localhost:18080/api/auth/login -H 'Content-Type: application/json' \
    -d '{"email":"admin@demo.local","password":"admin123"}' | jq -r .token)

$ curl -s "localhost:18080/api/v1/statement-lines/balance-series?bankAccountId=34f257ca-4551-5f7d-a9a1-8c7c53e2d46d" \
    -H "Authorization: Bearer $TOKEN" -H "Xero-Tenant-Id: 6823b27b-c48f-4099-bb27-4202a4f496a2"
{
  "BalanceSeries": [
    {"Date": "2026-08-12", "Balance": "11146.51"},
    {"Date": "2026-08-13", "Balance": "11134.51"},
    ... 28 more ...
    {"Date": "2026-09-10", "Balance": "7491.72"},
    {"Date": "2026-09-11", "Balance": "7375.72"}
  ]
}
```

### Last point vs LedgerBalance (account 090)

| | |
|---|---|
| `balance-series` last point (`2026-09-11`) | **7375.72** |
| `statement-lines/balance` `LedgerBalance` | **7375.72** |
| `statement-lines/balance` `StatementBalance` | 12392.72 |

They are equal; the statement balance — which the graph deliberately does *not*
plot — is 5017.00 away from it. Account 091 is the mirror case: a wholly
negative window, last point `-250` = its `LedgerBalance` of `-250`.

The carry-forward matches Xero's own demo figures: 16 Aug and 17 Aug both read
`11049.31`, exactly the "17 Aug repeats 16 Aug's 11,049.31" measured on the
reference page. The window maximum, `11146.51`, is also the value the reference
page plots at `y = 4`.

## 2. The component

`web/src/lib/components/BankBalanceChart.svelte` — the file existed but was dead
code (nothing imported it); it has been replaced wholesale. Geometry lives in
`web/src/lib/bank-balance-chart.ts`, which now keeps only what the graph needs
(the old `runningBalanceSeries` / `signedTxnAmount` / band-path helpers are gone
— nothing imported them once the page stopped using them).

Props: `series` (the endpoint's array), `currency` (the account's own), and
`baseCurrency` (the organisation's, so an amount carries a currency code only
when the card's figures would). It renders nothing at all for a series too short
to draw.

Wired up from `web/src/routes/app/accounting/bank-accounts/+page.svelte`, which
now fetches `statementApi.balanceSeries(id)` per account instead of deriving a
series from a page of statement lines, and no longer imports any chart helper.

## 3. Measured against the reference

Read from the running app in a browser (`document.querySelector` +
`getComputedStyle`), at a chart width of 802px — the same width the reference
page was measured at.

| Spec | Xero | goXero |
|---|---|---|
| plot height / axis band / axis svg | 70 / 30 / 20 | 70 / 30 / 20 |
| svg sizing | real px, no `viewBox` | `width=802 height=70`, `viewBox=null`, `display:block` |
| point x, n=31, W=802 | x0=4, x1=30.4667, x30=798 | 4, 30.4667, 798 |
| y, max balance 11146.51 | 4 | 4 |
| zero line | always y=66 | y=66 |
| axis ticks (days 4/11/18/25) | 106.9333, 294.0667, 481.2, 668.3333 | identical |
| tick labels | 16 Aug / 23 Aug / 30 Aug / 6 Sept, all middle-anchored | identical |
| clips | positive `(0,0,W,67)`, negative `(0,66,W,5)` | identical |
| baseline | `y1=y2=66`, `#a6a9b0`, 1px | identical |
| marker | `r=3`, `#ffffff`, 1px, `geometricPrecision`, per-side colour | identical |
| marker a11y | `role="img" tabindex="0" aria-label="12 August 2026 Balance 11146.51"` | identical |
| line/fill colours | `#0078c8`/`#a6d0ec`, `#dc3246`/`#f3b7be` | identical |
| axis label colour/size | `#404756`, 12px | identical |
| tooltip box | 180×81, `translateX(-50%) translateY(-100%)`, radius 3px, `z-index:1`, `pointer-events:none`, Xero's two-layer shadow | 180×81, identical |
| tooltip heading | `D MMMM YYYY`, 700, 20px line-height, `rgb(0,10,30)`, `margin:12px 16px`, 1px `#ccced2` rule under it | identical (15.2px vs Xero's 15.21px — Xero writes `0.95rem`) |
| tooltip columns | two 50% columns, `8px 16px`, 13px/20px, right column right-aligned, both sign-coloured | identical |
| "Skip graph" | first child of the chart div, `position:absolute; left:-9999px; overflow:hidden`, 40x40 (it carries Xero's standard button chrome: white, 1px `#a6a9b0`, radius 3, 13px, `#0078c8`) | first child, same position and overflow, 1x1 |

Pointer behaviour, exercised in the browser:

- pointer at the middle of the plot selects the middle day and the tooltip box
  lands at `left: <pointX>px; top: <pointY - 10>px`;
- pointer **past the end** of the series (`clientX` 200px beyond the last point)
  still selects the last day — `11 September 2026  Balance  7,375.72`;
- pointer **before** the start selects the first day, and the box overflows to
  the left (`getBoundingClientRect().left` negative) because Xero does not clamp
  it;
- `pointerleave` **removes** the tooltip from the DOM (0 elements);
- focusing a marker (verified: focus lands, `aria-label` reads back) does **not**
  open the tooltip.

Because the chart is sized from `bind:clientWidth`, widening the window from 802
to 1320 re-lays it out in real pixels — `x0=4`, `x1=47.7333`, `x30=1316`, tick 4
at `176` — rather than scaling a fixed viewBox.

## 4. Where this knowingly differs from the reference

1. **A window with no positive balance.** The domain is `[0, max]`, and the zero
   line is pinned at y=66, so if nothing in the window is positive there is no
   extent to divide by. Rather than divide by zero, every point is placed on the
   zero line — which is where Xero's own 5px negative clip band would leave it
   anyway. Account 091 (`-250` throughout) therefore draws as a flat red sliver
   along the baseline. Xero's demo company has no such account to measure
   against; the literal formula is undefined there.
2. **No zero-crossing points are inserted.** A single path is drawn twice, once
   under each clip, in that side's colour. The clips split it at the crossing,
   which is what they are for.
3. **Class names.** Xero's `mf-bank-widget-BalanceGraph` / `--chart-svg` /
   `--axis-svg` are CSS-module hashes with no meaning here; the markup is
   `data-automationid="balance-graph" | "graph" | "bank-tooltip"` and semantic
   Tailwind classes instead. The `role="dialog"` on the tooltip is kept.
4. **The invisible `--domain` gridline path** is reproduced as
   `path.balance-chart--domain` with `stroke:none`, purely for element-for-element
   parity.
5. **`Skip graph`.** Three things about it could not be settled against Xero.
   Its hidden box is 40x40 there against 1x1 here — both are invisible, at the
   same `left:-9999px`, so nothing renders differently; the size is kept at 1x1
   here because Xero's 40x40 comes from a standard-button class this app does
   not have. Its *reveal* styling (border, radius, colours) is likewise
   unmeasurable: no readable stylesheet on the reference page carries a
   `skip-graph:focus` rule — eight of the ten are cross-origin and throw on
   `.cssRules` — and the one thing that could be checked, whether `:focus`
   matches at all, is false for Xero's own button too (`document.hasFocus()` is
   false in the automated tab), so the browser cannot answer it. The reveal
   styling in the component is therefore a choice, not a measurement. And its
   destination could not be observed without activating it on the live page;
   here it moves focus to the date strip below the plot, which is
   `tabindex="-1"`, so Tab resumes after the thirty markers.
6. **Response envelope.** The points are wrapped as `{"BalanceSeries": [...]}`
   rather than returned as a bare array, to match every other list endpoint in
   `internal/router`.
7. **`days` query parameter.** Xero's graph is fixed at a month; the endpoint
   accepts `days` (default 31, clamped to 366) and refuses a non-numeric value
   rather than silently answering a different question.

## 5. Checks run

- `gofmt -l .` — clean; `go vet ./...`, `go build ./...` — clean.
- `go test ./... -count=1` — 12/12 packages ok (the known
  `TestIntegration_GetOrganisation_SeedDemo` short-code failure did not appear).
- `bun run check` — 479 files, 0 errors, 0 warnings.
- `bun run build` — ok.
- New tests: `internal/handlers/bank_balance_series_integration_test.go` covers
  the window's shape (31 consecutive days), the carry-forward, the `days`
  parameter and its rejection, the missing-account case, and — against the
  seeded tenant — that the series ends on `LedgerBalance`.

## 6. One thing left for the coordinator

The shared API on `:8080` is an older binary (`/tmp/goxero-server`, started
19:24) that predates the new route, so the app on `:5173` proxies there and
draws no graph. Everything above was verified against the same worktree's source
on private ports (API `:18080`, vite `:5174`). Rebuilding and restarting the
shared server makes `:5173` work:

```
go build -o /tmp/goxero-server ./cmd/server && kill <pid> && SERVER_PORT=8080 /tmp/goxero-server
```

This was left undone deliberately: the process belongs to another run, and a
restart would pick up every sibling's in-flight Go edits. A question about it
was sent to the coordinator and is still pending (`msg_4541c29483af`).
