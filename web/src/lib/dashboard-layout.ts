import { browser } from '$app/environment';
import type { BankTile } from './dashboard-types';

export const STATIC_WIDGET_IDS = [
	'bills-pay',
	'net-profit',
	'tasks',
	'recent-payments',
	'expenses-review',
	'invoices-owed',
	'cash-in-out',
	'coa-watchlist'
] as const;

export type StaticWidgetId = (typeof STATIC_WIDGET_IDS)[number];

export function bankWidgetId(accountId: string): string {
	return `bank:${accountId}`;
}

export function parseBankWidgetId(id: string): string | null {
	if (!id.startsWith('bank:')) return null;
	return id.slice(5) || null;
}

/** Fixed tile heights on the home dashboard (Xero-style grid). */
const DASHBOARD_WIDGET_LARGE = new Set<string>([
	'net-profit',
	'recent-payments',
	'cash-in-out',
	'coa-watchlist'
]);

/**
 * Tailwind height class: small tiles 251px, large 522px.
 * First bank account tile is large; other bank tiles are small.
 */
export function dashboardWidgetHeightClass(
	wid: string,
	bankTileIndex: (accountId: string) => number
): 'h-[251px]' | 'h-[522px]' {
	const aid = parseBankWidgetId(wid);
	if (aid !== null) {
		return bankTileIndex(aid) === 0 ? 'h-[522px]' : 'h-[251px]';
	}
	return DASHBOARD_WIDGET_LARGE.has(wid) ? 'h-[522px]' : 'h-[251px]';
}

export interface DashboardLayoutV1 {
	version: 1;
	order: string[];
	/** Widget id → hidden */
	hidden: Record<string, boolean>;
}

const KEY_PREFIX = 'goxero.dashboardLayout.';

function storageKey(tenantId: string): string {
	return `${KEY_PREFIX}${tenantId}`;
}

/**
 * Row-major order over the legacy three columns so a CSS `grid-cols-3` matches
 * the old flex layout (col1 / col2 / col3, top to bottom per row).
 */
export function defaultOrderFromTiles(tiles: BankTile[]): string[] {
	const col1: string[] = [];
	const col2: string[] = [];
	const col3: string[] = [];
	if (tiles[0]) col1.push(bankWidgetId(tiles[0].account.AccountID));
	for (let i = 2; i < tiles.length; i++) {
		col1.push(bankWidgetId(tiles[i]!.account.AccountID));
	}
	col1.push('bills-pay', 'net-profit');
	if (tiles[1]) col2.push(bankWidgetId(tiles[1].account.AccountID));
	col2.push('tasks', 'recent-payments', 'expenses-review');
	col3.push('invoices-owed', 'cash-in-out', 'coa-watchlist');
	const cols = [col1, col2, col3];
	const max = Math.max(col1.length, col2.length, col3.length, 1);
	const order: string[] = [];
	for (let r = 0; r < max; r++) {
		for (let c = 0; c < 3; c++) {
			const id = cols[c]![r];
			if (id) order.push(id);
		}
	}
	return order;
}

function allIdsForTiles(tiles: BankTile[]): Set<string> {
	const s = new Set<string>(STATIC_WIDGET_IDS);
	for (const t of tiles) {
		s.add(bankWidgetId(t.account.AccountID));
	}
	return s;
}

/** Merge saved order with new bank accounts; drop unknown ids. */
export function reconcileOrder(saved: string[] | undefined, tiles: BankTile[]): string[] {
	const valid = allIdsForTiles(tiles);
	const def = defaultOrderFromTiles(tiles);
	if (!saved?.length) return def;

	const seen = new Set<string>();
	const out: string[] = [];
	for (const id of saved) {
		if (valid.has(id) && !seen.has(id)) {
			out.push(id);
			seen.add(id);
		}
	}
	for (const id of def) {
		if (!seen.has(id)) {
			out.push(id);
			seen.add(id);
		}
	}
	return out;
}

export function loadLayout(tenantId: string): DashboardLayoutV1 | null {
	if (!browser) return null;
	try {
		const raw = localStorage.getItem(storageKey(tenantId));
		if (!raw) return null;
		const p = JSON.parse(raw) as Partial<DashboardLayoutV1>;
		if (p.version !== 1 || !Array.isArray(p.order)) return null;
		return {
			version: 1,
			order: p.order,
			hidden: typeof p.hidden === 'object' && p.hidden ? { ...p.hidden } : {}
		};
	} catch {
		return null;
	}
}

export function saveLayout(tenantId: string, layout: DashboardLayoutV1): void {
	if (!browser) return;
	try {
		localStorage.setItem(storageKey(tenantId), JSON.stringify(layout));
	} catch {
		/* quota */
	}
}
