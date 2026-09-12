package handlers_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/bankfeed"
	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/handlers"
	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
	"github.com/shurco/goxero/internal/testutil"
)

// plaidStub is a fake Plaid API whose replies a test can change between calls,
// which is what a second sync needs: the same connection, answering differently.
type plaidStub struct {
	createBody   map[string]any   // the last /link/token/create body we were sent
	createBodies []map[string]any // every one of them, in order — a refused institution is sent twice
	syncBody     string           // the JSON /transactions/sync should answer with
	syncStatus   int              // and the status to answer it with; 0 means 200
	accounts     string
	keyJSON      string
	// refusePinned makes the stub answer INVALID_INSTITUTION to a link token
	// that names an institution, which is what a restricted Plaid account does.
	refusePinned bool
	// sessionIncomplete makes /link/token/get answer a session the user has not
	// finished: Plaid knows the link token, it just has no public token to give.
	sessionIncomplete bool
	// revoked collects the access tokens we asked Plaid to delete Items for, and
	// refuseRemove makes that call fail — a provider that will not be told.
	revoked      []string
	refuseRemove bool
}

func newPlaidStub(t *testing.T) *plaidStub {
	t.Helper()
	return &plaidStub{
		// The Item names its institution the way a real /accounts/get does, so
		// the handler can re-label the connection from it.
		accounts: `{"accounts":[{"account_id":"acc-chk","name":"Plaid Checking",` +
			`"type":"depository","subtype":"checking","balances":{"current":110.00,"iso_currency_code":"USD"}}],` +
			`"item":{"institution_id":"ins_56","institution_name":"Chase"}}`,
	}
}

func (p *plaidStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This runs on the server's own goroutine, so failures are reported with
		// assert rather than require: require would call FailNow off the test
		// goroutine, which the testing package does not allow.
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		var body map[string]any
		if len(raw) > 0 {
			assert.NoError(t, json.Unmarshal(raw, &body))
		}
		answer := func(payload string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
		}
		switch r.URL.Path {
		case "/link/token/create":
			p.createBody = body
			p.createBodies = append(p.createBodies, body)
			if p.refusePinned && body["institution_id"] != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				answer(`{"error_type":"INVALID_INPUT","error_code":"INVALID_INSTITUTION",` +
					`"error_message":"invalid institution_id provided"}`)
				return
			}
			answer(`{"link_token":"link-sandbox-1","hosted_link_url":"https://secure.plaid.com/hl/abc"}`)
		case "/link/token/get":
			// Plaid looks the session up by the link token it issued, so a request
			// that does not carry it is refused rather than answered — which is what
			// makes the reference the handler sends observable here.
			if body["link_token"] != "link-sandbox-1" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				answer(`{"error_type":"INVALID_INPUT","error_code":"INVALID_FIELD",` +
					`"error_message":"link_token must be a non-empty string"}`)
				return
			}
			if p.sessionIncomplete {
				answer(`{"link_sessions":[{"results":{"item_add_results":[]}}]}`)
				return
			}
			answer(`{"link_sessions":[{"results":{"item_add_results":[{"public_token":"public-sandbox-9"}]}}]}`)
		case "/item/public_token/exchange":
			answer(`{"access_token":"access-sandbox-7","item_id":"item-42"}`)
		case "/accounts/get":
			answer(p.accounts)
		case "/item/remove":
			if p.refuseRemove {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				answer(`{"error_type":"INVALID_INPUT","error_code":"INVALID_ACCESS_TOKEN",` +
					`"error_message":"provided access token is not valid"}`)
				return
			}
			token, _ := body["access_token"].(string)
			p.revoked = append(p.revoked, token)
			answer(`{"request_id":"req-1"}`)
		case "/transactions/sync":
			if p.syncStatus >= 400 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(p.syncStatus)
			}
			answer(p.syncBody)
		case "/webhook_verification_key/get":
			answer(p.keyJSON)
		default:
			http.NotFound(w, r)
		}
	}))
}

// bankFeedApp mounts the bank feed routes over the real database. Both harnesses
// go through here — the stubbed one and the one that talks to Plaid itself — so
// the two differ only in which provider is behind the handler, never in what the
// API in front of it looks like.
func bankFeedApp(repos *repository.Repositories, cfg *config.Config, feed *handlers.BankFeedHandler) *fiber.App {
	statement := handlers.NewBankStatementHandler(repos)
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if fe, ok := err.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		},
	})
	app.Post("/api/v1/bank-feeds/webhooks/plaid", feed.PlaidWebhook)
	apiV1 := app.Group("/api/v1", middleware.JWTAuth(cfg.Auth), middleware.Tenant(cfg.Auth, repos))
	apiV1.Get("/bank-feeds/providers", feed.ListProviders)
	apiV1.Post("/bank-feeds/connections", feed.CreateConnection)
	apiV1.Get("/bank-feeds/connections", feed.ListConnections)
	apiV1.Post("/bank-feeds/connections/:id/finalize", feed.FinalizeConnection)
	apiV1.Post("/bank-feeds/connections/:id/sync", feed.SyncConnection)
	apiV1.Post("/bank-feeds/connections/:id/reconnect", feed.ReconnectConnection)
	apiV1.Delete("/bank-feeds/connections/:id", feed.DeleteConnection)
	apiV1.Post("/statement-lines/:id/dismiss-upstream-change", statement.DismissUpstreamChange)
	apiV1.Get("/statement-lines", statement.ListStatementLines)
	apiV1.Post("/bank-transactions", handlers.NewBankTransactionHandler(repos).Create)
	return app
}

