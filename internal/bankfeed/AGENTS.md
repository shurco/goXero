# internal/bankfeed — Agent Guide

Abstraction layer for Open Banking aggregators. Today: **Plaid** (US/CA) and
**GoCardless Bank Account Data** (EU/UK). Every aggregator is a `Provider`;
handlers resolve one by slug through `Registry`.

## Contracts

* `Provider` — the only interface handlers depend on. Methods: `Name`,
  `ListInstitutions`, `CreateSession`, `FinalizeSession`, `FetchStatementLines`.
* Seven further capabilities are **optional** and detected by type assertion, so
  an adapter implements only what its API supports: `IncrementalProvider`
  (cursor sync), `PublicTokenExchanger` (consent completed out of band),
  `ReconsentProvider` (repairing a lapsed consent in place), `WebhookVerifier`
  (signed deliveries), `InstitutionPicker` (consent that can start without being
  told which bank), `ConsentDiagnoser` (telling a broken consent from a failed
  call) and `ConsentRevoker` (ending a consent the user disconnected). Never
  widen `Provider` itself for one aggregator's sake.
* `StatementLine.Amount` is **signed** (`+` credit / `-` debit). Adapters must
  normalise to this convention before returning — Plaid reports the opposite
  (positive = money out), so `mapPlaidTx` negates. Getting this wrong silently
  inverts every reconciliation. GoCardless's amount is passed through untouched,
  on the assumption that it already signs a debit negative — an assumption only
  our own fixtures have confirmed, never a live call.
* `StatementLine.ProviderTxID` must be **stable across re-syncs** — it's the
  uniqueness key for `bank_statement_lines`. Fall back to a secondary id when
  the bank omits a primary one; refuse to emit a line that has none (both
  `mapGoCardlessTx` and `mapPlaidTx` do this).
* `StatementLine.CurrencyCode` is **never empty**: the staging column is NOT
  NULL, and Plaid leaves `iso_currency_code` null at some institutions. Fall
  back — `plaidCurrency` ends at `"USD"`.
* `FinalizeSession` returns **`ErrConsentNotFinished`** when the bank has not
  finished the consent yet — the session is still open and the user is one click
  early. That is not a broken feed, so the handler answers it `409` (the status
  the UI keys "still waiting at the bank" off) and leaves the connection as it
  was; any other error is a real failure and marks the row `ERROR`. Both adapters must distinguish the two: Plaid when
  `/link/token/get` reports no completed session, GoCardless when the
  requisition has not reached `LN`/`LINKED`. Wrap the sentinel with `%w` so
  `errors.Is` reaches it.

## Credentials: what a connection hands back

Providers split into two kinds, and `Credential` / `Consent` exist to cover both:

* **Application-level auth** (GoCardless): the adapter holds secret_id/secret_key,
  and the connection is identified by an opaque reference alone.
* **Per-connection auth** (Plaid): consent yields an Item `access_token` that
  every later call must present.

`FinalizeSession(ctx, cred Credential) (*Consent, error)` is where that is
settled. The `Reference` that starts a flow may be throwaway — Plaid's
`link_token` is traded there for a durable `item_id` + `access_token` — so the
handler persists the identity **from the Consent**, not from `CreateSession`.
`Consent.Secret` is empty for providers that need none.

## Incremental sync

`IncrementalProvider` is an optional capability, detected with a type assertion.
Plaid implements it: `/transactions/sync` is cursor-based, and its cursor tracks
an **Item, not an account**, so it cannot be expressed as a per-account date
range. GoCardless does not, and keeps using `FetchStatementLines` over a rolling
window. When a provider syncs a whole connection at once it returns
`ConnStatementLine` values, because there is no per-account call to hang the
account id off.

`SyncConnection`'s `removed` list is not decoration: Plaid replaces a pending
transaction with a posted one under a **new** id, so without it the abandoned
pending row would sit in the inbox forever.

The handler advances the stored cursor only after the lines it covers are
written, so a crash mid-sync replays rather than loses.

## Lines the bank restates or withdraws

`removed` is only half of it: Plaid also sends `modified` transactions, and a
line the bank has changed may already have been **coded** — turned into a bank
transaction, with line items, tax and a contact. By then it is the books, so
`upsertStatementLineSQL` splits the two cases and the split is the whole policy:

* **In the inbox** (`NEW`/`IGNORED`): nothing has been posted, the bank is
  simply right. Amounts, date and payee are rewritten in place, and a withdrawn
  line is deleted outright.
* **Booked** (`IMPORTED`/`PROCESSING`): the money fields are left exactly as
  they were and the bank's version is parked in `upstream_amount` /
  `upstream_posted_at` instead. The disagreement is *derived* on read by
  `upstreamChangeExpr`, so it clears itself the moment the two agree again and
  there is no flag to keep in step. `upstream_removed_at` is the exception —
  a withdrawal cannot be derived from anything, so it is stored, and the user
  dismisses it (`DismissUpstreamRemoval`) once they have seen it.

