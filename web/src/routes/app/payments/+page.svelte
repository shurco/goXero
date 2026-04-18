<script lang="ts">
	import { paymentApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import type { Payment, Pagination } from '$lib/types';
	import { onMount } from 'svelte';
	import { formatCurrency, formatDate, statusClass } from '$lib/utils/format';

	let payments = $state<Payment[]>([]);
	let pagination = $state<Pagination>({ page: 1, pageSize: 25, total: 0 });
	let loading = $state(true);

	async function reload() {
		loading = true;
		try {
			const res = await paymentApi.list({ page: String(pagination.page), pageSize: String(pagination.pageSize) });
			payments = res?.Payments ?? [];
			pagination = res?.Pagination ?? pagination;
		} finally {
			loading = false;
		}
	}

	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });
</script>

<div class="space-y-6">
	<div>
		<h1 class="section-title">Payments</h1>
		<p class="muted">Payments applied to invoices, credit notes and prepayments.</p>
	</div>

	<div class="card overflow-x-auto">
		<table class="table-auto-xero">
			<thead>
				<tr>
					<th>Date</th>
					<th>Type</th>
					<th>Reference</th>
					<th class="text-right">Amount</th>
					<th>Status</th>
					<th>Reconciled</th>
				</tr>
			</thead>
			<tbody>
				{#each payments as p}
					<tr>
						<td>{formatDate(p.Date)}</td>
						<td class="muted">{p.PaymentType}</td>
						<td class="muted">{p.Reference || '—'}</td>
						<td class="text-right tabular-nums">{formatCurrency(p.Amount)}</td>
						<td><span class={statusClass(p.Status)}>{p.Status}</span></td>
						<td>{p.IsReconciled ? '✓' : '—'}</td>
					</tr>
				{/each}
				{#if !loading && payments.length === 0}
					<tr><td colspan="6" class="text-center py-12 muted">No payments recorded yet.</td></tr>
				{/if}
			</tbody>
		</table>
	</div>
</div>
