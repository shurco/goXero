<script lang="ts">
	import { itemApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import type { Item } from '$lib/types';
	import { onMount } from 'svelte';
	import { formatCurrency } from '$lib/utils/format';

	let items = $state<Item[]>([]);
	let loading = $state(true);
	let showNew = $state(false);
	let newItem = $state<Partial<Item>>({ Code: '', Name: '', Description: '', IsSold: true, IsPurchased: false });

	async function reload() {
		loading = true;
		try {
			items = await itemApi.list();
		} finally {
			loading = false;
		}
	}

	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	async function create() {
		await itemApi.create(newItem);
		showNew = false;
		newItem = { Code: '', Name: '', Description: '', IsSold: true, IsPurchased: false };
		reload();
	}
</script>

<div class="space-y-6">
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<h1 class="section-title">Products & services</h1>
			<p class="muted">Reusable items you sell or purchase.</p>
		</div>
		<button class="btn-primary" onclick={() => (showNew = true)}>+ New item</button>
	</div>

	<div class="card overflow-x-auto">
		<table class="table-auto-xero">
			<thead>
				<tr>
					<th>Code</th>
					<th>Name</th>
					<th class="text-right">Sales price</th>
					<th>Sales account</th>
					<th class="text-right">Purchase price</th>
					<th class="text-right">On hand</th>
				</tr>
			</thead>
			<tbody>
				{#each items as it}
					<tr>
						<td class="font-mono text-ink-900">{it.Code}</td>
						<td class="font-medium">{it.Name || '—'}</td>
						<td class="text-right tabular-nums">{formatCurrency(it.SalesDetails?.UnitPrice)}</td>
						<td class="muted">{it.SalesDetails?.AccountCode ?? '—'}</td>
						<td class="text-right tabular-nums">{formatCurrency(it.PurchaseDetails?.UnitPrice)}</td>
						<td class="text-right tabular-nums">{it.QuantityOnHand ?? 0}</td>
					</tr>
				{/each}
				{#if !loading && items.length === 0}
					<tr><td colspan="6" class="text-center py-12 muted">No items yet.</td></tr>
				{/if}
			</tbody>
		</table>
	</div>
</div>

{#if showNew}
	<div class="fixed inset-0 bg-ink-900/50 flex items-center justify-center z-50 p-4">
		<div class="bg-white rounded-xl shadow-pop max-w-lg w-full p-6">
			<h3 class="text-lg font-semibold">New item</h3>
			<div class="mt-4 grid grid-cols-2 gap-3">
				<div><label class="label" for="item-code">Code *</label><input id="item-code" class="input" bind:value={newItem.Code} /></div>
				<div><label class="label" for="item-name">Name</label><input id="item-name" class="input" bind:value={newItem.Name} /></div>
				<div class="col-span-2"><label class="label" for="item-desc">Description</label><textarea id="item-desc" class="textarea" bind:value={newItem.Description}></textarea></div>
				<label class="flex items-center gap-2"><input type="checkbox" bind:checked={newItem.IsSold} /> Sold</label>
				<label class="flex items-center gap-2"><input type="checkbox" bind:checked={newItem.IsPurchased} /> Purchased</label>
			</div>
			<div class="flex justify-end gap-2 mt-6">
				<button class="btn-secondary" onclick={() => (showNew = false)}>Cancel</button>
				<button class="btn-primary" onclick={create} disabled={!newItem.Code}>Create</button>
			</div>
		</div>
	</div>
{/if}
