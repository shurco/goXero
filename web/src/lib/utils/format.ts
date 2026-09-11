import dayjs from 'dayjs';

export function formatCurrency(value: number | string | undefined, currency = 'USD') {
	const n = typeof value === 'string' ? Number(value) : (value ?? 0);
	try {
		return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(n);
	} catch {
		return `${currency} ${n.toFixed(2)}`;
	}
}

/**
 * Xero prints an amount held in the organisation's own currency as a bare
 * number — "7,430.22", not "US$7,430.22" — and names the currency only when
 * there is something to disambiguate. The shared formatCurrency always
 * prefixes, so the reconcile screen formats its own figures here rather than
 * changing what every other page shows.
 */
export function formatAmount(value: number | string | undefined) {
	const n = typeof value === 'string' ? Number(value) : (value ?? 0);
	return new Intl.NumberFormat(undefined, {
		minimumFractionDigits: 2,
		maximumFractionDigits: 2
	}).format(Number.isFinite(n) ? n : 0);
}

/**
 * The results-table form: the bare number with the currency code stuck to its
 * right, no space between — Xero's Find & match table prints "250.00USD".
 */
export function formatAmountCode(value: number | string | undefined, currency = 'USD') {
	return formatAmount(value) + currency;
}

/**
 * The totals form: the code first, then a space, then the number — Xero's
 * "USD 100.00" under step 3.
 */
export function formatCodeAmount(value: number | string | undefined, currency = 'USD') {
	return `${currency} ${formatAmount(value)}`;
}

export function formatDate(value: string | undefined, pattern = 'DD MMM YYYY') {
	if (!value) return '—';
	const d = dayjs(value);
	return d.isValid() ? d.format(pattern) : '—';
}

/**
 * Xero writes September as "Sept" — on a chart axis, in a tooltip heading, and
 * on a bank account card — while dayjs knows the month only as "Sep", so the
 * abbreviation is corrected on the way out of the shared date formatter.
 */
export function xeroDate(value: string | undefined, pattern = 'D MMM') {
	return formatDate(value, pattern).replace(/\bSep\b/, 'Sept');
}

export function statusClass(status: string | undefined) {
	switch ((status || '').toUpperCase()) {
		case 'DRAFT':      return 'badge-draft';
		case 'SUBMITTED':  return 'badge-submitted';
		case 'AUTHORISED': return 'badge-authorised';
		case 'SENT':       return 'badge-sent';
		case 'ACCEPTED':   return 'badge-paid';
		case 'DECLINED':   return 'badge-overdue';
		case 'INVOICED':   return 'badge-active';
		case 'PAID':       return 'badge-paid';
		case 'OVERDUE':    return 'badge-overdue';
		case 'VOIDED':
		case 'DELETED':
		case 'ARCHIVED':   return 'badge-archived';
		case 'ACTIVE':     return 'badge-active';
		default:           return 'badge-draft';
	}
}

/** Human-readable invoice/quote status label */
export function statusLabel(status: string | undefined) {
	switch ((status || '').toUpperCase()) {
		case 'AUTHORISED': return 'Awaiting payment';
		case 'SUBMITTED':  return 'Awaiting approval';
		case 'PAID':       return 'Paid';
		case 'DRAFT':      return 'Draft';
		case 'VOIDED':     return 'Voided';
		case 'DELETED':    return 'Deleted';
		case 'SENT':       return 'Sent';
		case 'ACCEPTED':   return 'Accepted';
		case 'DECLINED':   return 'Declined';
		case 'INVOICED':   return 'Invoiced';
		default:           return status ?? '';
	}
}
