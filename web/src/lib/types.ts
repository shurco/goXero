// Type definitions mirroring the Xero Accounting API response shapes.

export type InvoiceType = 'ACCREC' | 'ACCPAY';
export type InvoiceStatus = 'DRAFT' | 'SUBMITTED' | 'AUTHORISED' | 'PAID' | 'VOIDED' | 'DELETED';
export type LineAmountType = 'Exclusive' | 'Inclusive' | 'NoTax';
export type ContactStatus = 'ACTIVE' | 'ARCHIVED' | 'GDPRREQUEST';
export type AccountStatus = 'ACTIVE' | 'ARCHIVED';

/** Address block stored in Organisation.Profile (Xero-like). */
export interface OrganisationAddress {
	AddressLine1?: string;
	City?: string;
	Region?: string;
	PostalCode?: string;
	Country?: string;
	Attention?: string;
}

export interface OrganisationPhone {
	PhoneCountryCode?: string;
	PhoneNumber?: string;
}

export interface OrganisationSocial {
	Facebook?: string;
	Twitter?: string;
	LinkedIn?: string;
}

/** Extra reply-to line (in addition to the logged-in user). */
export interface ReplyEmailAddress {
	ID?: string;
	Email: string;
	Name?: string;
}

/** Stored email template row (subject/body may include [Placeholder] tokens). */
export interface OrgEmailTemplate {
	ID?: string;
	Type: string;
	Name: string;
	IsDefault?: boolean;
	Subject?: string;
	Body?: string;
}

export interface OrganisationProfile {
	ShowExtraOnInvoices?: boolean;
	SameAsPostal?: boolean;
	Postal?: OrganisationAddress;
	Physical?: OrganisationAddress;
	Telephone?: OrganisationPhone;
	Mobile?: OrganisationPhone;
	Fax?: OrganisationPhone;
	Email?: string;
	Website?: string;
	Social?: OrganisationSocial;
	ReplyAddresses?: ReplyEmailAddress[];
	EmailTemplates?: OrgEmailTemplate[];
	/** Whether the business employs staff (onboarding). */
	HasEmployees?: boolean;
	/** Prior accounting system, e.g. QuickBooks, None. */
	PriorAccountingTool?: string;
}

export interface Organisation {
	OrganisationID: string;
	APIKey?: string;
	Name: string;
	LegalName?: string;
	ShortCode?: string;
	OrganisationType?: string;
	CountryCode?: string;
	BaseCurrency: string;
	Timezone?: string;
	FinancialYearEndDay?: number;
	FinancialYearEndMonth?: number;
	TaxNumber?: string;
	LineOfBusiness?: string;
	RegistrationNumber?: string;
	Description?: string;
	Profile?: OrganisationProfile;
	IsDemoCompany?: boolean;
	OrganisationStatus: string;
	CreatedDateUTC?: string;
	UpdatedDateUTC?: string;
}

/** Xero /Users payload — everybody who has access to the current org. */
export interface OrgUser {
	UserID: string;
	EmailAddress: string;
	FirstName?: string;
	LastName?: string;
	IsSubscriber: boolean;
	OrganisationRole: string;
	CreatedDateUTC?: string;
}

export interface TaxRate {
	TaxRateID: string;
	Name: string;
	TaxType: string;
	ReportTaxType?: string;
	CanApplyToAssets: boolean;
	CanApplyToEquity: boolean;
	CanApplyToExpenses: boolean;
	CanApplyToLiabilities: boolean;
	CanApplyToRevenue: boolean;
	DisplayTaxRate: string | number;
	EffectiveRate: string | number;
	Status: string;
}

export interface Currency {
	Code: string;
	Description?: string;
}

export interface Account {
	AccountID: string;
	Code: string;
	Name: string;
	Type: string;
	Status: AccountStatus;
	BankAccountNumber?: string;
	BankAccountType?: string;
	CurrencyCode?: string;
	Description?: string;
	TaxType?: string;
	EnablePaymentsToAccount?: boolean;
	ShowInExpenseClaims?: boolean;
	Class?: string;
	SystemAccount?: string;
	UpdatedDateUTC?: string;
}

