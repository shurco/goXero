# internal/bankfeed — Agent Guide

Abstraction layer for Open Banking aggregators (GoCardless BAD, Plaid,
TrueLayer, Salt Edge, …). Every aggregator is a `Provider`; handlers resolve
one by slug through `Registry`.

## Contracts

* `Provider` — the only interface handlers depend on. Methods: `Name`,
  `ListInstitutions`, `CreateSession`, `FinalizeSession`, `FetchStatementLines`.
* `StatementLine.Amount` is **signed** (`+` credit / `-` debit). Adapters must
  normalise to this convention before returning.
* `StatementLine.ProviderTxID` must be **stable across re-syncs** — it's the
  uniqueness key for `bank_feed_statement_lines`. Fall back to
  `internalTransactionId` / `endToEndId` when the bank omits a primary id;
  refuse to emit a line that has none (`MapGoCardlessTx` does this).

## Adding a provider

1. Implement `Provider` in `internal/bankfeed/<name>.go`.
2. Add a `Credentials()` helper that returns `true` only when the minimum env
   secrets are set — `router.Register` uses it to skip unconfigured adapters.
3. Register the slug as a `Provider*` constant at the top of `provider.go` so
   it can be referenced from handlers/tests without typos.
4. Add a table-driven unit test that feeds sample API payloads through your
   mapper (see `gocardless_test.go`).

## No secrets on disk

We never persist raw bank credentials. The DB stores only the opaque reference
the provider hands back after consent (`bank_feed_connections.external_reference`,
e.g. GoCardless `requisition_id`, Plaid `item_id`). If an adapter needs OAuth
refresh tokens, encrypt them with `BANKFEED_ENCRYPTION_KEY` (not yet wired —
add a dedicated column + AES-GCM wrapper when needed).

## HTTP hygiene

* Every adapter owns its own `*http.Client` with a 30s timeout. Override it
  from tests through an option (`WithGoCardlessHTTPClient`).
* Limit response bodies via `io.LimitReader` to cap memory (5 MiB default).
* Never log raw secrets — the token exchange lives in `ensureToken` and keeps
  the access token in memory only.
