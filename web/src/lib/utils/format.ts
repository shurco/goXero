import dayjs from 'dayjs';

export function formatCurrency(value: number | string | undefined, currency = 'USD') {
	const n = typeof value === 'string' ? Number(value) : (value ?? 0);
	try {
		return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(n);
	} catch {
		return `${currency} ${n.toFixed(2)}`;
	}
}

export function formatDate(value: string | undefined, pattern = 'DD MMM YYYY') {
	if (!value) return '—';
	const d = dayjs(value);
	return d.isValid() ? d.format(pattern) : '—';
}

export function statusClass(status: string | undefined) {
	switch ((status || '').toUpperCase()) {
		case 'DRAFT':      return 'badge-draft';
		case 'SUBMITTED':  return 'badge-draft';
		case 'AUTHORISED': return 'badge-authorised';
		case 'PAID':       return 'badge-paid';
		case 'OVERDUE':    return 'badge-overdue';
		case 'VOIDED':
		case 'DELETED':
		case 'ARCHIVED':   return 'badge-archived';
		case 'ACTIVE':     return 'badge-active';
		default:           return 'badge-draft';
	}
}
