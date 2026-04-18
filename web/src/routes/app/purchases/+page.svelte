<script lang="ts">
	import { onMount } from 'svelte';
	import { invoiceApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate, statusClass } from '$lib/utils/format';
	import type { Invoice, Organisation } from '$lib/types';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let bills = $state<Invoice[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);

	async function reload() {
		loading = true;
		try {
			const [list, o] = await Promise.all([
				invoiceApi.list({ type: 'ACCPAY', pageSize: '10' }).catch(() => null),
				orgApi.current().catch(() => null)
			]);
			bills = list?.Invoices ?? [];
			org = o ?? null;
		} finally {
			loading = false;
		}
	}
	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	const currency = $derived(org?.BaseCurrency || 'USD');

	const totals = $derived({
		draft: bills.filter((b) => b.Status === 'DRAFT').length,
		authorised: bills.filter((b) => b.Status === 'AUTHORISED').length,
		paid: bills.filter((b) => b.Status === 'PAID').length
	});
</script>

<ModuleHeader
	title="Purchases overview"
	subtitle={org?.Name ?? ''}
	primary={{ label: 'New bill', href: '/app/purchases/bills/new' }}
	secondary={{ label: 'New purchase order', href: '/app/purchases/orders/new' }}
/>

<div class="grid grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
	<a class="card p-5 block hover:shadow-pop transition" href="/app/purchases/bills?status=DRAFT">
		<div class="text-sm muted">Draft bills</div>
		<div class="mt-2 text-2xl font-semibold">{loading ? '—' : totals.draft}</div>
	</a>
	<a class="card p-5 block hover:shadow-pop transition" href="/app/purchases/bills?status=AUTHORISED">
		<div class="text-sm muted">Awaiting payment</div>
		<div class="mt-2 text-2xl font-semibold">{loading ? '—' : totals.authorised}</div>
	</a>
	<a class="card p-5 block hover:shadow-pop transition" href="/app/purchases/bills?status=PAID">
		<div class="text-sm muted">Paid (recent)</div>
		<div class="mt-2 text-2xl font-semibold">{loading ? '—' : totals.paid}</div>
	</a>
</div>

<div class="card p-5">
	<div class="flex items-center justify-between mb-4">
		<h2 class="text-lg font-semibold text-ink-900">Recent bills</h2>
		<a href="/app/purchases/bills" class="text-sm text-brand-600 hover:underline">View all</a>
	</div>
	<table class="table-auto-xero">
		<thead>
			<tr>
				<th>Reference</th>
				<th>Supplier</th>
				<th>Due</th>
				<th class="text-right">Amount</th>
				<th>Status</th>
			</tr>
		</thead>
		<tbody>
			{#each bills as b}
				<tr>
					<td class="font-medium"><a href="/app/invoices/{b.InvoiceID}" class="hover:text-brand-700">{b.InvoiceNumber || '—'}</a></td>
					<td>{b.Contact?.Name ?? '—'}</td>
					<td>{formatDate(b.DueDate)}</td>
					<td class="text-right tabular-nums">{formatCurrency(b.Total, b.CurrencyCode || currency)}</td>
					<td><span class={statusClass(b.Status)}>{b.Status}</span></td>
				</tr>
			{/each}
			{#if !loading && bills.length === 0}
				<tr><td colspan="5" class="text-center py-8 muted">No bills yet.</td></tr>
			{/if}
		</tbody>
	</table>
</div>
