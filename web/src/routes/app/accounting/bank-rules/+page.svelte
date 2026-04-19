<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { bankRuleApi } from '$lib/api';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import type { BankRule } from '$lib/types';
	import { formatDate } from '$lib/utils/format';

	let loading = $state(true);
	let rules = $state<BankRule[]>([]);
	let err = $state('');

	function typeLabel(t: string) {
		if (t === 'SPEND') return 'Spend money';
		if (t === 'RECEIVE') return 'Receive money';
		if (t === 'TRANSFER') return 'Transfer';
		return t;
	}

	async function reload() {
		loading = true;
		err = '';
		try {
			rules = await bankRuleApi.list();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load rules';
			rules = [];
		} finally {
			loading = false;
		}
	}

	onMount(reload);
</script>

<div class="w-full space-y-6">
	<p class="text-sm mb-1">
		<a href="/app/accounting" class="text-brand-600 hover:underline">Accounting</a>
	</p>

	<ModuleHeader
		title="Bank rules"
		subtitle="Automate how bank statement lines are coded during reconciliation."
		primary={{ label: 'Create rule', href: '/app/accounting/bank-rules/new' }}
	/>

	{#if err}
		<p class="text-sm text-red-700" role="alert">{err}</p>
	{/if}

	{#if loading}
		<div class="card p-8 muted text-center">Loading…</div>
	{:else if rules.length === 0}
		<div class="card p-8 text-center text-ink-600">
			<p>No bank rules yet.</p>
			<button type="button" class="btn-primary mt-4" onclick={() => goto('/app/accounting/bank-rules/new')}>
				Create rule
			</button>
		</div>
	{:else}
		<div class="card overflow-hidden">
			<table class="table-auto-xero">
				<thead>
					<tr>
						<th>Name</th>
						<th>Type</th>
						<th>Updated</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each rules as r}
						<tr>
							<td class="font-medium text-ink-900">{r.Name}</td>
							<td>{typeLabel(r.RuleType)}</td>
							<td class="muted text-sm">
								{r.UpdatedDateUTC ? formatDate(r.UpdatedDateUTC) : '—'}
							</td>
							<td class="text-right">
								<a
									href="/app/accounting/bank-rules/{r.BankRuleID}"
									class="text-brand-600 hover:underline text-sm"
								>
									Edit
								</a>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
