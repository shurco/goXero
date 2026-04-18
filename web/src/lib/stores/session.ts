import { browser } from '$app/environment';
import { writable } from 'svelte/store';
import type { TenantSummary } from '../types';

const STORAGE_KEY = 'goxero.session';

interface SessionState {
	token: string | null;
	refreshToken: string | null;
	/**
	 * Epoch milliseconds at which the access token expires. Null when no
	 * token is loaded. Clients compare against `Date.now()` to decide whether
	 * a proactive refresh is needed before firing a request.
	 */
	expiresAt: number | null;
	/** Epoch milliseconds at which the refresh token itself expires. */
	refreshExpiresAt: number | null;
	email: string | null;
	firstName?: string;
	lastName?: string;
	tenantId: string | null;
	tenants: TenantSummary[];
}

const EMPTY: SessionState = {
	token: null,
	refreshToken: null,
	expiresAt: null,
	refreshExpiresAt: null,
	email: null,
	tenantId: null,
	tenants: []
};

function load(): SessionState {
	if (!browser) return EMPTY;
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (!raw) return EMPTY;
		return { ...EMPTY, ...JSON.parse(raw) };
	} catch {
		return EMPTY;
	}
}

function persist(s: SessionState) {
	if (!browser) return;
	if (!s.token) {
		localStorage.removeItem(STORAGE_KEY);
	} else {
		localStorage.setItem(STORAGE_KEY, JSON.stringify(s));
	}
}

/** Parse an ISO timestamp returned by the Go backend into epoch millis. */
function parseServerTime(raw: string | undefined | null): number | null {
	if (!raw) return null;
	const t = Date.parse(raw);
	return Number.isFinite(t) ? t : null;
}

/** Input accepted by `session.login` — mirrors the `authResponse` DTO. */
interface LoginPayload {
	token: string;
	refreshToken: string;
	expiresAt: string;
	refreshTokenExpiresAt: string;
	email: string;
	firstName?: string;
	lastName?: string;
	tenants: TenantSummary[];
}

function createSession() {
	const { subscribe, set, update } = writable<SessionState>(load());
	return {
		subscribe,
		login(data: LoginPayload) {
			const s: SessionState = {
				token: data.token,
				refreshToken: data.refreshToken,
				expiresAt: parseServerTime(data.expiresAt),
				refreshExpiresAt: parseServerTime(data.refreshTokenExpiresAt),
				email: data.email,
				firstName: data.firstName,
				lastName: data.lastName,
				tenants: data.tenants || [],
				tenantId: (data.tenants || [])[0]?.organisationId ?? null
			};
			persist(s);
			set(s);
		},
		/**
		 * Applies a freshly rotated access+refresh pair without touching the
		 * rest of the session (tenants, user details). Used by the API layer's
		 * automatic refresh path.
		 */
		setTokens(tokens: {
			token: string;
			refreshToken: string;
			expiresAt: string;
			refreshTokenExpiresAt: string;
		}) {
			update((s) => {
				const updated: SessionState = {
					...s,
					token: tokens.token,
					refreshToken: tokens.refreshToken,
					expiresAt: parseServerTime(tokens.expiresAt),
					refreshExpiresAt: parseServerTime(tokens.refreshTokenExpiresAt)
				};
				persist(updated);
				return updated;
			});
		},
		setTenant(tenantId: string) {
			update((s) => {
				const updated = { ...s, tenantId };
				persist(updated);
				return updated;
			});
		},
		updateTenants(tenants: TenantSummary[]) {
			update((s) => {
				const updated = {
					...s,
					tenants,
					tenantId: s.tenantId && tenants.some((t) => t.organisationId === s.tenantId)
						? s.tenantId
						: tenants[0]?.organisationId ?? null
				};
				persist(updated);
				return updated;
			});
		},
		logout() {
			persist(EMPTY);
			set(EMPTY);
		}
	};
}

export const session = createSession();
