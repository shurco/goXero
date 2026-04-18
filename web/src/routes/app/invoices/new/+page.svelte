<script lang="ts">
	import { contactApi, invoiceApi, accountApi, itemApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { goto } from '$app/navigation';
	import type { Account, Contact, Item, LineItem } from '$lib/types';
	import { onMount } from 'svelte';
	import { formatCurrency } from '$lib/utils/format';

	let contacts = $state<Contact[]>([]);
	let accounts = $state<Account[]>([]);
	let items = $state<Item[]>([]);

	let type = $state<'ACCREC' | 'ACCPAY'>('ACCREC');
	let contactId = $state('');
	let invoiceNumber = $state('');
	let reference = $state('');
	let date = $state(new Date().toISOString().slice(0, 10));
	let dueDate = $state('');
	let lineAmountTypes = $state<'Exclusive' | 'Inclusive' | 'NoTax'>('Exclusive');
	let status = $state<'DRAFT' | 'AUTHORISED'>('DRAFT');
	let saving = $state(false);
	let error = $state<string | null>(null);

	let lines = $state<LineItem[]>([
		{ Description: '', Quantity: 1, UnitAmount: 0, AccountCode: '200', TaxType: '', TaxAmount: 0 }
	]);

	onMount(async () => {
		const [cs, as, its] = await Promise.all([
			contactApi.list({ pageSize: '200' }).catch(() => ({ Contacts: [] })),
			accountApi.list({ status: 'ACTIVE' }).catch(() => []),
			itemApi.list().catch(() => [])
		]);
		contacts = cs?.Contacts ?? [];
		accounts = as ?? [];
		items = its ?? [];
	});

	function addLine() {
		lines = [...lines, { Description: '', Quantity: 1, UnitAmount: 0, AccountCode: '200', TaxType: '', TaxAmount: 0 }];
	}

	function removeLine(i: number) {
		lines = lines.filter((_, idx) => idx !== i);
	}

	const subTotal = $derived(
		lines.reduce((sum, l) => sum + Number(l.Quantity || 0) * Number(l.UnitAmount || 0), 0)
	);
	const totalTax = $derived(lines.reduce((sum, l) => sum + Number(l.TaxAmount || 0), 0));
	const total = $derived(lineAmountTypes === 'Exclusive' ? subTotal + totalTax : subTotal);

	async function save(newStatus: 'DRAFT' | 'AUTHORISED') {
		status = newStatus;
		saving = true;
		error = null;
		try {
			const payload = {
				Type: type,
				ContactID: contactId,
				InvoiceNumber: invoiceNumber,
				Reference: reference,
				Date: date,
				DueDate: dueDate || undefined,
				LineAmountTypes: lineAmountTypes,
				Status: newStatus,
				LineItems: lines.map((l) => ({
					Description: l.Description,
					Quantity: Number(l.Quantity),
					UnitAmount: Number(l.UnitAmount),
					AccountCode: l.AccountCode,
					TaxType: l.TaxType,
					TaxAmount: Number(l.TaxAmount || 0),
					ItemCode: l.ItemCode
				}))
			};
			const res = await invoiceApi.create(payload as unknown as Parameters<typeof invoiceApi.create>[0]);
			const created = res.Invoices?.[0];
			if (created) goto(`/app/invoices/${created.InvoiceID}`);
			else goto('/app/invoices');
		} catch (err) {
			error = (err as Error).message || 'Save failed';
		} finally {
			saving = false;
		}
	}

	function applyItem(i: number, code: string) {
		const it = items.find((x) => x.Code === code);
		if (!it) return;
		const price = it.SalesDetails?.UnitPrice ?? 0;
		lines[i] = {
			...lines[i],
			ItemCode: it.Code,
			Description: it.Name || it.Description || '',
			UnitAmount: Number(price),
			AccountCode: it.SalesDetails?.AccountCode || lines[i].AccountCode
		};
	}
</script>

<div class="space-y-6">
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<h1 class="section-title">New invoice</h1>
			<p class="muted">Create a new {type === 'ACCREC' ? 'sales invoice' : 'bill'}.</p>
		</div>
		<div class="flex gap-2">
			<a href="/app/invoices" class="btn-ghost">Cancel</a>
			<button class="btn-secondary" disabled={saving} onclick={() => save('DRAFT')}>Save as draft</button>
			<button class="btn-primary" disabled={saving} onclick={() => save('AUTHORISED')}>Approve</button>
		</div>
	</div>

	{#if error}
		<div class="rounded-lg bg-red-50 text-red-700 text-sm px-4 py-3 border border-red-100">{error}</div>
	{/if}

	<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
		<!-- Details -->
		<div class="card p-6 lg:col-span-2 space-y-5">
			<div class="grid grid-cols-2 gap-4">
				<div>
					<label class="label" for="inv-type">Type</label>
					<select id="inv-type" class="select" bind:value={type}>
						<option value="ACCREC">Sales invoice (ACCREC)</option>
						<option value="ACCPAY">Bill (ACCPAY)</option>
					</select>
				</div>
				<div>
					<label class="label" for="inv-contact">Contact</label>
					<select id="inv-contact" class="select" bind:value={contactId}>
						<option value="">— Select contact —</option>
						{#each contacts as c}
							<option value={c.ContactID}>{c.Name}</option>
						{/each}
					</select>
				</div>
				<div>
					<label class="label" for="inv-number">Invoice number</label>
					<input id="inv-number" class="input" placeholder="INV-001" bind:value={invoiceNumber} />
				</div>
				<div>
					<label class="label" for="inv-reference">Reference</label>
					<input id="inv-reference" class="input" bind:value={reference} />
				</div>
				<div>
					<label class="label" for="inv-date">Date</label>
					<input id="inv-date" class="input" type="date" bind:value={date} />
				</div>
				<div>
					<label class="label" for="inv-due">Due date</label>
					<input id="inv-due" class="input" type="date" bind:value={dueDate} />
				</div>
				<div>
					<label class="label" for="inv-amount-types">Amounts are</label>
					<select id="inv-amount-types" class="select" bind:value={lineAmountTypes}>
						<option value="Exclusive">Tax exclusive</option>
						<option value="Inclusive">Tax inclusive</option>
						<option value="NoTax">No tax</option>
					</select>
				</div>
			</div>

			<!-- Line items -->
			<div>
				<div class="flex items-center justify-between mb-2">
					<h3 class="font-semibold text-ink-900">Line items</h3>
					<button class="btn-ghost text-brand-700" onclick={addLine}>+ Add line</button>
				</div>
				<div class="overflow-x-auto -mx-6">
					<table class="table-auto-xero">
						<thead>
							<tr>
								<th class="w-48">Item</th>
								<th>Description</th>
								<th class="w-20 text-right">Qty</th>
								<th class="w-32 text-right">Unit price</th>
								<th class="w-28">Account</th>
								<th class="w-24 text-right">Tax</th>
								<th class="w-28 text-right">Amount</th>
								<th class="w-8"></th>
							</tr>
						</thead>
						<tbody>
							{#each lines as line, i}
								<tr>
									<td>
										<select class="select" value={line.ItemCode ?? ''} onchange={(e) => applyItem(i, (e.target as HTMLSelectElement).value)}>
											<option value="">—</option>
											{#each items as it}
												<option value={it.Code}>{it.Code}</option>
											{/each}
										</select>
									</td>
									<td><input class="input" bind:value={line.Description} /></td>
									<td><input class="input text-right" type="number" step="0.01" bind:value={line.Quantity} /></td>
									<td><input class="input text-right" type="number" step="0.01" bind:value={line.UnitAmount} /></td>
									<td>
										<select class="select" bind:value={line.AccountCode}>
											{#each accounts as acc}
												<option value={acc.Code}>{acc.Code} — {acc.Name}</option>
											{/each}
										</select>
									</td>
									<td><input class="input text-right" type="number" step="0.01" bind:value={line.TaxAmount} /></td>
									<td class="text-right tabular-nums">{formatCurrency(Number(line.Quantity || 0) * Number(line.UnitAmount || 0))}</td>
									<td><button class="text-ink-400 hover:text-red-500" onclick={() => removeLine(i)} aria-label="remove">✕</button></td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		</div>

		<!-- Summary -->
		<div class="card p-6 space-y-4">
			<h3 class="font-semibold text-ink-900">Summary</h3>
			<div class="space-y-2 text-sm">
				<div class="flex justify-between"><span class="muted">Subtotal</span><span class="font-medium tabular-nums">{formatCurrency(subTotal)}</span></div>
				<div class="flex justify-between"><span class="muted">Total tax</span><span class="font-medium tabular-nums">{formatCurrency(totalTax)}</span></div>
				<div class="flex justify-between border-t pt-2 border-ink-100">
					<span class="font-semibold">Total</span>
					<span class="font-semibold text-lg tabular-nums">{formatCurrency(total)}</span>
				</div>
			</div>
			<div class="rounded-lg bg-ink-50 p-3 text-xs muted">
				Totals are recalculated server-side before saving. Xero-style rounding and tax logic will apply.
			</div>
		</div>
	</div>
</div>
