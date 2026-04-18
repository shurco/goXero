<script lang="ts">
	import { invoiceApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate, statusClass } from '$lib/utils/format';
	import type { Invoice, Pagination } from '$lib/types';
	import { onMount } from 'svelte';

	let loading = $state(true);
	let invoices = $state<Invoice[]>([]);
	let pagination = $state<Pagination>({ page: 1, pageSize: 25, total: 0 });
	let statusFilter = $state('');
	let typeFilter = $state('ACCREC');
	let search = $state('');

	async function reload() {
		loading = true;
		try {
			const params: Record<string, string> = {
				page: String(pagination.page),
				pageSize: String(pagination.pageSize)
			};
			if (statusFilter) params.status = statusFilter;
			if (typeFilter) params.type = typeFilter;
			if (search) params.search = search;

			const res = await invoiceApi.list(params);
			invoices = res?.Invoices ?? [];
			pagination = res?.Pagination ?? pagination;
		} catch {
			invoices = [];
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	$effect(() => {
		if ($session.tenantId) void reload();
	});

	function applyFilters() {
		pagination.page = 1;
		reload();
	}

	function setPage(p: number) {
		pagination.page = p;
		reload();
	}

	const totalPages = $derived(Math.max(1, Math.ceil(pagination.total / pagination.pageSize)));
</script>

<div class="space-y-6">
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<h1 class="section-title">Invoices</h1>
			<p class="muted">Manage sales invoices and bills.</p>
		</div>
		<div class="flex gap-2">
			<a href="/app/invoices/new" class="btn-primary">+ New invoice</a>
		</div>
	</div>

	<!-- Tabs -->
	<div class="flex gap-6 border-b border-ink-100 text-sm">
		{#each [
			{ v: 'ACCREC', label: 'Sales invoices' },
			{ v: 'ACCPAY', label: 'Bills to pay' }
		] as t}
			<button
				class="pb-3 -mb-px border-b-2 {typeFilter === t.v ? 'border-brand-500 text-brand-700 font-semibold' : 'border-transparent text-ink-600 hover:text-ink-900'}"
				onclick={() => { typeFilter = t.v; applyFilters(); }}
			>
				{t.label}
			</button>
		{/each}
	</div>

	<!-- Filters -->
	<div class="card p-4 flex flex-wrap items-center gap-3">
		<input class="input w-60" placeholder="Search number or reference…" bind:value={search} onkeydown={(e) => e.key === 'Enter' && applyFilters()} />
		<select class="select w-48" bind:value={statusFilter} onchange={applyFilters}>
			<option value="">All statuses</option>
			<option value="DRAFT">Draft</option>
			<option value="SUBMITTED">Submitted</option>
			<option value="AUTHORISED">Authorised</option>
			<option value="PAID">Paid</option>
			<option value="VOIDED">Voided</option>
		</select>
		<button class="btn-secondary" onclick={applyFilters}>Apply</button>
		<div class="ml-auto muted text-sm">Total: {pagination.total}</div>
	</div>

	<!-- Table -->
	<div class="card overflow-x-auto">
		<table class="table-auto-xero">
			<thead>
				<tr>
					<th>Number</th>
					<th>Contact</th>
					<th>Reference</th>
					<th>Date</th>
					<th>Due</th>
					<th class="text-right">Total</th>
					<th class="text-right">Due</th>
					<th>Status</th>
				</tr>
			</thead>
			<tbody>
				{#each invoices as inv}
					<tr>
						<td class="font-medium text-ink-900">
							<a class="hover:text-brand-700" href="/app/invoices/{inv.InvoiceID}">{inv.InvoiceNumber || '—'}</a>
						</td>
						<td>{inv.Contact?.Name ?? '—'}</td>
						<td class="muted">{inv.Reference || '—'}</td>
						<td>{formatDate(inv.Date)}</td>
						<td>{formatDate(inv.DueDate)}</td>
						<td class="text-right tabular-nums">{formatCurrency(inv.Total, inv.CurrencyCode)}</td>
						<td class="text-right tabular-nums">{formatCurrency(inv.AmountDue, inv.CurrencyCode)}</td>
						<td><span class={statusClass(inv.Status)}>{inv.Status}</span></td>
					</tr>
				{/each}
				{#if !loading && invoices.length === 0}
					<tr><td colspan="8" class="text-center py-12 muted">No invoices match your filters.</td></tr>
				{/if}
			</tbody>
		</table>
	</div>

	<!-- Pagination -->
	{#if totalPages > 1}
		<div class="flex items-center justify-between">
			<div class="muted text-sm">Page {pagination.page} of {totalPages}</div>
			<div class="flex gap-2">
				<button class="btn-secondary" disabled={pagination.page <= 1} onclick={() => setPage(pagination.page - 1)}>‹ Prev</button>
				<button class="btn-secondary" disabled={pagination.page >= totalPages} onclick={() => setPage(pagination.page + 1)}>Next ›</button>
			</div>
		</div>
	{/if}
</div>