// bankFeedHarness is the shared appHarness pointed at a bank feed app wired to a
// fake Plaid. The shared app deliberately has no Plaid credentials — the test
// config carries none — so the feed routes get an app of their own over the same
// database, which also lets a test shape Plaid's replies.
func bankFeedHarness(t *testing.T) (*appHarness, *plaidStub, *ecdsa.PrivateKey) {
	t.Helper()
	return bankFeedHarnessWith(t)
}

// bankFeedHarnessWith mounts the same app with further providers registered, so a
// test can hold up an adapter that lacks a capability Plaid has — the control
// case for an optional interface.
func bankFeedHarnessWith(t *testing.T, extra ...bankfeed.Provider) (*appHarness, *plaidStub, *ecdsa.PrivateKey) {
	t.Helper()
	pool := testutil.NewPool(t)
	repos := repository.New(pool)
	cfg := testCfg()

	stub := newPlaidStub(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	stub.keyJSON = testutil.PlaidJWK(t, &key.PublicKey, "kid-1")

	srv := stub.server(t)
	t.Cleanup(srv.Close)

	registry := bankfeed.NewRegistry()
	registry.Register(bankfeed.NewPlaid("client-id", "secret", "goxero", false,
		bankfeed.WithPlaidBaseURL(srv.URL), bankfeed.WithPlaidHTTPClient(srv.Client()),
		bankfeed.WithPlaidWebhookURL("https://goxero.example/api/v1/bank-feeds/webhooks/plaid")))
	for _, p := range extra {
		registry.Register(p)
	}
	secrets, err := bankfeed.NewSecretBox("test-passphrase")
	require.NoError(t, err)

	feed := handlers.NewBankFeedHandler(repos, registry, secrets, "https://app.example/callback", 0)
	// Exactly what the router wires, so the sync path under test is the real one.
	feed.SetAutoReconciler(handlers.NewBankStatementHandler(repos))

	tok, err := middleware.IssueToken(cfg.Auth, seedDemoUserID, "demo@example.com")
	require.NoError(t, err)

	return &appHarness{app: bankFeedApp(repos, cfg, feed), cfg: cfg, repos: repos, token: tok}, stub, key
}

// doRaw posts an exact byte body with extra headers. The webhook path needs
// this: Plaid signs a hash of the body, so a request the test did not build
// byte for byte cannot be verified.
func (h *appHarness) doRaw(t *testing.T, method, path string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, data
}

// signWebhookKid signs a delivery the way Plaid does and names the key id in the
// header. The id matters to the adapter even though one key signs every delivery
// here: the id is what it looks the key up by.
func signWebhookKid(t *testing.T, key *ecdsa.PrivateKey, body []byte, kid string) string {
	t.Helper()
	return testutil.SignPlaidWebhook(t, key, kid, body, time.Now(), "ES256")
}

// signWebhook signs a delivery under the key id the stub publishes.
func signWebhook(t *testing.T, key *ecdsa.PrivateKey, body []byte) string {
	t.Helper()
	return signWebhookKid(t, key, body, "kid-1")
}

// postWebhook signs a delivery and posts it, which is what Plaid does.
func postWebhook(t *testing.T, h *appHarness, key *ecdsa.PrivateKey, payload map[string]any) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return h.doRaw(t, http.MethodPost, "/api/v1/bank-feeds/webhooks/plaid", body,
		map[string]string{"Plaid-Verification": signWebhook(t, key, body)})
}

// seedPlaidConnection stages a connection the way a finished consent leaves it,
// and returns it with its feed account.
func seedPlaidConnection(t *testing.T, h *appHarness, ledgerAccountID *uuid.UUID) (*models.BankFeedConnection, *models.BankFeedAccount) {
	t.Helper()
	ctx := context.Background()
	conn := &models.BankFeedConnection{
		Provider:        bankfeed.ProviderPlaid,
		Status:          models.BankFeedStatusPending,
		InstitutionID:   "ins_56",
		InstitutionName: "Chase",
		Country:         "US",
	}
	require.NoError(t, h.repos.BankFeeds.CreateConnection(ctx, seedDemoOrgID, conn))
	require.NoError(t, h.repos.BankFeeds.SetConnectionSession(ctx, seedDemoOrgID, conn.ConnectionID,
		"link-sandbox-1", "https://secure.plaid.com/hl/abc"))

	fa := &models.BankFeedAccount{
		ConnectionID:      conn.ConnectionID,
		ExternalAccountID: "acc-chk",
		DisplayName:       "Plaid Checking",
		CurrencyCode:      "USD",
	}
	require.NoError(t, h.repos.BankFeeds.UpsertAccount(ctx, seedDemoOrgID, fa))
	if ledgerAccountID != nil {
		require.NoError(t, h.repos.BankFeeds.BindAccount(ctx, seedDemoOrgID, fa.FeedAccountID, ledgerAccountID))
	}
	return conn, fa
}

// linkConnection gives a connection the durable identity consent produces.
func linkConnection(t *testing.T, h *appHarness, connID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	sealed, err := bankfeed.NewSecretBox("test-passphrase")
	require.NoError(t, err)
	blob, err := sealed.Seal("access-sandbox-7")
	require.NoError(t, err)
	require.NoError(t, h.repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, connID, "item-42", blob))
	require.NoError(t, h.repos.BankFeeds.UpdateConnectionStatus(ctx, seedDemoOrgID, connID,
		models.BankFeedStatusLinked, "", nil))
}

