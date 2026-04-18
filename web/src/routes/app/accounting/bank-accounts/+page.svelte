<script lang="ts">
	import { onMount } from 'svelte';
	import { accountApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency } from '$lib/utils/format';
	import type { Account, Organisation } from '$lib/types';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let accounts = $state<Account[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);

	async function reload() {
		loading = true;
		try {
			const [accs, o] = await Promise.all([
				accountApi.list({ status: 'ACTIVE' }).catch(() => []),
				orgApi.current().catch(() => null)
			]);
			accounts = (accs as Account[]).filter((a) => a.Type === 'BANK');
			org = o ?? null;
		} finally {
			loading = false;
		}
	}
	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	const currency = $derived(org?.BaseCurrency || 'USD');
</script>

<ModuleHeader
	title="Bank accounts"
	subtitle="Your chart-of-accounts bank and credit-card accounts."
	primary={{ label: 'Connect a bank', href: '/app/bank-feeds' }}
/>

<div class="card p-5">
	<table class="table-auto-xero">
		<thead>
			<tr>
				<th>Name</th>
				<th>Code</th>
				<th>Number</th>
				<th>Currency</th>
				<th class="text-right">Balance</th>
			</tr>
		</thead>
		<tbody>
			{#each accounts as a}
				<tr>
					<td class="font-medium">{a.Name}</td>
					<td>{a.Code}</td>
					<td class="tabular-nums">{a.BankAccountNumber ?? '—'}</td>
					<td>{a.CurrencyCode ?? currency}</td>
					<td class="text-right tabular-nums">{formatCurrency(0, a.CurrencyCode ?? currency)}</td>
				</tr>
			{/each}
			{#if !loading && accounts.length === 0}
				<tr><td colspan="5" class="text-center py-8 muted">No bank accounts yet. Add one from the Chart of accounts.</td></tr>
			{/if}
		</tbody>
	</table>
</div>