The descriptive fields (payee, reference, description) are COALESCEd on both
paths: a bank that fills in a field it used to leave blank is improving the
line, not contradicting the coding.

A feed sync that has stored its lines runs the inbox's `AutoReconcileIfEnabled`
for every ledger account that received new ones — after the store, never as part
of it, and failures are logged rather than raised, because a missed match is not
worth losing the lines over.

## Which bank the connection ended up at

`SessionRequest.InstitutionID` is what our own picker sends, and pinning it is
what makes the provider skip its own. Two things can make that pin useless:

* The provider has no picker to skip — a GoCardless requisition names the
  institution up front, so it does not implement `InstitutionPicker` and the
  handler still insists on the field.
* The provider's account refuses pinned institutions outright. A Plaid account
  can be restricted this way, and then *every* `institution_id` comes back
  `INVALID_INSTITUTION` — including the ones its own `/institutions/get` just
  returned — while the identical body without the field is accepted. That is a
  property of the account, not of the body: `institution_id` is confirmed to be
  the right field, since any other spelling earns `UNKNOWN_FIELDS`, and no
  amount of retrying with a different id helps. So `Plaid.CreateSession` retries
  once without the field (`Session.InstitutionPinned` says which branch ran) and
  the user chooses the bank inside Link's own UI.

The second case is why the connection's identity is taken from **`Consent`, not
from the request**: the Item is the only thing that knows which bank was really
connected, and it reports it on `/accounts/get`, the call finalize already makes.
`persistConsent` re-labels the row from it, keeping the name the request supplied
when the id matches — the provider is not always a source of names.

## Completing a consent out of band

`FinalizeSession` reads the result of the session off the bank's API — Plaid's
`GET /link/token/get`, six hours of grace. A provider that instead *pushes* the
result implements `PublicTokenExchanger`, and the handler calls that when the
session is finished by a webhook rather than by the browser. Plaid's
`SESSION_FINISHED` carries `public_tokens`; both routes end in the same
`persistConsent`, so a connection looks identical whichever got there first.

The two are not alternatives. A self-hosted goXero is often unreachable from the
internet, so the redirect is the path that always works, and a webhook is a
bonus: it also arrives when the tab was closed, and it is the only way to hear
about a connection the bank breaks weeks later.

## Disconnecting a bank

Consent has three ways in — the browser, the webhook, a repair — and one way out:
`DELETE /bank-feeds/connections/:id`. The row is what names the provider identity,
so the adapter is told **before** it is deleted, while the credential still
exists: Plaid `/item/remove`, GoCardless `DELETE /requisitions/{id}/`. Two things
make that best-effort rather than part of a transaction:

* A provider that refuses, or cannot be reached, must not stop the user. The row
  goes anyway and the refusal is logged: a disconnect the provider can veto is
  not a disconnect, and blocking it would trap somebody behind an outage.
* A credential that never completed — no durable reference, no secret — has
  nothing to revoke, and `RevokeConsent` answers nil rather than calling the API
  to say so.

Revocation is never retried on the wire. It is not a read, and a second attempt
answers about an Item that is already gone.

## Repairing a consent (Plaid update mode)

`ReconsentProvider` exists because reconnecting a broken feed must not mean
connecting the bank a second time: a new consent creates a second Item for the
same accounts, and their history splits in two. `CreateReconsentSession` opens
the provider's repair flow **against the credential we already hold** (Plaid:
`/link/token/create` with `access_token`, and no `products`), and
`CompleteReconsent` re-reads what came back. Two traps:

* The access token does **not** change, and no public token is exchanged — doing
  so would mint a second Item.
* Plaid checks `country_codes` against the Item being repaired, which is why
  `bank_feed_connections.country` exists: the repair has to name the country the
  user originally searched under.

`BankFeedConnection.SessionRef` is the session currently in flight. It cannot be
`ExternalReference`: that starts as the session handle too, but consent
overwrites it with the durable id, and a webhook arrives knowing only the
session. That is what makes `session_ref` the key a completion is looked up by
(`ConnectionBySessionRef`) — and, while a session is open, also the reference the
provider is asked about, since a first-time finalize names the link token it was
started with. What tells a first-time link from a repair is not the session at
all but whether the connection already holds a durable consent: see
`hasDurableConsent`, `external_reference` + `external_secret` together.

## Verifying webhooks

`WebhookVerifier` is how an unauthenticated endpoint earns the right to change
data. Plaid's `Plaid-Verification` header is an ES256 JWS: the key comes from
`POST /webhook_verification_key/get` by `kid` (cached, bounded, and validated to
be a point on P-256 before use — the cache remembers refusals too, which is what
stops an invented `kid` costing a round trip per request), the signature covers
`header.payload`, and the
claims carry `request_body_sha256` — a hex SHA-256 of the **raw** body, compared
in constant time. The body must therefore be read before anything parses it, and
`iat` older than five minutes is refused so a captured delivery cannot be
replayed indefinitely.