export interface Address {
	AddressType: 'POBOX' | 'STREET' | 'DELIVERY';
	AddressLine1?: string;
	AddressLine2?: string;
	City?: string;
	Region?: string;
	PostalCode?: string;
	Country?: string;
}

export interface Phone {
	PhoneType: 'DEFAULT' | 'DDI' | 'MOBILE' | 'FAX';
	PhoneNumber?: string;
	PhoneAreaCode?: string;
	PhoneCountryCode?: string;
}

export interface Contact {
	ContactID: string;
	ContactStatus: ContactStatus;
	Name: string;
	FirstName?: string;
	LastName?: string;
	EmailAddress?: string;
	TaxNumber?: string;
	IsSupplier: boolean;
	IsCustomer: boolean;
	DefaultCurrency?: string;
	Website?: string;
	Addresses?: Address[];
	Phones?: Phone[];
	UpdatedDateUTC?: string;
}

export interface LineItem {
	LineItemID?: string;
	Description?: string;
	Quantity: string | number;
	UnitAmount: string | number;
	ItemCode?: string;
	AccountCode?: string;
	TaxType?: string;
	TaxAmount?: string | number;
	LineAmount?: string | number;
	DiscountRate?: string | number;
}

export interface Invoice {
	InvoiceID: string;
	Type: InvoiceType;
	Contact?: Contact;
	ContactID?: string;
	InvoiceNumber?: string;
	Reference?: string;
	CurrencyCode?: string;
	Status: InvoiceStatus;
	LineAmountTypes: LineAmountType;
	Date?: string;
	DueDate?: string;
	SubTotal: string | number;
	TotalTax: string | number;
	Total: string | number;
	AmountDue: string | number;
	AmountPaid: string | number;
	LineItems?: LineItem[];
	UpdatedDateUTC?: string;
}

export type QuoteStatus = 'DRAFT' | 'SENT' | 'DECLINED' | 'ACCEPTED' | 'INVOICED' | 'DELETED';

export interface Quote {
	QuoteID: string;
	Contact?: Contact;
	ContactID?: string;
	QuoteNumber?: string;
	Reference?: string;
	Title?: string;
	Summary?: string;
	Terms?: string;
	Date?: string;
	ExpiryDate?: string;
	Status: QuoteStatus;
	LineAmountTypes: LineAmountType;
	CurrencyCode?: string;
	SubTotal: string | number;
	TotalTax: string | number;
	Total: string | number;
	TotalDiscount?: string | number;
	LineItems?: LineItem[];
	UpdatedDateUTC?: string;
}

export interface Item {
	ItemID: string;
	Code: string;
	Name?: string;
	Description?: string;
	IsSold: boolean;
	IsPurchased: boolean;
	IsTrackedAsInventory: boolean;
	QuantityOnHand: string | number;
	SalesDetails?: { UnitPrice?: string | number; AccountCode?: string; TaxType?: string };
	PurchaseDetails?: { UnitPrice?: string | number; AccountCode?: string; TaxType?: string };
	UpdatedDateUTC?: string;
}

export interface Payment {
	PaymentID: string;
	PaymentType: string;
	Status: string;
	Date: string;
	Amount: string | number;
	Reference?: string;
	IsReconciled?: boolean;
	UpdatedDateUTC?: string;
}

export interface Pagination {
	page: number;
	pageSize: number;
	total: number;
}

/** Row in Organisation → Files (inbox / archive). */
export interface OrgFile {
	AttachmentID: string;
	FileName: string;
	MimeType?: string;
	ContentLength: number;
	FileFolder?: string;
	CreatedDateUTC?: string;
}

export interface InvoiceSummary {
	totalInvoices: number;
	draft: number;
	authorised: number;
	paid: number;
	overdue: number;
	totalDue: string | number;
	totalPaid: string | number;
}

