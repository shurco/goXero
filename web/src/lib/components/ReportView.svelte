<script lang="ts">
	import { untrack } from 'svelte';
	import { reportApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import type { Report, ReportRow } from '$lib/types';

	interface Field {
		name: string;
		label: string;
		type?: 'date' | 'text' | 'checkbox' | 'select';
		/** Options for a select field. */
		options?: { value: string; label: string }[];
	}

	interface Props {
		title: string;
		endpoint: string;
		defaults?: Record<string, string>;
		fields?: Field[];
	}
	let { title, endpoint, defaults = {}, fields = [] }: Props = $props();

	// Seeded once from the defaults; doing this in an $effect would wipe whatever
	// the user typed every time the parent re-rendered.
	let params = $state<Record<string, string>>(untrack(() => ({ ...defaults })));
	let report = $state<Report | null>(null);
	let loading = $state(false);
	let error = $state('');

	async function run() {
		loading = true;
		error = '';
		try {
			const data = await reportApi.run(endpoint, params);
			report = data?.Reports?.[0] ?? data?.Payload?.Reports?.[0] ?? null;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	$effect(() => { if ($session.tenantId) void run(); });
</script>

<div class="space-y-4">
	<div class="flex items-start justify-between flex-wrap gap-3">
		<h1 class="section-title">{title}</h1>
		<div class="flex gap-2 items-end flex-wrap">
			{#each fields as f (f.name)}
				{#if f.type === 'checkbox'}
					<label class="flex items-center gap-2 pb-2">
						<input
							type="checkbox"
							checked={params[f.name] === 'true'}
							onchange={(e) =>
								(params[f.name] = e.currentTarget.checked ? 'true' : '')}
						/>
						<span class="text-sm">{f.label}</span>
					</label>
				{:else if f.type === 'select'}
					<label class="block">
						<span class="label">{f.label}</span>
						<select class="input" bind:value={params[f.name]}>
							{#each f.options ?? [] as o (o.value)}
								<option value={o.value}>{o.label}</option>
							{/each}
						</select>
					</label>
				{:else}
					<label class="block">
						<span class="label">{f.label}</span>
						<input class="input" type={f.type ?? 'text'} bind:value={params[f.name]} />
					</label>
				{/if}
			{/each}
			<button class="btn-primary" onclick={run} disabled={loading}>Run</button>
		</div>
	</div>

	{#if error}<div class="card p-4 text-red-700 text-sm">{error}</div>{/if}
	{#if loading && !report}<div class="muted">Loading…</div>{/if}

	{#if report}
		<div class="card p-5">
			<div class="mb-3">
				<h2 class="font-semibold text-ink-900">{report.ReportName ?? title}</h2>
				{#if report.ReportTitles?.length}
					<div class="muted text-sm">{report.ReportTitles.join(' · ')}</div>
				{/if}
			</div>

			{#each report.Rows ?? [] as row}
				{@render renderRow(row, 0)}
			{/each}
		</div>
	{/if}
</div>

{#snippet renderRow(row: ReportRow, depth: number)}
	{#if row.RowType === 'Header'}
		<div class="grid grid-cols-[1fr_auto_auto] gap-4 text-xs uppercase muted border-b pb-2 mb-2 mt-4">
			{#each row.Cells ?? [] as c}
				<div>{c.Value}</div>
			{/each}
		</div>
	{:else if row.RowType === 'Section'}
		<div class="mt-5">
			{#if row.Title}<div class="text-sm font-semibold text-ink-900 mb-1">{row.Title}</div>{/if}
			{#each row.Rows ?? [] as sub}
				{@render renderRow(sub, depth + 1)}
			{/each}
		</div>
	{:else if row.RowType === 'Row' || row.RowType === 'SummaryRow'}
		<div
			class="grid grid-cols-[1fr_auto_auto] gap-4 text-sm py-1.5 {row.RowType === 'SummaryRow' ? 'font-semibold border-t mt-2 pt-2' : ''}"
			style="padding-left: {depth * 8}px"
		>
			{#each row.Cells ?? [] as c}
				<div class={c.Value && !isNaN(Number(c.Value)) ? 'text-right tabular-nums' : ''}>
					{c.Value ?? ''}
				</div>
			{/each}
		</div>
	{/if}
{/snippet}
