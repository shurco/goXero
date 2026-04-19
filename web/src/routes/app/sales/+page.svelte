<script lang="ts">
	import { onMount } from 'svelte';
	import { invoiceApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate, statusClass } from '$lib/utils/format';
	import type { Invoice, InvoiceSummary, Organisation } from '$lib/types';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let summary = $state<InvoiceSummary | null>(null);
	let recent = $state<Invoice[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);

	async function reload() {
		loading = true;
		try {
			const [s, list, o] = await Promise.all([
				invoiceApi.summary().catch(() => null),
				invoiceApi.list({ pageSize: '8', type: 'ACCREC' }).catch(() => null),
				orgApi.current().catch(() => null)
			]);
			summary = s ?? null;
			recent = list?.Invoices ?? [];
			org = o ?? null;
		} finally {
			loading = false;
		}
	}
	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	const currency = $derived(org?.BaseCurrency || 'USD');
	const tiles = $derived([
		{ label: 'Draft', value: summary?.draft ?? 0, href: '/app/invoices?status=DRAFT', color: 'bg-ink-400' },
		{ label: 'Awaiting payment', value: summary?.authorised ?? 0, href: '/app/invoices?status=AUTHORISED', color: 'bg-brand-500' },
		{ label: 'Paid', value: summary?.paid ?? 0, href: '/app/invoices?status=PAID', color: 'bg-emerald-500' },
		{ label: 'Overdue', value: summary?.overdue ?? 0, href: '/app/invoices?overdue=1', color: 'bg-red-500' }
	]);
</script>

<ModuleHeader
	title="Sales overview"
	subtitle={org?.Name ?? ''}
	primary={{ label: 'New invoice', href: '/app/invoices/new' }}
	secondary={{ label: 'New quote', href: '/app/sales/quotes/new' }}
/>

<div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
	{#each tiles as t}
		<a href={t.href} class="card p-5 block hover:shadow-pop transition">
			<div class="flex items-center gap-2">
				<span class="h-2 w-2 rounded-full {t.color}"></span>
				<span class="text-sm muted">{t.label}</span>
			</div>
			<div class="mt-2 text-2xl font-semibold tabular-nums">{loading ? '—' : t.value}</div>
		</a>
	{/each}
</div>

<div class="card p-5">
	<div class="flex items-center justify-between mb-4">
		<h2 class="content-section-title">Recent invoices</h2>
		<a href="/app/invoices" class="text-sm text-brand-600 hover:underline">View all</a>
	</div>
	<table class="table-auto-xero">
		<thead>
			<tr>
				<th>Number</th>
				<th>Contact</th>
				<th>Date</th>
				<th class="text-right">Amount</th>
				<th>Status</th>
			</tr>
		</thead>
		<tbody>
			{#each recent as inv}
				<tr>
					<td class="font-medium"><a href="/app/invoices/{inv.InvoiceID}" class="hover:text-brand-700">{inv.InvoiceNumber || '—'}</a></td>
					<td>{inv.Contact?.Name ?? '—'}</td>
					<td>{formatDate(inv.Date)}</td>
					<td class="text-right tabular-nums">{formatCurrency(inv.Total, inv.CurrencyCode || currency)}</td>
					<td><span class={statusClass(inv.Status)}>{inv.Status}</span></td>
				</tr>
			{/each}
			{#if !loading && recent.length === 0}
				<tr><td colspan="5" class="text-center py-8 muted">No invoices yet.</td></tr>
			{/if}
		</tbody>
	</table>
</div>
