import type { BankTransaction, Invoice } from '$lib/types';

export function num(v: string | number | undefined | null): number {
	if (v === undefined || v === null) return 0;
	return typeof v === 'number' ? v : Number(v) || 0;
}

export function bankBalancesFromTransactions(
	txs: BankTransaction[],
	currency: string
): { statement: number; xero: number } {
	let statement = 0;
	let xero = 0;
	for (const t of txs) {
		const raw = num(t.Total);
		const signed = t.Type === 'SPEND' || t.Type?.startsWith('SPEND') ? -Math.abs(raw) : Math.abs(raw);
		statement += signed;
		if (t.IsReconciled) xero += signed;
	}
	return { statement, xero };
}

/** ACCREC buckets by due-date age for a simple bar display */
export function invoiceAgingBuckets(invoices: Invoice[], type: 'ACCREC' | 'ACCPAY') {
	const list = invoices.filter((i) => i.Type === type && i.Status === 'AUTHORISED');
	const now = new Date();
	const startOfWeek = new Date(now);
	startOfWeek.setDate(now.getDate() - now.getDay());
	const weekEnd = new Date(startOfWeek);
	weekEnd.setDate(startOfWeek.getDate() + 7);

	let older = 0;
	let thisWeek = 0;
	let nextWeek = 0;
	let later = 0;

	for (const inv of list) {
		const due = inv.DueDate ? new Date(inv.DueDate) : now;
		const amt = num(inv.AmountDue);
		if (due < startOfWeek) older += amt;
		else if (due < weekEnd) thisWeek += amt;
		else if (due < new Date(weekEnd.getTime() + 7 * 86400000)) nextWeek += amt;
		else later += amt;
	}

	const parts = [
		{ key: 'older', label: 'Older', value: older },
		{ key: 'week', label: 'This week', value: thisWeek },
		{ key: 'next', label: 'Next', value: nextWeek },
		{ key: 'later', label: 'Later', value: later }
	];
	const max = Math.max(1, ...parts.map((p) => p.value));
	return { parts, max };
}

export function monthlyReceiveSpend(
	txs: BankTransaction[],
	months = 6
): { label: string; in: number; out: number }[] {
	const out: { label: string; in: number; out: number }[] = [];
	const now = new Date();
	for (let i = months - 1; i >= 0; i--) {
		const d = new Date(now.getFullYear(), now.getMonth() - i, 1);
		const label = d.toLocaleString(undefined, { month: 'short' });
		out.push({ label, in: 0, out: 0 });
	}

	for (const t of txs) {
		if (!t.Date) continue;
		const dt = new Date(t.Date);
		const idx = months - 1 - (now.getFullYear() - dt.getFullYear()) * 12 - (now.getMonth() - dt.getMonth());
		if (idx < 0 || idx >= months) continue;
		const n = num(t.Total);
		if (t.Type === 'RECEIVE' || t.Type?.startsWith('RECEIVE')) out[idx]!.in += Math.abs(n);
		else if (t.Type === 'SPEND' || t.Type?.startsWith('SPEND')) out[idx]!.out += Math.abs(n);
	}
	return out;
}

export function ytdPaidTotals(invoices: Invoice[], year: number) {
	let income = 0;
	let bills = 0;
	for (const inv of invoices) {
		if (inv.Status !== 'PAID' || !inv.Date) continue;
		const d = new Date(inv.Date);
		if (d.getFullYear() !== year) continue;
		if (inv.Type === 'ACCREC') income += num(inv.Total);
		if (inv.Type === 'ACCPAY') bills += num(inv.Total);
	}
	return { income, bills };
}
