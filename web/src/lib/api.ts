import { browser } from '$app/environment';
import { goto } from '$app/navigation';
import { session } from './stores/session';
import { get } from 'svelte/store';
import type {
	Account,
	BankRule,
	BankTransaction,
	Contact,
	Currency,
	Invoice,
	InvoiceSummary,
	Item,
	LoginResponse,
	ManualJournal,
	OrgFile,
	Organisation,
	OrgUser,
	Pagination,
	Payment,
	Quote,
	RefreshResponse,
	TaxRate
} from './types';

interface ApiError extends Error {
	status: number;
	payload?: unknown;
}

/**
 * Skew tolerated when checking access-token expiry. A request starting within
 * this window triggers a proactive refresh so that by the time the HTTP call
 * reaches the server the new token is already in effect.
 */
const REFRESH_SKEW_MS = 30_000;

/**
 * Singleton promise for any in-flight refresh. Parallel requests that all
 * notice an expired token would otherwise stampede the /refresh endpoint and
 * invalidate each other's rotation — instead we share the first call's result.
 */
let pendingRefresh: Promise<boolean> | null = null;

/**
 * Calls `/api/auth/refresh` and replaces the session tokens on success.
 * Returns `true` when a fresh access token is now in the session store.
 */
async function refreshAccessToken(): Promise<boolean> {
	if (!browser) return false;
	if (pendingRefresh) return pendingRefresh;

	const s = get(session);
	if (!s.refreshToken) return false;
	if (s.refreshExpiresAt && s.refreshExpiresAt <= Date.now()) {
		session.logout();
		return false;
	}

	pendingRefresh = (async () => {
		try {
			const res = await fetch('/api/auth/refresh', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
				body: JSON.stringify({ refreshToken: s.refreshToken })
			});
			if (!res.ok) return false;
			const data = (await res.json()) as RefreshResponse;
			session.setTokens({
				token: data.token,
				refreshToken: data.refreshToken,
				expiresAt: data.expiresAt,
				refreshTokenExpiresAt: data.refreshTokenExpiresAt
			});
			return true;
		} catch {
			return false;
		} finally {
			pendingRefresh = null;
		}
	})();

	return pendingRefresh;
}

function shouldPreemptivelyRefresh(): boolean {
	const s = get(session);
	if (!s.token || !s.refreshToken || !s.expiresAt) return false;
	return s.expiresAt - Date.now() <= REFRESH_SKEW_MS;
}

/**
 * Core fetcher that attaches the JWT + tenant header, proactively refreshes
 * expiring tokens, retries once on a 401 after refresh, and surfaces API
 * errors in a consistent shape for the UI layer.
 */
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
	if (!browser) {
		// Return a dummy value for SSR — pages re-fetch on the client.
		return undefined as T;
	}

	// Skip refresh on the auth endpoints themselves so we don't recurse.
	const isAuthPath = path.startsWith('/api/auth/');
	if (!isAuthPath && shouldPreemptivelyRefresh()) {
		await refreshAccessToken();
	}

	const doFetch = async (): Promise<Response> => {
		const s = get(session);
		const headers = new Headers(init.headers || {});
		if (!headers.has('Content-Type') && init.body && !(init.body instanceof FormData)) {
			headers.set('Content-Type', 'application/json');
		}
		headers.set('Accept', 'application/json');
		if (s.token) headers.set('Authorization', `Bearer ${s.token}`);
		if (s.tenantId && path.startsWith('/api/v1/')) headers.set('Xero-Tenant-Id', s.tenantId);
		return fetch(path, { ...init, headers });
	};

	let res = await doFetch();

	// On 401 try exactly one refresh+retry round. Reuses the singleton
	// promise so concurrent 401s share one refresh call.
	if (res.status === 401 && !isAuthPath) {
		const refreshed = await refreshAccessToken();
		if (refreshed) {
			res = await doFetch();
		}
	}

	const text = await res.text();
	const data = text ? safeJson(text) : undefined;

	if (!res.ok) {
		if (res.status === 401) {
			session.logout();
			goto('/login');
		}
		const err: ApiError = Object.assign(
			new Error(typeof data === 'object' && data && 'Message' in data ? (data as { Message: string }).Message : res.statusText),
			{ status: res.status, payload: data }
		);
		throw err;
	}

	return data as T;
}

