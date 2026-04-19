import type { Account } from '$lib/types';

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

const ASSET = new Set([
	'BANK',
	'CURRENT',
	'FIXED',
	'INVENTORY',
	'NONCURRENT',
	'PREPAYMENT'
]);
const LIABILITY = new Set([
	'CURRLIAB',
	'LIABILITY',
	'TERMLIAB',
	'PAYGLIABILITY',
	'SUPERANNUATIONLIABILITY'
]);
const EXPENSE = new Set(['EXPENSE', 'OVERHEADS', 'DEPRECIATN', 'DIRECTCOSTS', 'WAGESEXPENSE']);
const REV = new Set(['REVENUE', 'SALES']);

/** YTD column — illustrative values until GL balances are exposed via API (aligned with US Xero-style demo chart codes). */
export const YTD_BY_CODE: Record<string, number> = {
	'090': 2045,
	'091': 0,
	'120': 9542.39,
	'130': 0,
	'140': 0,
	'150': 1475,
	'151': -525,
	'160': 850,
	'200': 10254.11,
	'210': 0,
	'220': 3477.11,
	'260': 0,
	'310': 0,
	'320': 0,
	'400': 28942.57,
	'460': 0,
	'500': 2345.22,
	'600': 1043.12,
	'604': 20,
	'608': 150,
	'612': 80,
	'620': 125,
	'624': 225,
	'632': 250,
	'668': 12500,
	'680': 150,
	'684': 1123.5,
	'800': 240.12
};

export function ytdForAccount(a: Account): number | null {
	const v = YTD_BY_CODE[a.Code?.trim()];
	return v !== undefined ? v : null;
}

export function displayTypeColumn(a: Account): string {
	const code = a.Code?.trim();
	const name = (a.Name || '').toLowerCase();
	if (name === 'gst' || name.includes('sales tax')) return 'GST';
	const t = a.Type;
	if (t === 'BANK') return 'Bank';
	if (t === 'CURRENT') return 'Current Asset';
	if (t === 'FIXED') return 'Fixed Asset';
	if (t === 'CURRLIAB') return 'Current Liability';
	if (t === 'EQUITY') return 'Equity';
	if (t === 'REVENUE' || t === 'SALES') return 'Revenue';
	if (EXPENSE.has(t) || t === 'EXPENSE' || t === 'OVERHEADS') return 'Expense';
	if (ACCOUNT_TYPE_OPTIONS.find((o) => o.value === t)) {
		return ACCOUNT_TYPE_OPTIONS.find((o) => o.value === t)!.label;
	}
	return t;
}

export function tabMatches(tab: CoaTabId, a: Account): boolean {
	if (tab === 'archive') return a.Status === 'ARCHIVED';
	if (a.Status === 'ARCHIVED') return false;
	const t = a.Type;
	switch (tab) {
		case 'all':
			return true;
		case 'assets':
			return ASSET.has(t);
		case 'liabilities':
			return LIABILITY.has(t);
		case 'equity':
			return t === 'EQUITY';
		case 'expenses':
			return EXPENSE.has(t);
		case 'revenue':
			return REV.has(t);
		default:
			return true;
	}
}

/** Rows to offer in “Import standard chart” — aligns with US Xero-style demo (see migrations/data/ChartOfAccounts.csv). */
export const STANDARD_CHART_IMPORT: Partial<Account>[] = [
	{
		Code: '090',
		Name: 'Checking Account',
		Type: 'BANK',
		Status: 'ACTIVE',
		BankAccountNumber: '12-3456-7890123-00'
	},
	{ Code: '091', Name: 'Savings Account', Type: 'BANK', Status: 'ACTIVE' },
	{ Code: '120', Name: 'Accounts Receivable', Type: 'CURRENT', Status: 'ACTIVE' },
	{ Code: '130', Name: 'Prepayments', Type: 'CURRENT', Status: 'ACTIVE' },
	{ Code: '150', Name: 'Office Equipment', Type: 'FIXED', Status: 'ACTIVE' },
	{
		Code: '151',
		Name: 'Less Accumulated Depreciation on Office Equipment',
		Type: 'FIXED',
		Status: 'ACTIVE'
	},
	{ Code: '200', Name: 'Accounts Payable', Type: 'CURRLIAB', Status: 'ACTIVE' },
	{ Code: '220', Name: 'Sales Tax', Type: 'CURRLIAB', Status: 'ACTIVE' },
	{ Code: '400', Name: 'Sales', Type: 'SALES', Status: 'ACTIVE' },
	{ Code: '600', Name: 'Advertising', Type: 'EXPENSE', Status: 'ACTIVE' },
	{ Code: '620', Name: 'Entertainment', Type: 'EXPENSE', Status: 'ACTIVE' },
	{ Code: '628', Name: 'General Expenses', Type: 'EXPENSE', Status: 'ACTIVE' }
];
