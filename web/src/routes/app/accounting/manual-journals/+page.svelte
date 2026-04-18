<script lang="ts">
	import { onMount } from 'svelte';
	import { manualJournalApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatDate } from '$lib/utils/format';
	import type { ManualJournal } from '$lib/types';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let items = $state<ManualJournal[]>([]);
	let loading = $state(true);

	async function reload() {
		loading = true;
		try {
			const res = await manualJournalApi.list().catch(() => null);
			items = res?.ManualJournals ?? [];
		} finally {
			loading = false;
		}
	}
	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });
</script>

<ModuleHeader title="Manual journals" subtitle="Direct entries into the general ledger." />

<div class="card p-5">
	<table class="table-auto-xero">
		<thead>
			<tr>
				<th>Date</th>
				<th>Narration</th>
				<th>Status</th>
			</tr>
		</thead>
		<tbody>
			{#each items as j}
				<tr>
					<td>{formatDate(j.Date)}</td>
					<td>{j.Narration ?? '—'}</td>
					<td>{j.Status ?? '—'}</td>
				</tr>
			{/each}
			{#if !loading && items.length === 0}
				<tr><td colspan="3" class="text-center py-8 muted">No manual journals yet.</td></tr>
			{/if}
		</tbody>
	</table>
</div>
