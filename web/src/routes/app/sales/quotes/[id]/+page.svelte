<script lang="ts">
	import { page } from '$app/stores';
	import { quoteApi } from '$lib/api';
	import { formatCurrency, formatDate, statusClass, statusLabel } from '$lib/utils/format';
	import type { Quote } from '$lib/types';
	import { onMount } from 'svelte';

	let quote = $state<Quote | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	async function load() {
		loading = true;
		error = null;
		const id = $page.params.id;
		if (!id) {
			error = 'Quote id is missing';
			loading = false;
			return;
		}
		try {
			quote = await quoteApi.get(id);
		} catch (err) {
			error = (err as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);
</script>

<div class="space-y-6">
	{#if loading}
		<div class="muted">Loading…</div>
	{:else if error}
		<div class="rounded-lg border border-red-100 bg-red-50 px-4 py-3 text-red-700">{error}</div>
		<a class="btn-secondary mt-4 inline-block" href="/app/sales/quotes">← Back to quotes</a>
	{:else if quote}
		<div class="flex flex-wrap items-start justify-between gap-4">
			<div>
				<div class="text-sm">
					<a href="/app/sales" class="text-brand-600 hover:underline">Sales overview</a>
					<span class="muted">›</span>
					<a href="/app/sales/quotes" class="text-brand-600 hover:underline">Quotes</a>
				</div>
				<div class="mt-2 flex flex-wrap items-center gap-3">
					<h1 class="section-title">{quote.QuoteNumber || 'Quote'}</h1>
					<span class={statusClass(quote.Status)}>{statusLabel(quote.Status)}</span>
				</div>
				<p class="muted">Issued {formatDate(quote.Date)} · Expires {formatDate(quote.ExpiryDate)}</p>
			</div>
			<a class="btn-ghost" href="/app/sales/quotes">← Back</a>
		</div>

		<div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
			<div class="card space-y-6 p-6 lg:col-span-2">
				<div class="grid grid-cols-2 gap-4 text-sm md:grid-cols-4">
					<div>
						<div class="muted">Contact</div>
						<div class="font-medium">{quote.Contact?.Name ?? '—'}</div>
					</div>
					<div>
						<div class="muted">Reference</div>
						<div class="font-medium">{quote.Reference ?? '—'}</div>
					</div>
					<div>
						<div class="muted">Currency</div>
						<div class="font-medium">{quote.CurrencyCode ?? 'USD'}</div>
					</div>
					<div>
						<div class="muted">Total</div>
						<div class="font-medium tabular-nums">{formatCurrency(quote.Total, quote.CurrencyCode)}</div>
					</div>
				</div>

				<div class="-mx-6 overflow-x-auto">
					<table class="table-auto-xero">
						<thead>
							<tr>
								<th>Description</th>
								<th class="text-right">Qty</th>
								<th class="text-right">Unit</th>
								<th class="text-right">Amount</th>
							</tr>
						</thead>
						<tbody>
							{#each quote.LineItems ?? [] as li}
								<tr>
									<td>{li.Description || '—'}</td>
									<td class="text-right tabular-nums">{li.Quantity}</td>
									<td class="text-right tabular-nums">{formatCurrency(li.UnitAmount, quote.CurrencyCode)}</td>
									<td class="text-right tabular-nums">{formatCurrency(li.LineAmount, quote.CurrencyCode)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>

			<div class="card space-y-3 p-6 text-sm">
				<div class="flex justify-between">
					<span class="muted">Subtotal</span>
					<span class="tabular-nums">{formatCurrency(quote.SubTotal, quote.CurrencyCode)}</span>
				</div>
				<div class="flex justify-between">
					<span class="muted">Tax</span>
					<span class="tabular-nums">{formatCurrency(quote.TotalTax, quote.CurrencyCode)}</span>
				</div>
				<div class="flex justify-between border-t border-ink-100 pt-3 font-semibold">
					<span>Total</span>
					<span class="tabular-nums">{formatCurrency(quote.Total, quote.CurrencyCode)}</span>
				</div>
			</div>
		</div>
	{/if}
</div>