// TestHTTP_BankFeed_WebhookCompletesConsent is the point of the webhook: the
// consent is finished by Plaid telling us, without waiting for the browser to
// come back — and a delivery that is not signed by Plaid changes nothing.
func TestHTTP_BankFeed_WebhookCompletesConsent(t *testing.T) {
	h, _, key := bankFeedHarness(t)
	conn, _ := seedPlaidConnection(t, h, nil)

	// A stranger's delivery is refused before anything is read out of it.
	tampered, err := json.Marshal(map[string]any{
		"webhook_type": "LINK", "webhook_code": "SESSION_FINISHED", "link_token": "link-sandbox-1",
		"public_tokens": []string{"public-sandbox-9"},
	})
	require.NoError(t, err)
	status, _ := h.doRaw(t, http.MethodPost, "/api/v1/bank-feeds/webhooks/plaid", tampered,
		map[string]string{"Plaid-Verification": signWebhook(t, key, []byte(`{"other":"body"}`))})
	require.Equal(t, http.StatusUnauthorized, status)

	status, _ = h.doRaw(t, http.MethodPost, "/api/v1/bank-feeds/webhooks/plaid", tampered, nil)
	require.Equal(t, http.StatusUnauthorized, status, "no signature header at all")

	got, err := h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	require.Equal(t, models.BankFeedStatusPending, got.Status)

	// The real thing.
	status, body := postWebhook(t, h, key, map[string]any{
		"webhook_type": "LINK", "webhook_code": "SESSION_FINISHED",
		"link_token": "link-sandbox-1", "public_tokens": []string{"public-sandbox-9"},
	})
	require.Equal(t, http.StatusOK, status, string(body))

	got, err = h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusLinked, got.Status)
	assert.Equal(t, "item-42", got.ExternalReference, "the throwaway session became the durable Item")
	assert.Empty(t, got.SessionRef)
	require.NotEmpty(t, got.ExternalSecret)
	assert.NotContains(t, string(got.ExternalSecret), "access-sandbox-7", "the token is stored sealed")
	require.Len(t, got.Accounts, 1)
	assert.Equal(t, "acc-chk", got.Accounts[0].ExternalAccountID)

	// The same delivery again — browsers and webhooks both arrive, and Plaid
	// retries — must leave everything exactly as it was.
	status, body = postWebhook(t, h, key, map[string]any{
		"webhook_type": "LINK", "webhook_code": "SESSION_FINISHED",
		"link_token": "link-sandbox-1", "public_tokens": []string{"public-sandbox-9"},
	})
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), `"Ignored":true`, "there is no session in flight any more")
	got, err = h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, "item-42", got.ExternalReference)
	require.Len(t, got.Accounts, 1, "no duplicate account from the replay")
}

// TestHTTP_BankFeed_WebhookThrottledKeyLookupsAreNotRejections — having refused
// to check a delivery is not the same as having found its signature wrong. Plaid
// retries anything that is not a 200, so nothing is lost either way; what the
// distinction is for is that 401 would record a verdict we never reached and send
// whoever reads the log hunting for a signature problem that does not exist.
func TestHTTP_BankFeed_WebhookThrottledKeyLookupsAreNotRejections(t *testing.T) {
	h, _, key := bankFeedHarness(t)
	// A delivery this integration does not act on, so the flood below costs the
	// route nothing but the key lookup it is meant to spend.
	ignored := []byte(`{"webhook_type":"LATER","webhook_code":"SOMETHING_NEW"}`)
	post := func(kid string) int {
		t.Helper()
		status, _ := h.doRaw(t, http.MethodPost, "/api/v1/bank-feeds/webhooks/plaid", ignored,
			map[string]string{"Plaid-Verification": signWebhookKid(t, key, ignored, kid)})
		return status
	}

	require.Equal(t, http.StatusOK, post("kid-1"), "to begin with a delivery is answered on its merits")

	// A stream of ids we have never seen spends the adapter's budget for looking
	// key ids up. The count is far above that budget on purpose — what is being
	// pinned is that the budget does end, and what the route says when it does.
	status := http.StatusOK
	for i := 0; i < 60 && status != http.StatusServiceUnavailable; i++ {
		status = post(fmt.Sprintf("kid-flood-%d", i))
	}
	assert.Equal(t, http.StatusServiceUnavailable, status,
		"a spent lookup budget is 'try again', not a bad signature")
}

// TestHTTP_BankFeed_WebhookReportsBrokenConsent — an ITEM webhook is how we find
// out a feed has stopped working while nobody was looking at the connections
// screen. It is also what makes the Reconnect button appear.
func TestHTTP_BankFeed_WebhookReportsBrokenConsent(t *testing.T) {
	h, _, key := bankFeedHarness(t)
	conn, _ := seedPlaidConnection(t, h, nil)
	linkConnection(t, h, conn.ConnectionID)
	ctx := context.Background()

	status, body := postWebhook(t, h, key, map[string]any{
		"webhook_type": "ITEM", "webhook_code": "ERROR", "item_id": "item-42",
		"error": map[string]any{"error_code": "ITEM_LOGIN_REQUIRED", "error_message": "the login details changed"},
	})
	require.Equal(t, http.StatusOK, status, string(body))

	got, err := h.repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusError, got.Status)
	assert.Contains(t, got.LastError, "ITEM_LOGIN_REQUIRED")

	status, body = postWebhook(t, h, key, map[string]any{
		"webhook_type": "ITEM", "webhook_code": "USER_PERMISSION_REVOKED", "item_id": "item-42",
	})
	require.Equal(t, http.StatusOK, status, string(body))
	got, err = h.repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusRevoked, got.Status)

	// An Item that is not ours is acknowledged and dropped, not an error.
	status, body = postWebhook(t, h, key, map[string]any{
		"webhook_type": "ITEM", "webhook_code": "ERROR", "item_id": "item-somebody-else",
	})
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), `"Ignored":true`)

	// Nor is a webhook this integration does not model.
	status, body = postWebhook(t, h, key, map[string]any{
		"webhook_type": "IDENTITY", "webhook_code": "SOMETHING_NEW", "item_id": "item-42",
	})
	require.Equal(t, http.StatusOK, status, string(body))
}