export interface TenantSummary {
	organisationId: string;
	name: string;
	shortCode?: string;
	baseCurrency?: string;
}

export interface LoginResponse {
	token: string;
	refreshToken: string;
	expiresAt: string;
	refreshTokenExpiresAt: string;
	email: string;
	user: { userId: string; firstName?: string; lastName?: string };
	organisations: TenantSummary[];
}

export interface RefreshResponse {
	token: string;
	refreshToken: string;
	expiresAt: string;
	refreshTokenExpiresAt: string;
	email: string;
	user: { userId: string; firstName?: string; lastName?: string };
	organisations: TenantSummary[];
}

// ── Xero extended resources ─────────────────────────────────────────────────

export type BankTransactionType = 'RECEIVE' | 'SPEND' | 'RECEIVE-OVERPAYMENT' | 'SPEND-OVERPAYMENT';

export interface BankTransaction {
	BankTransactionID: string;
	Type: BankTransactionType;
	Status: InvoiceStatus;
	LineAmountTypes: LineAmountType;
	/** Present when the API embeds the bank account (list/get). */
	BankAccount?: Pick<Account, 'AccountID' | 'Name' | 'Code'>;
	BankAccountID?: string;
	ContactID?: string;
	Contact?: Contact;
	CurrencyCode?: string;
	Date?: string;
	Reference?: string;
	SubTotal?: string | number;
	TotalTax?: string | number;
	Total?: string | number;
	LineItems?: LineItem[];
	IsReconciled?: boolean;
	UpdatedDateUTC?: string;
}

export type ManualJournalStatus = 'DRAFT' | 'POSTED' | 'DELETED' | 'VOIDED';

export interface ManualJournalLine {
	LineAmount: string | number;
	AccountCode?: string;
	AccountID?: string;
	Description?: string;
	TaxType?: string;
}

export interface ManualJournal {
	ManualJournalID: string;
	Narration: string;
	Status: ManualJournalStatus;
	Date?: string;
	ShowOnCashBasisReports?: boolean;
	JournalLines?: ManualJournalLine[];
	UpdatedDateUTC?: string;
}

export type BankRuleType = 'SPEND' | 'RECEIVE' | 'TRANSFER';

export interface BankRuleCondition {
	Field: string;
	Operator: string;
	Value: string;
}

export interface BankRuleAllocationLine {
	LineID?: string;
	Description?: string;
	AccountID?: string;
	TaxRateID?: string;
	Region?: string;
	Amount?: number;
	Percent?: number;
}

export interface BankRuleDefinition {
	MatchMode: string;
	Conditions: BankRuleCondition[];
	ContactMode?: string;
	ContactID?: string;
	ContactName?: string;
	FixedLines?: BankRuleAllocationLine[];
	PercentLines?: BankRuleAllocationLine[];
	ReferenceField?: string;
	RunOn: string;
	ScopeBankAccountID?: string;
	TransferTargetMode?: string;
	TransferBankAccountID?: string;
	TransferTrackingRegion?: string;
}

export interface BankRule {
	BankRuleID?: string;
	RuleType: BankRuleType;
	Name: string;
	Definition: BankRuleDefinition;
	IsActive?: boolean;
	CreatedDateUTC?: string;
	UpdatedDateUTC?: string;
}

// ── Reports ────────────────────────────────────────────────────────────────

export interface ReportCell {
	Value?: string;
	Attributes?: Record<string, string>;
}
export interface ReportRow {
	RowType?: 'Header' | 'Section' | 'Row' | 'SummaryRow';
	Title?: string;
	Cells?: ReportCell[];
	Rows?: ReportRow[];
}
export interface Report {
	ReportID?: string;
	ReportName?: string;
	ReportType?: string;
	ReportTitles?: string[];
	ReportDate?: string;
	UpdatedDateUTC?: string;
	Rows?: ReportRow[];
}