function safeJson(text: string) {
	try {
		return JSON.parse(text);
	} catch {
		return text;
	}
}

// ── Auth ────────────────────────────────────────────────────────────────────
export const authApi = {
	login: (email: string, password: string) =>
		request<LoginResponse>('/api/auth/login', {
			method: 'POST',
			body: JSON.stringify({ email, password })
		}),
	register: (payload: { email: string; password: string; firstName?: string; lastName?: string }) =>
		request<LoginResponse>('/api/auth/register', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	refresh: (refreshToken: string) =>
		request<RefreshResponse>('/api/auth/refresh', {
			method: 'POST',
			body: JSON.stringify({ refreshToken })
		}),
	/**
	 * Revokes the caller's refresh token server-side. Passing
	 * `everywhere: true` revokes every refresh token for the user (requires
	 * a valid access token to identify them).
	 */
	logout: (opts: { refreshToken?: string | null; everywhere?: boolean } = {}) => {
		const qs = opts.everywhere ? '?everywhere=true' : '';
		return request<void>(`/api/auth/logout${qs}`, {
			method: 'POST',
			body: JSON.stringify({ refreshToken: opts.refreshToken ?? '' })
		});
	},
	me: () =>
		request<{ user: { userId: string; email: string; firstName?: string; lastName?: string }; organisations: { organisationId: string; name: string; baseCurrency?: string }[] }>(
			'/api/auth/me'
		)
};

// ── Generic Xero envelope helpers ──────────────────────────────────────────
interface XeroEnvelope<K extends string, T> {
	Payload?: Record<K, T[]>;
}

function unwrap<K extends string, T>(resp: XeroEnvelope<K, T> | Record<K, T[]>, key: K): T[] {
	if ('Payload' in resp && resp.Payload && key in resp.Payload) {
		return (resp.Payload as Record<K, T[]>)[key] ?? [];
	}
	return (resp as Record<K, T[]>)[key] ?? [];
}

// ── Organisation ───────────────────────────────────────────────────────────
export const orgApi = {
	current: async () => {
		const res = await request<XeroEnvelope<'Organisations', Organisation>>('/api/v1/organisation');
		return unwrap(res, 'Organisations')[0];
	},
	mine: () => request<{ organisations: Organisation[] }>('/api/organisations'),
	create: (payload: {
		name: string;
		legalName?: string;
		shortCode?: string;
		organisationType?: string;
		countryCode?: string;
		baseCurrency?: string;
		timezone?: string;
		taxNumber?: string;
		lineOfBusiness?: string;
		registrationNumber?: string;
		financialYearEndDay?: number;
		financialYearEndMonth?: number;
		hasEmployees?: boolean;
		priorAccountingTool?: string;
	}) =>
		request<{ organisation: Organisation }>('/api/organisations', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	/** PUT current tenant organisation (settings). */
	update: async (payload: Partial<Organisation> & { Profile?: Organisation['Profile'] }) => {
		const res = await request<XeroEnvelope<'Organisations', Organisation>>('/api/v1/organisation', {
			method: 'PUT',
			body: JSON.stringify(payload)
		});
		return unwrap(res, 'Organisations')[0];
	}
};

// ── Accounts ───────────────────────────────────────────────────────────────
export const accountApi = {
	list: async (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		const res = await request<XeroEnvelope<'Accounts', Account>>(
			'/api/v1/accounts' + (qs ? `?${qs}` : '')
		);
		return unwrap(res, 'Accounts');
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'Accounts', Account>>(`/api/v1/accounts/${id}`);
		return unwrap(res, 'Accounts')[0];
	},
	create: (payload: Partial<Account>) =>
		request<{ Accounts: Account[] }>(`/api/v1/accounts`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: Partial<Account>) =>
		request<{ Accounts: Account[] }>(`/api/v1/accounts/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	delete: (id: string) => request<void>(`/api/v1/accounts/${id}`, { method: 'DELETE' })
};

// ── Contacts ───────────────────────────────────────────────────────────────
export const contactApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ Contacts: Contact[]; Pagination: Pagination }>(
			'/api/v1/contacts' + (qs ? `?${qs}` : '')
		);
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'Contacts', Contact>>(`/api/v1/contacts/${id}`);
		return unwrap(res, 'Contacts')[0];
	},
	create: (payload: Partial<Contact>) =>
		request<{ Contacts: Contact[] }>(`/api/v1/contacts`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: Partial<Contact>) =>
		request<{ Contacts: Contact[] }>(`/api/v1/contacts/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		})
};

// ── Items ──────────────────────────────────────────────────────────────────
export const itemApi = {
	list: async () => {
		const res = await request<XeroEnvelope<'Items', Item>>('/api/v1/items');
		return unwrap(res, 'Items');
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'Items', Item>>(`/api/v1/items/${id}`);
		return unwrap(res, 'Items')[0];
	},
	create: (payload: Partial<Item>) =>
		request<{ Items: Item[] }>(`/api/v1/items`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: Partial<Item>) =>
		request<{ Items: Item[] }>(`/api/v1/items/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		})
};

// ── Invoices ───────────────────────────────────────────────────────────────
export const invoiceApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ Invoices: Invoice[]; Pagination: Pagination }>(
			'/api/v1/invoices' + (qs ? `?${qs}` : '')
		);
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'Invoices', Invoice>>(`/api/v1/invoices/${id}`);
		return unwrap(res, 'Invoices')[0];
	},
	create: (payload: Partial<Invoice>) =>
		request<{ Invoices: Invoice[] }>(`/api/v1/invoices`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: Partial<Invoice>) =>
		request<{ Invoices: Invoice[] }>(`/api/v1/invoices/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	updateStatus: (id: string, status: string) =>
		request<{ Invoices: Invoice[] }>(`/api/v1/invoices/${id}`, {
			method: 'PUT',
			body: JSON.stringify({ status })
		}),
	delete: (id: string) =>
		request<void>(`/api/v1/invoices/${id}`, { method: 'DELETE' }),
	payments: async (id: string) => {
		const res = await request<XeroEnvelope<'Payments', Payment>>(
			`/api/v1/invoices/${id}/payments`
		);
		return unwrap(res, 'Payments');
	},
	email: (id: string) =>
		request<void>(`/api/v1/invoices/${id}/email`, { method: 'POST' }),
	onlineInvoice: (id: string) =>
		request<{ OnlineInvoices: { OnlineInvoiceUrl: string }[] }>(
			`/api/v1/invoices/${id}/online-invoice`
		),
	summary: () => request<InvoiceSummary>('/api/v1/reports/invoice-summary')
};

// ── Payments ───────────────────────────────────────────────────────────────
export const paymentApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ Payments: Payment[]; Pagination: Pagination }>(
			'/api/v1/payments' + (qs ? `?${qs}` : '')
		);
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'Payments', Payment>>(`/api/v1/payments/${id}`);
		return unwrap(res, 'Payments')[0];
	},
	create: (payload: {
		invoiceId?: string;
		accountId?: string;
		date?: string;
		amount: string | number;
		reference?: string;
	}) =>
		request<{ Payments: Payment[] }>(`/api/v1/payments`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	void: (id: string) => request<void>(`/api/v1/payments/${id}`, { method: 'DELETE' })
};

