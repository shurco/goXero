<script lang="ts">
	import { accountApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import type { Account } from '$lib/types';
	import { onMount } from 'svelte';
	import { statusClass } from '$lib/utils/format';

	let accounts = $state<Account[]>([]);
	let loading = $state(true);
	let filter = $state('');

	async function reload() {
		loading = true;
		try {
			accounts = await accountApi.list({ status: 'ACTIVE' });
		} finally {
			loading = false;
		}
	}
	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	const filtered = $derived(
		accounts.filter((a) =>
			!filter ||
			a.Name.toLowerCase().includes(filter.toLowerCase()) ||
			a.Code.toLowerCase().includes(filter.toLowerCase())
		)
	);

	const grouped = $derived(groupBy(filtered, (a) => typeGroup(a.Type)));

	function typeGroup(t: string) {
		if (['BANK', 'CURRENT', 'FIXED', 'INVENTORY', 'NONCURRENT', 'PREPAYMENT'].includes(t)) return 'Assets';
		if (['CURRLIAB', 'LIABILITY', 'TERMLIAB', 'PAYGLIABILITY', 'SUPERANNUATIONLIABILITY'].includes(t)) return 'Liabilities';
		if (t === 'EQUITY') return 'Equity';
		if (['REVENUE', 'SALES'].includes(t)) return 'Revenue';
		return 'Expenses';
	}

	function groupBy<T>(arr: T[], fn: (x: T) => string): Record<string, T[]> {
		return arr.reduce((acc, x) => {
			const k = fn(x);
			acc[k] = acc[k] || [];
			acc[k].push(x);
			return acc;
		}, {} as Record<string, T[]>);
	}
</script>

<div class="space-y-6">
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<h1 class="section-title">Chart of accounts</h1>
			<p class="muted">The backbone of your general ledger.</p>
		</div>
	</div>

	<div class="card p-4 flex gap-3">
		<input class="input w-80" placeholder="Search accounts…" bind:value={filter} />
	</div>

	{#each Object.keys(grouped) as group}
		<div class="card overflow-hidden">
			<div class="px-6 py-3 bg-ink-50 text-sm font-semibold text-ink-700 uppercase tracking-wide">{group}</div>
			<table class="table-auto-xero">
				<thead>
					<tr>
						<th class="w-24">Code</th>
						<th>Name</th>
						<th>Type</th>
						<th>Tax</th>
						<th>Status</th>
					</tr>
				</thead>
				<tbody>
					{#each grouped[group] as acc}
						<tr>
							<td class="font-mono text-ink-900">{acc.Code}</td>
							<td class="font-medium">{acc.Name}</td>
							<td class="muted">{acc.Type}</td>
							<td class="muted">{acc.TaxType || '—'}</td>
							<td><span class={statusClass(acc.Status)}>{acc.Status}</span></td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/each}

	{#if !loading && filtered.length === 0}
		<div class="muted text-center py-12">No accounts found.</div>
	{/if}
</div>