// TestHTTP_BankFeed_ReconnectRepairsInPlace — a connection the bank has broken
// is repaired on the connection itself. A second consent would import the same
// accounts again under a new connection and split their history in two.
func TestHTTP_BankFeed_ReconnectRepairsInPlace(t *testing.T) {
	h, stub, key := bankFeedHarness(t)
	conn, _ := seedPlaidConnection(t, h, nil)
	linkConnection(t, h, conn.ConnectionID)
	require.NoError(t, h.repos.BankFeeds.FailLinkedConnection(context.Background(), seedDemoOrgID,
		conn.ConnectionID, "ITEM_LOGIN_REQUIRED: the login details changed"))

	status, body := h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/reconnect", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Equal(t, "access-sandbox-7", stub.createBody["access_token"],
		"update mode is opened against the Item we already hold")
	assert.NotContains(t, stub.createBody, "products", "update mode asks for no products")

	var got struct {
		Connections []struct {
			ConnectionID string `json:"ConnectionID"`
			AuthURL      string `json:"AuthURL"`
		} `json:"Connections"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Connections, 1)
	assert.Equal(t, conn.ConnectionID.String(), got.Connections[0].ConnectionID, "the same connection is repaired")
	assert.Equal(t, "https://secure.plaid.com/hl/abc", got.Connections[0].AuthURL)

	// The user finishes the repair, and Plaid says so. There is no public token
	// to exchange: the Item and its credential are the ones we already have.
	status, body = postWebhook(t, h, key, map[string]any{
		"webhook_type": "LINK", "webhook_code": "SESSION_FINISHED",
		"link_token": "link-sandbox-1", "public_tokens": []string{"public-sandbox-9"},
	})
	require.Equal(t, http.StatusOK, status, string(body))

	refreshed, err := h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusLinked, refreshed.Status)
	assert.Equal(t, "item-42", refreshed.ExternalReference)
	assert.Empty(t, refreshed.LastError)
	require.Len(t, refreshed.Accounts, 1)

	list, err := h.repos.BankFeeds.ListConnections(context.Background(), seedDemoOrgID)
	require.NoError(t, err)
	assert.Len(t, list, 1, "a repair must not leave a second connection behind")
}

// TestHTTP_BankFeed_FinalizeBeforeConsentIsNotAFailure — "I've finished" clicked
// before the user has approved access at the bank. The session is still open, so
// the answer is a 409 whose message says so, and the connection keeps the state it
// had: recording a failure here would turn one click too early into a feed that
// looks broken, and a broken one is offered a repair instead of the consent link.
func TestHTTP_BankFeed_FinalizeBeforeConsentIsNotAFailure(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	conn, _ := seedPlaidConnection(t, h, nil)
	stub.sessionIncomplete = true

	status, body := h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusConflict, status, string(body))
	assert.Contains(t, string(body), "has not finished this consent")

	refreshed, err := h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusPending, refreshed.Status)
	assert.Empty(t, refreshed.LastError)

	// The user then really finishes, and the same call links the connection: the
	// refused attempt left nothing behind to get in the way.
	stub.sessionIncomplete = false
	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	refreshed, err = h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusLinked, refreshed.Status)
	assert.Equal(t, "item-42", refreshed.ExternalReference)
}

// TestHTTP_BankFeed_FinalizeProvisionsTheLedgerAccount — a connection on its own
// leaves the bank accounts screen — which lists ledger accounts, not feed
// accounts — with nothing to show, so finishing a consent has to leave a bank
// account behind.
func TestHTTP_BankFeed_FinalizeProvisionsTheLedgerAccount(t *testing.T) {
	h, _, _ := bankFeedHarness(t)
	conn, _ := seedPlaidConnection(t, h, nil)

	status, body := h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	refreshed, err := h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	require.Len(t, refreshed.Accounts, 1)
	require.NotNil(t, refreshed.Accounts[0].AccountID,
		"the granted account is bound to a ledger account, not left for someone to bind by hand")

	acct, err := h.repos.Accounts.GetByID(context.Background(), seedDemoOrgID, *refreshed.Accounts[0].AccountID)
	require.NoError(t, err)
	assert.Equal(t, models.AccountTypeBank, acct.Type)
	assert.Equal(t, "Plaid Checking", acct.Name)
	assert.Equal(t, "092", acct.Code)

	// And it is on the list the bank accounts screen reads.
	listed, err := h.repos.Accounts.List(context.Background(), seedDemoOrgID,
		repository.AccountFilter{Type: models.AccountTypeBank})
	require.NoError(t, err)
	names := make([]string, 0, len(listed))
	for _, a := range listed {
		names = append(names, a.Name)
	}
	assert.Contains(t, names, "Plaid Checking")
}

// TestHTTP_BankFeed_SyncProvisionsAnAccountForAnOlderConnection — a connection
// linked before provisioning existed, or one the bank granted another account
// to later, still has to end up on the bank accounts screen. Syncing is where
// that happens, and it happens before the pull so the lines of this same run
// are filed against it.
func TestHTTP_BankFeed_SyncProvisionsAnAccountForAnOlderConnection(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	stub.syncBody = `{"added":[],"modified":[],"removed":[],"next_cursor":"cursor-1","has_more":false}`

	conn, _ := seedPlaidConnection(t, h, nil)
	linkConnection(t, h, conn.ConnectionID) // a finished consent, nothing bound

	status, body := h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/sync", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	refreshed, err := h.repos.BankFeeds.GetConnection(context.Background(), seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	require.Len(t, refreshed.Accounts, 1)
	require.NotNil(t, refreshed.Accounts[0].AccountID)

	acct, err := h.repos.Accounts.GetByID(context.Background(), seedDemoOrgID, *refreshed.Accounts[0].AccountID)
	require.NoError(t, err)
	assert.Equal(t, models.AccountTypeBank, acct.Type)
	assert.Equal(t, "Plaid Checking", acct.Name)
}

// TestHTTP_BankFeed_SyncReconcilesAndFlagsWhatTheBankWithdrew walks the two
// halves of an incremental sync that touch booked data: a line that agrees with
// a transaction already on the account is reconciled as it lands, and a line the
// bank later withdraws is kept and flagged rather than deleted from the books.
func TestHTTP_BankFeed_SyncReconcilesAndFlagsWhatTheBankWithdrew(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	account := newStatementAccount(t, h)
	conn, _ := seedPlaidConnection(t, h, &account.AccountID)
	linkConnection(t, h, conn.ConnectionID)
	require.NoError(t, h.repos.Accounts.SetAutoReconcile(context.Background(), seedDemoOrgID, account.AccountID, true))

	// A payment the bookkeeper entered by hand: same day, same amount, opposite
	// direction to nothing — this is the line the feed is about to bring in.
	createBankTransaction(t, h, account.AccountID.String(), "RECEIVE", "2026-04-02T00:00:00Z", 250.00,
		firstAccountCode(t, h, account.Code))

	stub.syncBody = `{
		"added":[{"transaction_id":"tx-2","account_id":"acc-chk","amount":-250.00,
		          "iso_currency_code":"USD","date":"2026-04-02","name":"ACME PAYROLL"}],
		"modified":[],"removed":[],"next_cursor":"cursor-1","has_more":false
	}`
	status, body := h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/sync", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var synced struct {
		Fetched, NewLines, AutoMatched, UpstreamChanges int
	}
	require.NoError(t, json.Unmarshal(body, &synced))
	assert.Equal(t, 1, synced.Fetched)
	assert.Equal(t, 1, synced.NewLines)
	assert.Equal(t, 1, synced.AutoMatched, "the setting on the account is what does the matching")
	assert.Zero(t, synced.UpstreamChanges)

	lines := listLines(t, h, "?bankAccountId="+account.AccountID.String()+"&unreconciled=false")
	require.Len(t, lines, 1)
	require.Equal(t, "IMPORTED", lines[0].Status)
	require.NotEmpty(t, lines[0].BankTransactionID, "it was matched to the transaction on the account")

	// The bank withdraws the transaction it just posted — a pending charge that
	// settled under this id and was then reversed, say. The line has been coded,
	// so it stays exactly as it is and the difference is reported instead.
	stub.syncBody = `{"added":[],"modified":[],"removed":[{"transaction_id":"tx-2"}],
	                  "next_cursor":"cursor-2","has_more":false}`
	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/sync", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &synced))
	assert.Equal(t, 1, synced.UpstreamChanges, "the sync reports what needs a human")

	lines = listLines(t, h, "?bankAccountId="+account.AccountID.String()+"&unreconciled=false")
	require.Len(t, lines, 1, "a coded line is not deleted out from under the ledger")
	assert.Equal(t, "REMOVED", lines[0].UpstreamChange)
	require.NotNil(t, lines[0].UpstreamRemovedAt)

	// The user says they know — the posted version arrived as its own line and
	// their entry is the right one — and the notice goes.
	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-lines/"+lines[0].StatementLineID+"/dismiss-upstream-change", nil, true)
	require.Equal(t, http.StatusNoContent, status, string(body))
	lines = listLines(t, h, "?bankAccountId="+account.AccountID.String()+"&unreconciled=false")
	require.Len(t, lines, 1)
	assert.Empty(t, lines[0].UpstreamChange)
}

// TestHTTP_BankFeed_SyncFailureKeepsAWorkingFeedRunning — a sync can fail without
// the consent being broken, and the two must not be confused. A rate limit or an
// outage is one bad sync: the feed stays LINKED, keeps its place in the
// background sweep, and carries the reason in last_error. Only an error the
// provider blames on the credential takes the connection out of service, which is
// what puts Reconnect in front of the user.
func TestHTTP_BankFeed_SyncFailureKeepsAWorkingFeedRunning(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	ctx := context.Background()
	conn, _ := seedPlaidConnection(t, h, nil)
	linkConnection(t, h, conn.ConnectionID)

	// A failure that says nothing about the consent.
	stub.syncStatus = http.StatusTooManyRequests
	stub.syncBody = `{"error_type":"RATE_LIMIT_EXCEEDED","error_code":"RATE_LIMIT_EXCEEDED",` +
		`"error_message":"too many requests"}`

	status, body := h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/sync", nil, true)
	require.Equal(t, http.StatusInternalServerError, status, string(body))

	got, err := h.repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusLinked, got.Status,
		"a rate limit must not cost the user a repair")
	assert.Contains(t, got.LastError, "RATE_LIMIT_EXCEEDED", "but the reason is not lost")

	// The bank says the Item itself no longer works. Now it is the user's move.
	stub.syncStatus = http.StatusBadRequest
	stub.syncBody = `{"error_type":"ITEM_ERROR","error_code":"ITEM_LOGIN_REQUIRED",` +
		`"error_message":"the login details changed"}`

	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/sync", nil, true)
	require.Equal(t, http.StatusInternalServerError, status, string(body))

	got, err = h.repos.BankFeeds.GetConnection(ctx, seedDemoOrgID, conn.ConnectionID)
	require.NoError(t, err)
	assert.Equal(t, models.BankFeedStatusError, got.Status)
	assert.Contains(t, got.LastError, "ITEM_LOGIN_REQUIRED")
	assert.Equal(t, "item-42", got.ExternalReference, "the consent is kept for the repair to reuse")
}

// TestHTTP_BankFeed_WebhookRouteIsPublic — the webhook is the one bank feed
// route a logged-in user is not the caller of. It has to be reachable without a
// session (Plaid sends no token) while still being mounted on the real router,
// which is the thing this asserts: an unauthenticated POST gets as far as the
// handler, and is refused there for the reason the handler cares about.
func TestHTTP_BankFeed_WebhookRouteIsPublic(t *testing.T) {
	h := newHarness(t)
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/webhooks/plaid",
		map[string]any{"webhook_type": "ITEM", "webhook_code": "ERROR"}, false)
	// 404, not 401: the request was not turned away by the auth middleware, it
	// reached a handler and found no Plaid adapter configured in this test.
	require.Equal(t, http.StatusNotFound, status, string(body))
	assert.Contains(t, string(body), "not configured")

	// The tenant-scoped feed routes, by contrast, are behind the tenant guard:
	// the same request, authenticated but with no Xero-Tenant-Id, is refused
	// before it reaches a handler at all.
	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/00000000-0000-0000-0000-000000000001/sync", nil, false)
	require.Equal(t, http.StatusBadRequest, status, string(body))
	assert.Contains(t, string(body), "missing tenant id")
}

// --- the institution a connection ends up at ---------------------------------

// TestHTTP_BankFeed_InstitutionComesFromTheItemWhenTheProviderPicks — a Plaid
// account can refuse a pinned institution, and the adapter then falls back to
// Plaid's own picker, so the bank that gets connected is the one the user chose
// *there* and not the one they clicked in our catalogue. The row has to be
// re-labelled from the Item afterwards: the reconcile screen naming a bank
// nobody linked is worse than naming none.
func TestHTTP_BankFeed_InstitutionComesFromTheItemWhenTheProviderPicks(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	stub.refusePinned = true
	stub.accounts = `{"accounts":[{"account_id":"acc-chk","name":"Plaid Checking",` +
		`"type":"depository","subtype":"checking","balances":{"current":110.00,"iso_currency_code":"USD"}}],` +
		`"item":{"institution_id":"ins_109508","institution_name":"First Platypus Bank"}}`

	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
		"Provider": "plaid", "InstitutionID": "ins_56", "InstitutionName": "Chase", "Country": "US",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		Connections []models.BankFeedConnection
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Connections, 1)
	conn := created.Connections[0]
	require.NotEmpty(t, conn.AuthURL, "a refused institution still has to leave the user somewhere to consent")

	// The refusal is not fatal, it is a second attempt: the request went out with
	// the institution and then without it, which is the whole of the fallback.
	require.Len(t, stub.createBodies, 2)
	assert.Equal(t, "ins_56", stub.createBodies[0]["institution_id"])
	assert.NotContains(t, stub.createBodies[1], "institution_id")

	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	listed := listConnections(t, h)
	require.Len(t, listed, 1)
	assert.Equal(t, "ins_109508", listed[0].InstitutionID)
	assert.Equal(t, "First Platypus Bank", listed[0].InstitutionName,
		"the row must name the bank the Item is actually at")
	assert.Equal(t, models.BankFeedStatusLinked, listed[0].Status)
}

// TestHTTP_BankFeed_ConsentRunsWithNoInstitutionAtAll — the other half of the
// same capability: Plaid's Hosted Link is the whole Link flow, so a request that
// names no bank is not an error, it is the form of the picker. The connection
// still has to come out of it labelled, which is what the Item above is for.
func TestHTTP_BankFeed_ConsentRunsWithNoInstitutionAtAll(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections",
		map[string]any{"Provider": "plaid", "Country": "US"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	require.Len(t, stub.createBodies, 1, "there is nothing to fall back from")
	assert.NotContains(t, stub.createBodies[0], "institution_id")

	var created struct {
		Connections []models.BankFeedConnection
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Connections, 1)
	assert.Empty(t, created.Connections[0].InstitutionID, "nothing has named a bank yet")

	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+created.Connections[0].ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	listed := listConnections(t, h)
	require.Len(t, listed, 1)
	assert.Equal(t, "ins_56", listed[0].InstitutionID, "consent fills the blank in")
	assert.Equal(t, "Chase", listed[0].InstitutionName)
}

// TestHTTP_BankFeed_SecondConnectionToTheSameBankIsRefused — a bank that is
// already connected is not another way in. The provider would mint a second
// identity for the same accounts, every transaction would be staged twice, and
// the chart would grow a second bank account for money that already has one.
// What the user wants instead is the connection that exists.
func TestHTTP_BankFeed_SecondConnectionToTheSameBankIsRefused(t *testing.T) {
	h, _, _ := bankFeedHarness(t)

	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
		"Provider": "plaid", "InstitutionID": "ins_56", "InstitutionName": "Chase", "Country": "US",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, body = h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
		"Provider": "plaid", "InstitutionID": "ins_56", "InstitutionName": "Chase", "Country": "US",
	}, true)
	require.Equal(t, http.StatusConflict, status, string(body))
	assert.Contains(t, string(body), "Chase", "the refusal names the bank it is talking about")
	assert.Contains(t, string(body), "waiting for consent", "and says which connection to go back to")
	assert.Len(t, listConnections(t, h), 1, "the refused request must not leave a row behind")

	// Another bank is unaffected: this is one rule about one institution, not a
	// limit on how many banks somebody may connect.
	status, body = h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
		"Provider": "plaid", "InstitutionID": "ins_109508", "InstitutionName": "First Platypus Bank",
		"Country": "US",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	assert.Len(t, listConnections(t, h), 2)
}

// TestHTTP_BankFeed_PickerCannotLandOnABankThatIsAlreadyConnected — the same rule
// where it is hardest to keep. The request names no bank (a Plaid account that
// refuses a pinned institution makes that the only way in), the user chooses one
// inside Plaid, and the Item is the first thing to say which bank it turned out
// to be. Consent has happened by then, so the connection cannot be un-made — but
// nothing of it is adopted: no accounts, no ledger account, no second feed.
func TestHTTP_BankFeed_PickerCannotLandOnABankThatIsAlreadyConnected(t *testing.T) {
	h, _, _ := bankFeedHarness(t)

	// Already connected, pinned through our own picker and finished, so the bank
	// is genuinely linked. The stub's Item names ins_56 — which is the bank the
	// second consent will land on.
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
		"Provider": "plaid", "InstitutionID": "ins_56", "InstitutionName": "Chase", "Country": "US",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var first struct {
		Connections []models.BankFeedConnection
	}
	require.NoError(t, json.Unmarshal(body, &first))
	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+first.Connections[0].ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	status, body = h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections",
		map[string]any{"Provider": "plaid", "Country": "US"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		Connections []models.BankFeedConnection
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Connections, 1)
	second := created.Connections[0].ConnectionID

	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+second.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusConflict, status, string(body))
	assert.Contains(t, string(body), "already connected")

	listed := listConnections(t, h)
	require.Len(t, listed, 2)
	var found bool
	for _, c := range listed {
		if c.ConnectionID != second {
			assert.Equal(t, models.BankFeedStatusLinked, c.Status, "the connection that exists is untouched")
			continue
		}
		found = true
		assert.Equal(t, models.BankFeedStatusError, c.Status, "the refused one says so rather than looking granted")
		assert.Contains(t, c.LastError, "already connected")
		assert.Empty(t, c.AuthURL, "and it is no longer offered as a consent still to finish")
		assert.Empty(t, c.Accounts, "nothing of the second consent was staged")
	}
	require.True(t, found)

	// The chart is untouched too: the two bank accounts the demo organisation
	// started with plus the one the *first* consent provisioned — and nothing
	// from the consent that was not adopted.
	bankAccounts, err := h.repos.Accounts.List(context.Background(), seedDemoOrgID,
		repository.AccountFilter{Type: models.AccountTypeBank})
	require.NoError(t, err)
	assert.Len(t, bankAccounts, 3, "a refused connection must not provision a ledger account")
}

// TestHTTP_BankFeed_ABankIsFreeAgainOnceItsConnectionIsGone — the guard is about
// two *live* feeds, not about a bank being untouchable. A disconnected bank can
// be connected again, and so can one whose feed broke: the way back from ERROR is
// a repair or a fresh consent, and refusing the fresh one would leave somebody
// with a bank they cannot re-add.
func TestHTTP_BankFeed_ABankIsFreeAgainOnceItsConnectionIsGone(t *testing.T) {
	h, _, _ := bankFeedHarness(t)
	create := func() (int, []byte) {
		return h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
			"Provider": "plaid", "InstitutionID": "ins_56", "InstitutionName": "Chase", "Country": "US",
		}, true)
	}

	status, body := create()
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		Connections []models.BankFeedConnection
	}
	require.NoError(t, json.Unmarshal(body, &created))
	conn := created.Connections[0].ConnectionID

	status, body = create()
	require.Equal(t, http.StatusConflict, status, string(body))

	// Disconnected.
	require.NoError(t, h.repos.BankFeeds.DeleteConnection(context.Background(), seedDemoOrgID, conn))
	status, body = create()
	require.Equal(t, http.StatusCreated, status, string(body))
	assert.Len(t, listConnections(t, h), 1)

	// A broken feed does not hold the bank hostage either.
	require.NoError(t, json.Unmarshal(body, &created))
	require.NoError(t, h.repos.BankFeeds.UpdateConnectionStatus(context.Background(), seedDemoOrgID,
		created.Connections[0].ConnectionID, models.BankFeedStatusError, "ITEM_LOGIN_REQUIRED", nil))
	status, body = create()
	require.Equal(t, http.StatusCreated, status, string(body), string(body))
}

// TestHTTP_BankFeed_DisconnectEndsTheConsentAtTheProvider — the row is what names
// the provider identity, so deleting it is the last moment at which that identity
// can be given up. Left behind, a Plaid Item keeps a working access token and
// keeps occupying one of the account's limited number of Items.
func TestHTTP_BankFeed_DisconnectEndsTheConsentAtTheProvider(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)
	conn, _ := seedPlaidConnection(t, h, nil)
	linkConnection(t, h, conn.ConnectionID)

	status, body := h.do(t, http.MethodDelete,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String(), nil, true)
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Equal(t, []string{"access-sandbox-7"}, stub.revoked, "the Item has to be removed at Plaid")
	assert.Empty(t, listConnections(t, h))
}

// TestHTTP_BankFeed_DisconnectIsNotBlockedByTheProvider — a disconnect the
// provider can veto is not a disconnect. A consent that never completed has
// nothing to revoke, and one the provider refuses must still leave the user with
// the bank gone from their list: the alternative is a connection they cannot get
// rid of while Plaid is down.
func TestHTTP_BankFeed_DisconnectIsNotBlockedByTheProvider(t *testing.T) {
	h, stub, _ := bankFeedHarness(t)

	// A session nobody finished: no durable identity, so no Item exists to remove.
	pending, _ := seedPlaidConnection(t, h, nil)
	status, body := h.do(t, http.MethodDelete,
		"/api/v1/bank-feeds/connections/"+pending.ConnectionID.String(), nil, true)
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Empty(t, stub.revoked, "an unfinished consent is not a Plaid call")
	assert.Empty(t, listConnections(t, h))

	// A provider that will not be told.
	stub.refuseRemove = true
	linked, _ := seedPlaidConnection(t, h, nil)
	linkConnection(t, h, linked.ConnectionID)
	status, body = h.do(t, http.MethodDelete,
		"/api/v1/bank-feeds/connections/"+linked.ConnectionID.String(), nil, true)
	require.Equal(t, http.StatusNoContent, status, string(body),
		"a refused revocation is logged, not raised: the user asked for the bank to go")
	assert.Empty(t, listConnections(t, h))
}

// listConnections reads the connections back the way the bank feeds screen does —
// through the API, not the repository — so what is asserted is what a user sees.
func listConnections(t *testing.T, h *appHarness) []models.BankFeedConnection {
	t.Helper()
	status, body := h.do(t, http.MethodGet, "/api/v1/bank-feeds/connections", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var env struct {
		Payload struct {
			Connections []models.BankFeedConnection
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	return env.Payload.Connections
}

// TestHTTP_BankFeed_ProviderWithoutAPickerStillNeedsAnInstitution — the guard
// around the optional capability. GoCardless's API has no picker to fall back
// to, so an institution is mandatory there; without the guard the request would
// create a PENDING row that can only ever fail.
func TestHTTP_BankFeed_ProviderWithoutAPickerStillNeedsAnInstitution(t *testing.T) {
	h, _, _ := bankFeedHarnessWith(t, fixedInstitutionProvider{})
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections",
		map[string]any{"Provider": "stub_bank", "Country": "GB"}, true)
	require.Equal(t, http.StatusBadRequest, status, string(body))
	assert.Contains(t, string(body), "InstitutionID is required")

	status, body = h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections",
		map[string]any{"Provider": "stub_bank", "InstitutionID": "BANK_GB_1", "Country": "GB"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

// TestHTTP_BankFeed_ProvidersReportWhatTheyCanDo pins the contract the screen
// builds its buttons from. The client cannot inspect an adapter, so if the
// server does not say which flows one has, the client has to guess from the slug
// — which is how a Reconnect button that only ever answers 400 gets shipped.
func TestHTTP_BankFeed_ProvidersReportWhatTheyCanDo(t *testing.T) {
	h, _, _ := bankFeedHarnessWith(t, fixedInstitutionProvider{}, pickerOnlyProvider{})

	status, body := h.do(t, http.MethodGet, "/api/v1/bank-feeds/providers", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var res struct {
		Providers []struct {
			Slug             string `json:"Slug"`
			PicksInstitution bool   `json:"PicksInstitution"`
			CanRepair        bool   `json:"CanRepair"`
		}
	}
	require.NoError(t, json.Unmarshal(body, &res))
	require.Len(t, res.Providers, 3)

	capabilities := map[string][2]bool{}
	for _, p := range res.Providers {
		capabilities[p.Slug] = [2]bool{p.PicksInstitution, p.CanRepair}
	}
	assert.Equal(t, [2]bool{true, true}, capabilities["plaid"],
		"Hosted Link asks which bank to use, and the Item behind it can be repaired in place")
	assert.Equal(t, [2]bool{true, false}, capabilities["picker_only"],
		"picking a bank and repairing a consent are separate questions")
	assert.Equal(t, [2]bool{false, false}, capabilities["stub_bank"],
		"an adapter with neither flow must say so rather than leave the UI to guess")
}

// pickerOnlyProvider implements InstitutionPicker and nothing else: its consent
// flow can run without being told which bank, but a consent of its kind cannot be
// repaired in place. It is here because Plaid implements both interfaces, so a
// test that only ever sees Plaid cannot tell the two capabilities apart — this is
// the provider where their answers diverge.
type pickerOnlyProvider struct{ fixedInstitutionProvider }

func (pickerOnlyProvider) Name() string { return "picker_only" }

func (pickerOnlyProvider) PicksInstitution() bool { return true }

// fixedInstitutionProvider is an adapter whose consent flow must be told which
// institution to use — the shape GoCardless has. It implements the required
// contract and nothing else, which is what makes it the control for the optional
// InstitutionPicker: the absence of the interface is the point.
type fixedInstitutionProvider struct{}

func (fixedInstitutionProvider) Name() string { return "stub_bank" }

func (fixedInstitutionProvider) ListInstitutions(context.Context, string, string) ([]bankfeed.Institution, error) {
	return nil, nil
}

func (fixedInstitutionProvider) CreateSession(context.Context, bankfeed.SessionRequest) (*bankfeed.Session, error) {
	return &bankfeed.Session{ExternalReference: "req-1", AuthURL: "https://stub.example/auth"}, nil
}

func (fixedInstitutionProvider) FinalizeSession(context.Context, bankfeed.Credential) (*bankfeed.Consent, error) {
	return &bankfeed.Consent{Reference: "req-1"}, nil
}

func (fixedInstitutionProvider) FetchStatementLines(context.Context, bankfeed.Credential, string, time.Time, time.Time) ([]bankfeed.StatementLine, error) {
	return nil, nil
}
