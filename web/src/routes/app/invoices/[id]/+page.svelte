<script lang="ts">
	import { page } from '$app/stores';
	import { invoiceApi, paymentApi } from '$lib/api';
	import { formatCurrency, formatDate, statusClass } from '$lib/utils/format';
	import type { Invoice } from '$lib/types';
	import { onMount } from 'svelte';

	let invoice = $state<Invoice | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let showPay = $state(false);
	let payAmount = $state('0');

	async function load() {
		loading = true;
		error = null;
		const id = $page.params.id;
		if (!id) {
			error = 'Invoice id is missing';
			loading = false;
			return;
		}
		try {
			invoice = await invoiceApi.get(id);
			payAmount = String(invoice?.AmountDue ?? 0);
		} catch (err) {
			error = (err as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	async function approve() {
		if (!invoice) return;
		await invoiceApi.updateStatus(invoice.InvoiceID, 'AUTHORISED');
		await load();
	}
	async function voidIt() {
		if (!invoice) return;
		await invoiceApi.updateStatus(invoice.InvoiceID, 'VOIDED');
		await load();
	}

	async function submitPayment() {
		if (!invoice) return;
		await paymentApi.create({
			invoiceId: invoice.InvoiceID,
			amount: Number(payAmount),
			date: new Date().toISOString().slice(0, 10)
		});
		showPay = false;
		await load();
	}
</script>

<div class="space-y-6">
	{#if loading}
		<div class="muted">Loading…</div>
	{:else if error}
		<div class="rounded-lg bg-red-50 text-red-700 px-4 py-3 border border-red-100">{error}</div>
	{:else if invoice}
		<div class="flex items-start justify-between flex-wrap gap-4">
			<div>
				<div class="flex items-center gap-3">
					<h1 class="section-title">{invoice.InvoiceNumber || 'Invoice'}</h1>
					<span class={statusClass(invoice.Status)}>{invoice.Status}</span>
				</div>
				<p class="muted">{invoice.Type === 'ACCREC' ? 'Sales invoice' : 'Bill'} · issued {formatDate(invoice.Date)}</p>
			</div>
			<div class="flex gap-2">
				{#if invoice.Status === 'DRAFT'}
					<button class="btn-primary" onclick={approve}>Approve</button>
				{/if}
				{#if invoice.Status === 'AUTHORISED'}
					<button class="btn-primary" onclick={() => (showPay = true)}>Record payment</button>
				{/if}
				{#if invoice.Status !== 'PAID' && invoice.Status !== 'VOIDED'}
					<button class="btn-secondary" onclick={voidIt}>Void</button>
				{/if}
				<a class="btn-ghost" href="/app/invoices">← Back</a>
			</div>
		</div>

		<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
			<div class="card p-6 lg:col-span-2 space-y-6">
				<div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
					<div><div class="muted">Contact</div><div class="font-medium">{invoice.Contact?.Name ?? '—'}</div></div>
					<div><div class="muted">Reference</div><div class="font-medium">{invoice.Reference ?? '—'}</div></div>
					<div><div class="muted">Due date</div><div class="font-medium">{formatDate(invoice.DueDate)}</div></div>
					<div><div class="muted">Currency</div><div class="font-medium">{invoice.CurrencyCode ?? 'USD'}</div></div>
				</div>

				<div class="overflow-x-auto -mx-6">
					<table class="table-auto-xero">
						<thead>
							<tr>
								<th>Description</th>
								<th class="text-right">Qty</th>
								<th class="text-right">Unit</th>
								<th>Account</th>
								<th class="text-right">Tax</th>
								<th class="text-right">Amount</th>
							</tr>
						</thead>
						<tbody>
							{#each invoice.LineItems ?? [] as li}
								<tr>
									<td>{li.Description || '—'}</td>
									<td class="text-right tabular-nums">{li.Quantity}</td>
									<td class="text-right tabular-nums">{formatCurrency(li.UnitAmount, invoice.CurrencyCode)}</td>
									<td class="muted">{li.AccountCode ?? ''}</td>
									<td class="text-right tabular-nums">{formatCurrency(li.TaxAmount, invoice.CurrencyCode)}</td>
									<td class="text-right tabular-nums">{formatCurrency(li.LineAmount, invoice.CurrencyCode)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>

			<div class="card p-6 space-y-3 text-sm">
				<h3 class="font-semibold text-ink-900">Summary</h3>
				<div class="flex justify-between"><span class="muted">Subtotal</span><span class="font-medium tabular-nums">{formatCurrency(invoice.SubTotal, invoice.CurrencyCode)}</span></div>
				<div class="flex justify-between"><span class="muted">Tax</span><span class="font-medium tabular-nums">{formatCurrency(invoice.TotalTax, invoice.CurrencyCode)}</span></div>
				<div class="flex justify-between border-t border-ink-100 pt-2"><span class="font-semibold">Total</span><span class="font-semibold tabular-nums">{formatCurrency(invoice.Total, invoice.CurrencyCode)}</span></div>
				<div class="flex justify-between"><span class="muted">Paid</span><span class="text-emerald-600 font-medium tabular-nums">{formatCurrency(invoice.AmountPaid, invoice.CurrencyCode)}</span></div>
				<div class="flex justify-between"><span class="muted">Due</span><span class="font-semibold text-brand-700 tabular-nums">{formatCurrency(invoice.AmountDue, invoice.CurrencyCode)}</span></div>
			</div>
		</div>
	{/if}
</div>

{#if showPay && invoice}
	<div class="fixed inset-0 bg-ink-900/50 flex items-center justify-center z-50 p-4">
		<div class="bg-white rounded-xl shadow-pop max-w-md w-full p-6">
			<h3 class="text-lg font-semibold">Record payment</h3>
			<p class="muted text-sm">Apply a payment to {invoice.InvoiceNumber}.</p>
			<div class="mt-4 space-y-3">
				<div>
					<label class="label" for="pay-amount">Amount</label>
					<input id="pay-amount" class="input" type="number" step="0.01" bind:value={payAmount} />
				</div>
			</div>
			<div class="flex justify-end gap-2 mt-6">
				<button class="btn-secondary" onclick={() => (showPay = false)}>Cancel</button>
				<button class="btn-primary" onclick={submitPayment}>Record</button>
			</div>
		</div>
	</div>
{/if}
