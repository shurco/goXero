import type { Account, Report, ReportRow } from '$lib/types';

export type CoaTabId =
	| 'all'
	| 'assets'
	| 'liabilities'
	| 'equity'
	| 'expenses'
	| 'revenue'
	| 'archive';

export const COA_TABS: { id: CoaTabId; label: string }[] = [
	{ id: 'all', label: 'All Accounts' },
	{ id: 'assets', label: 'Assets' },
	{ id: 'liabilities', label: 'Liabilities' },
	{ id: 'equity', label: 'Equity' },
	{ id: 'expenses', label: 'Expenses' },
	{ id: 'revenue', label: 'Revenue' },
	{ id: 'archive', label: 'Archive' }
];

/** Xero API account types available when creating accounts. */
export const ACCOUNT_TYPE_OPTIONS: { value: string; label: string }[] = [
	{ value: 'BANK', label: 'Bank' },
	{ value: 'CURRENT', label: 'Current Asset' },
	{ value: 'FIXED', label: 'Fixed Asset' },
	{ value: 'INVENTORY', label: 'Inventory' },
	{ value: 'NONCURRENT', label: 'Non-current Asset' },
	{ value: 'PREPAYMENT', label: 'Prepayment' },
	{ value: 'CURRLIAB', label: 'Current Liability' },
	{ value: 'LIABILITY', label: 'Liability' },
	{ value: 'TERMLIAB', label: 'Non-current Liability' },
	{ value: 'PAYGLIABILITY', label: 'PAYG Liability' },
	{ value: 'SUPERANNUATIONLIABILITY', label: 'Superannuation Liability' },
	{ value: 'EQUITY', label: 'Equity' },
	{ value: 'REVENUE', label: 'Revenue' },
	{ value: 'SALES', label: 'Sales' },
	{ value: 'EXPENSE', label: 'Expense' },
	{ value: 'OVERHEADS', label: 'Overheads' },
	{ value: 'DEPRECIATN', label: 'Depreciation' },
	{ value: 'DIRECTCOSTS', label: 'Direct Costs' },
	{ value: 'WAGESEXPENSE', label: 'Wages' }
];

/**
 * YTD balances for the Accounts screen, read from the Trial Balance rather than
 * carried here.
 *
 * Xero's chart of accounts prints a YTD column, and this file used to answer it
 * with a `YTD_BY_CODE` table: thirty-odd balances written into the source and
 * describing no organisation's books at all ("illustrative values", its own
 * comment said). A made-up figure in a column headed YTD is worse than an empty
 * one, because nothing distinguishes it from a real one, and the ledger already
 * answers the question.
 *
 * The answer used is the Trial Balance, which is where this application measures
 * YTD and the one report whose per-account figures are checked against Xero's own
 * capture (`docs/reports-xero-parity-summary.md`). Its third and fourth figures
 * are Xero's YTD Debit and YTD Credit: the year-to-date movement for a profit and
 * loss account, the balance carried as at the report date for a balance sheet
 * account.
 *
 * Xero prints that column unsigned — of the 58 accounts in its captured chart, 27
 * carry a non-zero YTD and all 27 are positive, including the twelve that are
 * credit balances (`migrations/data/xero/accounts.csv`, e.g. Accounts Payable
 * 8,386.76 and Sales Tax 422.59). The magnitude is therefore what is printed, so
 * that this column reads as Xero's does. An account the Trial Balance does not
 * list has no movement to show and is left blank rather than shown as zero.
 */
export function ytdByCode(report: Report | undefined): Record<string, number> {
	const out: Record<string, number> = {};
	for (const row of flattenReportRows(report?.Rows ?? [])) {
		const cells = (row.Cells ?? []).map((c) => c.Value ?? '');
		if (cells.length < 5) continue;
		const code = codeFromAccountCell(cells[0]);
		if (!code) continue;
		out[code] = Math.abs(printedAmount(cells[3]) - printedAmount(cells[4]));
	}
	return out;
}

/** Every row of a report, at whatever depth Xero nested it. */
function flattenReportRows(rows: ReportRow[]): ReportRow[] {
	return rows.flatMap((row) => [row, ...flattenReportRows(row.Rows ?? [])]);
}

/** Xero names a Trial Balance account "Accounts Receivable (610)". */
function codeFromAccountCell(cell: string): string {
	const match = /\(([^()]+)\)\s*$/.exec(cell.trim());
	return match ? match[1].trim() : '';
}

/** A printed figure as a number: separators dropped, parentheses as negative. */
function printedAmount(cell: string): number {
	const text = cell.trim().replace(/,/g, '');
	const bracketed = text.startsWith('(') && text.endsWith(')');
	const value = Number(bracketed ? text.slice(1, -1) : text);
	if (!Number.isFinite(value)) return 0;
	return bracketed ? -value : value;
}

/**
 * The classes a balance sheet is made of — Xero's Account.Class, ASSET /
 * LIABILITY / EQUITY, as opposed to the profit and loss classes. The server
 * derives Class from the account's type (models.AccountClassForType) and sends
 * it, so this is the same value the tabs below read.
 */
export const BALANCE_SHEET_CLASSES = ['ASSET', 'LIABILITY', 'EQUITY'];

/**
 * The label Xero's chart of accounts prints in its Type column.
 *
 * The two rows that are not simply the account's type come from the server's own
 * facts rather than from the name: a tax account is the one the organisation
 * tagged with the GST role, and every profit-and-loss expense type (Overheads,
 * Depreciation, Direct Costs, Wages) is printed as "Expense". Both used to be
 * re-derived here — the first by matching the account's *name* against "GST" and
 * "sales tax", which mislabels any other account so named and misses a tax
 * account called anything else.
 */
export function displayTypeColumn(a: Account): string {
	if (a.SystemAccount === 'GST') return 'GST';
	const t = a.Type;
	if (t === 'BANK') return 'Bank';
	if (t === 'CURRENT') return 'Current Asset';
	if (t === 'FIXED') return 'Fixed Asset';
	if (t === 'CURRLIAB') return 'Current Liability';
	if (t === 'EQUITY') return 'Equity';
	if (t === 'REVENUE' || t === 'SALES') return 'Revenue';
	if (a.Class === 'EXPENSE') return 'Expense';
	return ACCOUNT_TYPE_OPTIONS.find((o) => o.value === t)?.label ?? t;
}

export function tabMatches(tab: CoaTabId, a: Account): boolean {
	if (tab === 'archive') return a.Status === 'ARCHIVED';
	if (a.Status === 'ARCHIVED') return false;
	switch (tab) {
		case 'all':
			return true;
		case 'assets':
			return a.Class === 'ASSET';
		case 'liabilities':
			return a.Class === 'LIABILITY';
		case 'equity':
			return a.Class === 'EQUITY';
		case 'expenses':
			return a.Class === 'EXPENSE';
		case 'revenue':
			return a.Class === 'REVENUE';
		default:
			return true;
	}
}
