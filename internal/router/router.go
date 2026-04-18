package router

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/shurco/goxero/internal/bankfeed"
	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/handlers"
	mw "github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/repository"
)

// Register wires up every route on the Fiber app.
func Register(app *fiber.App, cfg *config.Config, repos *repository.Repositories) {
	app.Use(requestid.New())
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", cfg.Auth.TenantHeaderName},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	auth := handlers.NewAuthHandler(cfg.Auth, repos)
	organisation := handlers.NewOrganisationHandler(repos)
	accountHandler := handlers.NewAccountHandler(repos)
	taxHandler := handlers.NewTaxRateHandler(repos)
	contactHandler := handlers.NewContactHandler(repos)
	itemHandler := handlers.NewItemHandler(repos)
	invoiceHandler := handlers.NewInvoiceHandler(repos)
	paymentHandler := handlers.NewPaymentHandler(repos)
	creditNoteHandler := handlers.NewCreditNoteHandler(repos)
	bankTxHandler := handlers.NewBankTransactionHandler(repos)
	bankTransferHandler := handlers.NewBankTransferHandler(repos)
	manualJournalHandler := handlers.NewManualJournalHandler(repos)
	journalHandler := handlers.NewJournalHandler(repos)
	quoteHandler := handlers.NewQuoteHandler(repos)
	poHandler := handlers.NewPurchaseOrderHandler(repos)
	contactGroupHandler := handlers.NewContactGroupHandler(repos)
	currencyHandler := handlers.NewCurrencyHandler(repos)
	brandingHandler := handlers.NewBrandingThemeHandler(repos)
	trackingHandler := handlers.NewTrackingCategoryHandler(repos)
	attachmentHandler := handlers.NewAttachmentHandler(repos)
	historyHandler := handlers.NewHistoryHandler(repos)
	reportHandler := handlers.NewReportHandler(repos)
	prepaymentHandler := handlers.NewPrepaymentHandler(repos)
	overpaymentHandler := handlers.NewOverpaymentHandler(repos)
	repeatingHandler := handlers.NewRepeatingInvoiceHandler(repos)
	batchPaymentHandler := handlers.NewBatchPaymentHandler(repos)
	linkedTxHandler := handlers.NewLinkedTransactionHandler(repos)
	employeeHandler := handlers.NewEmployeeHandler(repos)
	receiptHandler := handlers.NewReceiptHandler(repos)
	expenseClaimHandler := handlers.NewExpenseClaimHandler(repos)
	usersHandler := handlers.NewUsersHandler(repos)

	bankFeedRegistry := bankfeed.NewRegistry()
	if gc := bankfeed.NewGoCardless(cfg.BankFeed.GoCardlessBADSecretID, cfg.BankFeed.GoCardlessBADSecretKey); gc.Credentials() {
		bankFeedRegistry.Register(gc)
	}
	bankFeedHandler := handlers.NewBankFeedHandler(repos, bankFeedRegistry, cfg.BankFeed.RedirectURL, cfg.BankFeed.SyncWindow)

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Public auth routes
	app.Post("/api/auth/login", auth.Login)
	app.Post("/api/auth/register", auth.Register)
	// Refresh + logout accept a raw refresh token in the body and therefore
	// must be reachable *without* a valid JWT (the whole point of refresh is
	// that the access token has expired). Logout also accepts an optional JWT
	// via the handler-side middleware below.
	app.Post("/api/auth/refresh", auth.Refresh)
	app.Post("/api/auth/logout", mw.OptionalJWTAuth(cfg.Auth), auth.Logout)

	// Authenticated routes (no tenant required)
	api := app.Group("/api", mw.JWTAuth(cfg.Auth))
	api.Get("/auth/me", auth.Me)
	api.Get("/organisations", organisation.List)
	api.Post("/organisations", organisation.Create)

	// Versioned accounting API (require tenant)
	apiV1 := app.Group("/api/v1", mw.JWTAuth(cfg.Auth), mw.Tenant(cfg.Auth, repos))
	apiV1.Get("/organisation", organisation.Get)

	apiV1.Get("/accounts", accountHandler.List)
	apiV1.Post("/accounts", accountHandler.Create)
	apiV1.Get("/accounts/:id", accountHandler.Get)
	apiV1.Put("/accounts/:id", accountHandler.Update)
	apiV1.Post("/accounts/:id", accountHandler.Update)
	apiV1.Delete("/accounts/:id", accountHandler.Delete)

	apiV1.Get("/tax-rates", taxHandler.List)
	apiV1.Post("/tax-rates", taxHandler.Create)
	apiV1.Get("/tax-rates/:id", taxHandler.Get)
	apiV1.Put("/tax-rates/:id", taxHandler.Update)
	apiV1.Post("/tax-rates/:id", taxHandler.Update)
	apiV1.Delete("/tax-rates/:id", taxHandler.Delete)

	apiV1.Get("/contacts", contactHandler.List)
	apiV1.Post("/contacts", contactHandler.Create)
	apiV1.Get("/contacts/:id", contactHandler.Get)
	apiV1.Put("/contacts/:id", contactHandler.Update)
	apiV1.Post("/contacts/:id", contactHandler.Update)
	apiV1.Delete("/contacts/:id", contactHandler.Archive)

	apiV1.Get("/contact-groups", contactGroupHandler.List)
	apiV1.Post("/contact-groups", contactGroupHandler.Create)
	apiV1.Get("/contact-groups/:id", contactGroupHandler.Get)
	apiV1.Put("/contact-groups/:id", contactGroupHandler.Update)
	apiV1.Post("/contact-groups/:id", contactGroupHandler.Update)
	apiV1.Delete("/contact-groups/:id", contactGroupHandler.Delete)
	apiV1.Put("/contact-groups/:id/contacts", contactGroupHandler.AddContacts)
	apiV1.Delete("/contact-groups/:id/contacts/:contactId", contactGroupHandler.RemoveContact)

	apiV1.Get("/items", itemHandler.List)
	apiV1.Post("/items", itemHandler.Create)
	apiV1.Get("/items/:id", itemHandler.Get)
	apiV1.Put("/items/:id", itemHandler.Update)
	apiV1.Post("/items/:id", itemHandler.Update)
	apiV1.Delete("/items/:id", itemHandler.Delete)

	apiV1.Get("/invoices", invoiceHandler.List)
	apiV1.Post("/invoices", invoiceHandler.Create)
	apiV1.Get("/invoices/:id", invoiceHandler.Get)
	apiV1.Put("/invoices/:id", invoiceHandler.Update)
	apiV1.Post("/invoices/:id", invoiceHandler.Update)
	apiV1.Delete("/invoices/:id", invoiceHandler.Delete)
	apiV1.Get("/invoices/:id/payments", invoiceHandler.Payments)
	apiV1.Post("/invoices/:id/email", invoiceHandler.Email)
	apiV1.Get("/invoices/:id/online-invoice", invoiceHandler.OnlineInvoice)

	apiV1.Get("/credit-notes", creditNoteHandler.List)
	apiV1.Post("/credit-notes", creditNoteHandler.Create)
	apiV1.Get("/credit-notes/:id", creditNoteHandler.Get)
	apiV1.Put("/credit-notes/:id", creditNoteHandler.Update)
	apiV1.Post("/credit-notes/:id", creditNoteHandler.Update)
	apiV1.Delete("/credit-notes/:id", creditNoteHandler.Delete)
	apiV1.Post("/credit-notes/:id/allocations", creditNoteHandler.Allocate)

	apiV1.Get("/bank-transactions", bankTxHandler.List)
	apiV1.Post("/bank-transactions", bankTxHandler.Create)
	apiV1.Get("/bank-transactions/:id", bankTxHandler.Get)
	apiV1.Delete("/bank-transactions/:id", bankTxHandler.Delete)

	apiV1.Get("/bank-transfers", bankTransferHandler.List)
	apiV1.Post("/bank-transfers", bankTransferHandler.Create)
	apiV1.Get("/bank-transfers/:id", bankTransferHandler.Get)

	apiV1.Get("/manual-journals", manualJournalHandler.List)
	apiV1.Post("/manual-journals", manualJournalHandler.Create)
	apiV1.Get("/manual-journals/:id", manualJournalHandler.Get)
	apiV1.Delete("/manual-journals/:id", manualJournalHandler.Delete)

	apiV1.Get("/journals", journalHandler.List)

	apiV1.Get("/quotes", quoteHandler.List)
	apiV1.Post("/quotes", quoteHandler.Create)
	apiV1.Get("/quotes/:id", quoteHandler.Get)
	apiV1.Put("/quotes/:id", quoteHandler.Update)
	apiV1.Post("/quotes/:id", quoteHandler.Update)
	apiV1.Delete("/quotes/:id", quoteHandler.Delete)

	apiV1.Get("/purchase-orders", poHandler.List)
	apiV1.Post("/purchase-orders", poHandler.Create)
	apiV1.Get("/purchase-orders/:id", poHandler.Get)
	apiV1.Put("/purchase-orders/:id", poHandler.Update)
	apiV1.Post("/purchase-orders/:id", poHandler.Update)
	apiV1.Delete("/purchase-orders/:id", poHandler.Delete)

	apiV1.Get("/currencies", currencyHandler.List)
	apiV1.Post("/currencies", currencyHandler.Create)

	apiV1.Get("/branding-themes", brandingHandler.List)
	apiV1.Post("/branding-themes", brandingHandler.Create)

	apiV1.Get("/tracking-categories", trackingHandler.List)
	apiV1.Post("/tracking-categories", trackingHandler.Create)
	apiV1.Get("/tracking-categories/:id", trackingHandler.Get)
	apiV1.Put("/tracking-categories/:id", trackingHandler.Update)
	apiV1.Post("/tracking-categories/:id", trackingHandler.Update)
	apiV1.Delete("/tracking-categories/:id", trackingHandler.Delete)
	apiV1.Put("/tracking-categories/:id/options", trackingHandler.AddOption)
	apiV1.Post("/tracking-categories/:id/options", trackingHandler.AddOption)

	apiV1.Get("/payments", paymentHandler.List)
	apiV1.Post("/payments", paymentHandler.Create)
	apiV1.Get("/payments/:id", paymentHandler.Get)
	apiV1.Delete("/payments/:id", paymentHandler.Delete)

	apiV1.Get("/batch-payments", batchPaymentHandler.List)
	apiV1.Post("/batch-payments", batchPaymentHandler.Create)
	apiV1.Get("/batch-payments/:id", batchPaymentHandler.Get)

	apiV1.Get("/prepayments", prepaymentHandler.List)
	apiV1.Post("/prepayments", prepaymentHandler.Create)
	apiV1.Get("/prepayments/:id", prepaymentHandler.Get)
	apiV1.Delete("/prepayments/:id", prepaymentHandler.Delete)

	apiV1.Get("/overpayments", overpaymentHandler.List)
	apiV1.Post("/overpayments", overpaymentHandler.Create)
	apiV1.Get("/overpayments/:id", overpaymentHandler.Get)
	apiV1.Delete("/overpayments/:id", overpaymentHandler.Delete)

	apiV1.Get("/repeating-invoices", repeatingHandler.List)
	apiV1.Post("/repeating-invoices", repeatingHandler.Create)
	apiV1.Get("/repeating-invoices/:id", repeatingHandler.Get)
	apiV1.Delete("/repeating-invoices/:id", repeatingHandler.Delete)

	apiV1.Get("/linked-transactions", linkedTxHandler.List)
	apiV1.Post("/linked-transactions", linkedTxHandler.Create)
	apiV1.Get("/linked-transactions/:id", linkedTxHandler.Get)
	apiV1.Delete("/linked-transactions/:id", linkedTxHandler.Delete)

	apiV1.Get("/employees", employeeHandler.List)
	apiV1.Post("/employees", employeeHandler.Create)
	apiV1.Get("/employees/:id", employeeHandler.Get)
	apiV1.Put("/employees/:id", employeeHandler.Update)
	apiV1.Post("/employees/:id", employeeHandler.Update)
	apiV1.Delete("/employees/:id", employeeHandler.Delete)

	apiV1.Get("/receipts", receiptHandler.List)
	apiV1.Post("/receipts", receiptHandler.Create)
	apiV1.Get("/receipts/:id", receiptHandler.Get)
	apiV1.Delete("/receipts/:id", receiptHandler.Delete)

	apiV1.Get("/expense-claims", expenseClaimHandler.List)
	apiV1.Post("/expense-claims", expenseClaimHandler.Create)
	apiV1.Get("/expense-claims/:id", expenseClaimHandler.Get)
	apiV1.Delete("/expense-claims/:id", expenseClaimHandler.Delete)

	apiV1.Get("/users", usersHandler.List)

	// Bank feeds — Open Banking integration (GoCardless BAD, Plaid, …).
	apiV1.Get("/bank-feeds/providers", bankFeedHandler.ListProviders)
	apiV1.Get("/bank-feeds/institutions", bankFeedHandler.ListInstitutions)
	apiV1.Get("/bank-feeds/connections", bankFeedHandler.ListConnections)
	apiV1.Post("/bank-feeds/connections", bankFeedHandler.CreateConnection)
	apiV1.Get("/bank-feeds/connections/:id", bankFeedHandler.GetConnection)
	apiV1.Post("/bank-feeds/connections/:id/finalize", bankFeedHandler.FinalizeConnection)
	apiV1.Post("/bank-feeds/connections/:id/sync", bankFeedHandler.SyncConnection)
	apiV1.Delete("/bank-feeds/connections/:id", bankFeedHandler.DeleteConnection)
	apiV1.Put("/bank-feeds/accounts/:feedAccountId", bankFeedHandler.BindFeedAccount)
	apiV1.Get("/bank-feeds/statement-lines", bankFeedHandler.ListStatementLines)
	apiV1.Post("/bank-feeds/statement-lines/:id/import", bankFeedHandler.ImportStatementLine)
	apiV1.Post("/bank-feeds/statement-lines/:id/ignore", bankFeedHandler.IgnoreStatementLine)

	// Polymorphic attachment + history endpoints (Xero pattern).
	apiV1.Get("/:subject/:id/attachments", attachmentHandler.List)
	apiV1.Post("/:subject/:id/attachments/:filename", attachmentHandler.Upload)
	apiV1.Put("/:subject/:id/attachments/:filename", attachmentHandler.Upload)
	apiV1.Get("/:subject/:id/attachments/:attachmentId", attachmentHandler.Fetch)
	apiV1.Get("/attachments/:attachmentId/content", attachmentHandler.Fetch)
	apiV1.Get("/:subject/:id/history", historyHandler.List)
	apiV1.Put("/:subject/:id/history", historyHandler.AddNote)
	apiV1.Post("/:subject/:id/history", historyHandler.AddNote)

	// Reports.
	apiV1.Get("/reports", reportHandler.ReportsList)
	apiV1.Get("/reports/invoice-summary", invoiceHandler.Summary)
	apiV1.Get("/reports/trial-balance", reportHandler.TrialBalance)
	apiV1.Get("/reports/profit-and-loss", reportHandler.ProfitAndLoss)
	apiV1.Get("/reports/profit-loss", reportHandler.ProfitAndLoss)
	apiV1.Get("/reports/balance-sheet", reportHandler.BalanceSheet)
	apiV1.Get("/reports/aged-receivables", reportHandler.AgedReceivables)
	apiV1.Get("/reports/aged-payables", reportHandler.AgedPayables)
	apiV1.Get("/reports/aged-receivables-by-contact", reportHandler.AgedReceivables)
	apiV1.Get("/reports/aged-payables-by-contact", reportHandler.AgedPayables)
	apiV1.Get("/reports/bank-summary", reportHandler.BankSummary)
	apiV1.Get("/reports/cash-summary", reportHandler.CashSummary)
	apiV1.Get("/reports/executive-summary", reportHandler.ExecutiveSummary)
	apiV1.Get("/reports/budget-summary", reportHandler.BudgetSummary)
	apiV1.Get("/reports/bas", reportHandler.BAS)
	apiV1.Get("/reports/sales-tax", reportHandler.BAS)
	apiV1.Get("/reports/journal-report", reportHandler.JournalReport)
}
