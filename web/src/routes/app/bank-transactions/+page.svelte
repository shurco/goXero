<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';
	import { accountApi, bankTransactionApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, BankTransaction } from '$lib/types';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let loading = $state(true);
	let rows = $state<BankTransaction[]>([]);
	let accounts = $state<Account[]>([]);
	let selectedAccountId = $state<string>('');

	async function load() {
		loading = true;
		try {
			const list = await accountApi
				.list({ where: 'Type=="BANK"' })
				.catch(() => [] as Account[]);
			accounts = list.filter((a) => a.Type === 'BANK');
			const params: Record<string, string> = { pageSize: '100' };
			if (selectedAccountId) params.accountId = selectedAccountId;
			const res = await bankTransactionApi.list(params);
			rows = res.BankTransactions ?? [];
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		const q = $page.url.searchParams.get('accountId') ?? '';
		if (q !== selectedAccountId) selectedAccountId = q;
	});

	$effect(() => {
		if ($session.tenantId) void load();
	});

	onMount(load);
</script>

<ModuleHeader
	title="Bank transactions"
	subtitle="Spend and receive money recorded against bank accounts"
/>

<div class="card p-4 mb-4 flex flex-wrap items-end gap-3">
	<label class="block">
		<span class="label">Bank account</span>
		<select class="input" bind:value={selectedAccountId} onchange={() => load()}>
			<option value="">All accounts</option>
			{#each accounts as a}
				<option value={a.AccountID}>{a.Name}</option>
			{/each}
		</select>
	</label>
</div>

<div class="card overflow-hidden">
	<table class="min-w-full text-sm">
		<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
			<tr>
				<th class="px-4 py-2 text-left">Date</th>
				<th class="px-4 py-2 text-left">Type</th>
				<th class="px-4 py-2 text-left">Contact</th>
				<th class="px-4 py-2 text-right">Amount</th>
				<th class="px-4 py-2">Reconciled</th>
			</tr>
		</thead>
		<tbody class="divide-y divide-ink-100">
			{#if loading}
				<tr><td colspan="5" class="p-6 text-center muted">Loading…</td></tr>
			{:else if rows.length === 0}
				<tr><td colspan="5" class="p-6 text-center muted">No transactions.</td></tr>
			{:else}
				{#each rows as t}
					<tr class="hover:bg-ink-50">
						<td class="px-4 py-2">{formatDate(t.Date)}</td>
						<td class="px-4 py-2">{t.Type}</td>
						<td class="px-4 py-2">{t.Contact?.Name ?? '—'}</td>
						<td class="px-4 py-2 text-right tabular-nums">
							{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? 'USD')}
						</td>
						<td class="px-4 py-2 text-center">
							{#if t.IsReconciled}
								<span class="text-emerald-700">✓</span>
							{:else}
								<span class="text-amber-600">—</span>
							{/if}
						</td>
					</tr>
				{/each}
			{/if}
		</tbody>
	</table>
</div>