The cache bounds one invented `kid` repeated; a stream of *different* invented
ids is a fresh miss every time, so the lookups themselves are also rate-limited
(`spendKeyLookup`) — a budget spent only on misses, so a delivery naming a key we
already hold is never delayed by it. A spent budget returns
`ErrWebhookVerifyThrottled`, which the handler answers 503, never 401: refusing to
check is not the same as finding the signature wrong, and Plaid retries anything
that is not a 200 either way.

## Adding a provider

1. Implement `Provider` in `internal/bankfeed/<name>.go`.
2. Add a `Credentials()` helper that returns `true` only when the minimum env
   secrets are set — `router.Register` uses it to skip unconfigured adapters. If
   the provider stores a per-connection secret, it additionally needs the
   `SecretBox`, and must stay unregistered without one.
3. Register the slug as a `Provider*` constant at the top of `provider.go` so
   it can be referenced from handlers/tests without typos.
4. Add a table-driven unit test that feeds sample API payloads through your
   mapper (see `gocardless_test.go`, `plaid_test.go`), plus an `httptest`-backed
   test for the multi-call flows.

## Live tests

`plaid_live_test.go` talks to the real sandbox and is skipped unless both
`PLAID_LIVE=1` and credentials are set:

```
PLAID_LIVE=1 PLAID_CLIENT_ID=... PLAID_SECRET=... \
  go test ./internal/bankfeed/ -run TestPlaidLive -v -timeout 400s
```

It earns its keep because a stub answers whatever it is asked. The request *body*
is the part of an adapter that no fixture can check, and it is where one of these
actually broke: `/institutions/get` and `/institutions/search` want `products` in
different places, and Plaid answers a misplaced one with `UNKNOWN_FIELDS` rather
than a hint. Every adapter should have this pair of tests — one for reading the
API, one for being read by it.

## No secrets on disk

`bank_feed_connections.external_secret` holds the **AES-GCM ciphertext** of any
per-connection secret, sealed by `SecretBox` under `BANKFEED_ENCRYPTION_KEY`
and never written to a log in plaintext. The plaintext is opened per call in
`BankFeedHandler.credentialFor` and does not outlive the request. Rotating that
key makes existing tokens undecryptable by design — affected connections have to
be reconnected.

`models.BankFeedConnection.ExternalSecret` and `.SyncCursor` are tagged
`json:"-"`: they must never appear in an API response.

## HTTP hygiene

* Every adapter owns its own `*http.Client` with a 30s timeout. Override it from
  tests through an option (`WithGoCardlessHTTPClient`, `WithPlaidHTTPClient`).
* Limit response bodies via `io.LimitReader` to cap memory (5 MiB default).
  That limits what is *read*, not what an error message carries: a body rendered
  into an error goes through `errBody`, because it is persisted to
  `last_error` and logged.
* Never log raw secrets — the GoCardless token exchange lives in `ensureToken`
  and keeps the access token in memory only.
* Plaid has no bearer token and no session: `client_id`/`secret` ride in the
  body of every POST, so `Plaid.do` is the single place they are attached.
* A call that fails **on the wire** is sent again once — but only where repeating
  it cannot change anything (`retryablePlaidCall`): the reads and the cursor
  sync, never the token exchange, which is what creates the Item. The failure
  this exists for is the host's rather than Plaid's: a content filter or proxy
  that holds a flow while it decides surfaces as a TLS handshake timeout with TCP
  perfectly healthy, and the identical request succeeds a moment later. An API
  refusal is an answer and is never retried, and a caller who cancels stops the
  wait immediately.

## Plaid specifics

* Consent runs through **Hosted Link**: `/link/token/create` with a `hosted_link`
  object returns `hosted_link_url`, and goXero ships no JavaScript SDK. The
  `public_token` comes back via `GET /link/token/get` — the documented path for
  integrations without webhooks, and the fallback when there is no public URL to
  receive `SESSION_FINISHED` at. Plaid keeps completed session data for **six
  hours**, so finalize must run promptly; the callback page does it on load.
* `WithPlaidWebhookURL` registers the URL Plaid posts deliveries to and is
  optional: without it the adapter is exactly as capable, just deaf.
* Most large US banks (Chase, BofA, …) authenticate over OAuth. That works here
  without extra setup: for desktop-web integrations Plaid drives the OAuth
  handoff itself and opens the bank's site in a new window, so no `redirect_uri`
  has to be registered. Allowlisting one is the optional hardening path.
* Institutions cannot be enumerated (~12k), so a non-empty `q` goes to
  `/institutions/search`; an empty one returns the first 500 of the catalogue.
* Only `depository` and `credit` accounts are staged — investment accounts need
  a different Plaid product, and a mortgage is not reconciled.
