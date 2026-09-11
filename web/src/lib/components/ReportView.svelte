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

	/** A cell holding an amount, as opposed to a label, date or reference. */
	const AMOUNT = /^-?\d[\d,]*(\.\d+)?$/;

	function eachRow(rows: ReportRow[], visit: (row: ReportRow) => void) {
		for (const row of rows) {
			visit(row);
			if (row.Rows?.length) eachRow(row.Rows, visit);
		}
	}

	function isAmount(value: string | undefined): boolean {
		const trimmed = (value ?? '').trim();
		return trimmed !== '' && AMOUNT.test(trimmed);
	}

	/**
	 * Where each cell of a row sits. Rows are written left to right, except for
	 * one shape the API emits: a short row of label + amounts, such as the Total
	 * line of Account Transactions, which carries only its label and the two
	 * amount columns and belongs under the *trailing* amount columns rather than
	 * under Source and Reference. A short row of anything else — a period
	 * heading with no comparative column to show — stays left to right.
	 */
	function placements(row: ReportRow, columns: number): { start: number; span: number }[] {
		const cells = row.Cells ?? [];
		const count = cells.length;
		if (count >= columns) return cells.map((_, i) => ({ start: i + 1, span: 1 }));

		const trailing = count > 1 && cells.slice(1).every((c) => isAmount(c.Value));
		const starts = trailing
			? [1, ...Array.from({ length: count - 1 }, (_, i) => columns - (count - 1) + i + 1)]
			: cells.map((_, i) => i + 1);
		return starts.map((start, i) => ({
			start,
			span: trailing && i === 0 ? columns - (count - 1) : 1
		}));
	}

	/**
	 * The table's shape, read from the response rather than assumed: the column
	 * count comes from the header row (the widest row, if a report ever exceeds
	 * it) and each column is right-aligned only when every figure in it is an
	 * amount. The same component renders two-column statements and the
	 * seven-column ageing reports.
	 */
	const shape = $derived.by(() => {
		const rows = report?.Rows ?? [];
		let headerColumns = 0;
		let widest = 1;
		eachRow(rows, (row) => {
			const count = row.Cells?.length ?? 0;
			if (row.RowType === 'Header') headerColumns = Math.max(headerColumns, count);
			widest = Math.max(widest, count);
		});
		const columns = Math.max(headerColumns, widest);

		const amountColumn: (boolean | undefined)[] = new Array(columns).fill(undefined);
		eachRow(rows, (row) => {
			if (row.RowType !== 'Row' && row.RowType !== 'SummaryRow') return;
			const cells = row.Cells ?? [];
			const placed = placements(row, columns);
			cells.forEach((cell, i) => {
				const value = (cell.Value ?? '').trim();
				if (!value) return;
				const index = (placed[i]?.start ?? i + 1) - 1;
				amountColumn[index] = (amountColumn[index] ?? true) && AMOUNT.test(value);
			});
		});

		return {
			columns,
			template: `minmax(0, 2fr)${' minmax(0, 1fr)'.repeat(columns - 1)}`,
			amount: amountColumn.map((seen) => seen === true)
		};
	});
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

			<div class="overflow-x-auto">
				<div class="min-w-[44rem]">
					{#each report.Rows ?? [] as row}
						{@render renderRow(row, 0)}
					{/each}
				</div>
			</div>
		</div>
	{/if}
</div>

{#snippet renderRow(row: ReportRow, depth: number)}
	{#if row.RowType === 'Header'}
		<div
			class="grid gap-x-4 text-xs uppercase muted border-b pb-2 mb-2 mt-4"
			style="grid-template-columns: {shape.template}"
		>
			{#each row.Cells ?? [] as c, i}
				{@const place = placements(row, shape.columns)[i] ?? { start: i + 1, span: 1 }}
				<div
					class={shape.amount[place.start - 1] ? 'text-right tabular-nums' : ''}
					style="grid-column: {place.start} / span {place.span}"
				>
					{c.Value}
				</div>
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
			class="grid gap-x-4 text-sm py-1.5 {row.RowType === 'SummaryRow' ? 'font-semibold border-t mt-2 pt-2' : ''}"
			style="grid-template-columns: {shape.template}"
		>
			{#each row.Cells ?? [] as c, i}
				{@const place = placements(row, shape.columns)[i] ?? { start: i + 1, span: 1 }}
				<div
					class={shape.amount[place.start - 1] ? 'text-right tabular-nums' : ''}
					style="grid-column: {place.start} / span {place.span}; {i === 0
						? `padding-left: ${depth * 8}px`
						: ''}"
				>
					{c.Value ?? ''}
				</div>
			{/each}
		</div>
	{/if}
{/snippet}