// ── Bank transactions ──────────────────────────────────────────────────────
export const bankTransactionApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ BankTransactions: BankTransaction[]; Pagination: Pagination }>(
			'/api/v1/bank-transactions' + (qs ? `?${qs}` : '')
		);
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'BankTransactions', BankTransaction>>(
			`/api/v1/bank-transactions/${id}`
		);
		return unwrap(res, 'BankTransactions')[0];
	},
	create: (payload: Partial<BankTransaction>) =>
		request<{ BankTransactions: BankTransaction[] }>(`/api/v1/bank-transactions`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	delete: (id: string) =>
		request<void>(`/api/v1/bank-transactions/${id}`, { method: 'DELETE' })
};

// ── Bank rules ─────────────────────────────────────────────────────────────
export const bankRuleApi = {
	list: async () => {
		const res = await request<XeroEnvelope<'BankRules', BankRule>>('/api/v1/bank-rules');
		return unwrap(res, 'BankRules');
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'BankRules', BankRule>>(`/api/v1/bank-rules/${id}`);
		return unwrap(res, 'BankRules')[0];
	},
	create: (payload: BankRule) =>
		request<{ BankRules: BankRule[] }>('/api/v1/bank-rules', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: BankRule) =>
		request<{ BankRules: BankRule[] }>(`/api/v1/bank-rules/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	delete: (id: string) => request<void>(`/api/v1/bank-rules/${id}`, { method: 'DELETE' })
};

// ── Users (GET /api/v1/users) ─────────────────────────────────────────────
export const userApi = {
	list: async () => {
		const res = await request<XeroEnvelope<'Users', OrgUser>>('/api/v1/users');
		return unwrap(res, 'Users');
	}
};

// ── Tax rates ──────────────────────────────────────────────────────────────
export const taxRateApi = {
	list: async () => {
		const res = await request<XeroEnvelope<'TaxRates', TaxRate>>('/api/v1/tax-rates');
		return unwrap(res, 'TaxRates');
	},
	create: (payload: Partial<TaxRate>) =>
		request<{ TaxRates: TaxRate[] }>('/api/v1/tax-rates', {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: Partial<TaxRate>) =>
		request<{ TaxRates: TaxRate[] }>(`/api/v1/tax-rates/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	delete: (id: string) => request<void>(`/api/v1/tax-rates/${id}`, { method: 'DELETE' })
};

// ── Currencies ─────────────────────────────────────────────────────────────
export const currencyApi = {
	list: async () => {
		const res = await request<XeroEnvelope<'Currencies', Currency>>('/api/v1/currencies');
		return unwrap(res, 'Currencies');
	},
	create: (payload: Currency) =>
		request<{ Currencies: Currency[] }>('/api/v1/currencies', {
			method: 'POST',
			body: JSON.stringify(payload)
		})
};

// ── Quotes ─────────────────────────────────────────────────────────────────
export const quoteApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ Quotes: Quote[]; Pagination: Pagination }>(
			'/api/v1/quotes' + (qs ? `?${qs}` : '')
		);
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'Quotes', Quote>>(`/api/v1/quotes/${id}`);
		return unwrap(res, 'Quotes')[0];
	},
	create: (payload: Partial<Quote>) =>
		request<{ Quotes: Quote[] }>(`/api/v1/quotes`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	update: (id: string, payload: Partial<Quote>) =>
		request<{ Quotes: Quote[] }>(`/api/v1/quotes/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		}),
	delete: (id: string) => request<void>(`/api/v1/quotes/${id}`, { method: 'DELETE' })
};

// ── Manual journals ────────────────────────────────────────────────────────
export const manualJournalApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ ManualJournals: ManualJournal[]; Pagination: Pagination }>(
			'/api/v1/manual-journals' + (qs ? `?${qs}` : '')
		);
	},
	get: async (id: string) => {
		const res = await request<XeroEnvelope<'ManualJournals', ManualJournal>>(
			`/api/v1/manual-journals/${id}`
		);
		return unwrap(res, 'ManualJournals')[0];
	},
	create: (payload: Partial<ManualJournal>) =>
		request<{ ManualJournals: ManualJournal[] }>(`/api/v1/manual-journals`, {
			method: 'POST',
			body: JSON.stringify(payload)
		}),
	delete: (id: string) =>
		request<void>(`/api/v1/manual-journals/${id}`, { method: 'DELETE' })
};

// ── Bank feeds ────────────────────────────────────────────────────────────
export interface BankFeedConnection {
	FeedConnectionID: string;
	Provider: string;
	Status: string;
	InstitutionID?: string;
	InstitutionName?: string;
	Country?: string;
	ExternalReference?: string;
	AuthURL?: string;
	CreatedDateUTC?: string;
	UpdatedDateUTC?: string;
}
export interface BankFeedAccount {
	FeedAccountID: string;
	FeedConnectionID: string;
	AccountID?: string;
	ExternalAccountID: string;
	DisplayName?: string;
	IBAN?: string;
	CurrencyCode?: string;
	LastBalance?: number;
}
export interface BankFeedInstitution {
	ID: string;
	Name: string;
	BIC?: string;
	LogoURL?: string;
	Countries?: string[];
	TransactionTotalDays?: number;
}
export const bankFeedApi = {
	providers: () => request<{ Providers: string[] }>('/api/v1/bank-feeds/providers'),
	institutions: (provider: string, country = '') => {
		const qs = new URLSearchParams({ provider, country }).toString();
		return request<{ Institutions: BankFeedInstitution[] }>(
			`/api/v1/bank-feeds/institutions?${qs}`
		);
	},
	listConnections: () =>
		request<{ Connections: BankFeedConnection[]; Accounts: BankFeedAccount[] }>(
			'/api/v1/bank-feeds/connections'
		),
	createConnection: (payload: {
		provider: string;
		institutionId: string;
		institutionName?: string;
		redirectUrl?: string;
	}) =>
		request<{ Connections: BankFeedConnection[] }>('/api/v1/bank-feeds/connections', {
			method: 'POST',
			body: JSON.stringify({
				Provider: payload.provider,
				InstitutionID: payload.institutionId,
				InstitutionName: payload.institutionName ?? '',
				RedirectURL: payload.redirectUrl ?? ''
			})
		}),
	finalize: (id: string) =>
		request<{ Connections: BankFeedConnection[] }>(
			`/api/v1/bank-feeds/connections/${id}/finalize`,
			{ method: 'POST' }
		),
	sync: (id: string) =>
		request<{ Fetched: number; NewLines: number }>(
			`/api/v1/bank-feeds/connections/${id}/sync`,
			{ method: 'POST' }
		),
	deleteConnection: (id: string) =>
		request<void>(`/api/v1/bank-feeds/connections/${id}`, { method: 'DELETE' }),
	bindAccount: (feedAccountId: string, accountId: string | null) =>
		request<void>(
			`/api/v1/bank-feeds/accounts/${feedAccountId}`,
			{ method: 'PUT', body: JSON.stringify({ AccountID: accountId }) }
		)
};

// ── Organisation Files (inbox / archive) ───────────────────────────────────
export const orgFileApi = {
	list: (params: Record<string, string> = {}) => {
		const qs = new URLSearchParams(params).toString();
		return request<{ Files: OrgFile[]; Pagination: Pagination }>(
			'/api/v1/files' + (qs ? `?${qs}` : '')
		);
	},
	upload: (file: File, folder: 'inbox' | 'archive' = 'inbox') => {
		const fd = new FormData();
		fd.append('file', file);
		fd.append('folder', folder);
		return request<{ Files: OrgFile[] }>('/api/v1/files', { method: 'POST', body: fd });
	},
	move: (AttachmentIDs: string[], Folder: 'INBOX' | 'ARCHIVE') =>
		request<{ ok: boolean }>('/api/v1/files/move', {
			method: 'POST',
			body: JSON.stringify({ AttachmentIDs, Folder })
		}),
	delete: (AttachmentIDs: string[]) =>
		request<{ ok: boolean }>('/api/v1/files/delete', {
			method: 'POST',
			body: JSON.stringify({ AttachmentIDs })
		})
};

/** Download binary attachment (JWT + tenant header). */
export async function downloadOrgFileContent(attachmentId: string, filename: string): Promise<void> {
	if (!browser) return;
	const path = `/api/v1/attachments/${attachmentId}/content`;
	const isAuthPath = path.startsWith('/api/auth/');
	if (!isAuthPath && shouldPreemptivelyRefresh()) {
		await refreshAccessToken();
	}
	const doFetch = async (): Promise<Response> => {
		const s = get(session);
		const headers = new Headers();
		headers.set('Accept', '*/*');
		if (s.token) headers.set('Authorization', `Bearer ${s.token}`);
		if (s.tenantId && path.startsWith('/api/v1/')) headers.set('Xero-Tenant-Id', s.tenantId);
		return fetch(path, { headers });
	};
	let res = await doFetch();
	if (res.status === 401 && !isAuthPath) {
		const refreshed = await refreshAccessToken();
		if (refreshed) res = await doFetch();
	}
	if (!res.ok) {
		if (res.status === 401) {
			session.logout();
			goto('/login');
		}
		throw new Error(res.statusText || 'Download failed');
	}
	const blob = await res.blob();
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename || 'download';
	a.click();
	URL.revokeObjectURL(url);
}
