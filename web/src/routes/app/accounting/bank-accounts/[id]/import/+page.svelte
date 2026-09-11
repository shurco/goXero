<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { accountApi, statementImportApi } from '$lib/api';
	import { formatCurrency } from '$lib/utils/format';
	import type {
		Account,
		DetectedColumn,
		StatementImport,
		StatementMapping,
		StatementParseResult,
		StatementPreviewRow
	} from '$lib/types';

	/**
	 * The three-step import wizard, in the order Xero asks the questions:
	 * upload the file, confirm what the columns mean, then review the rows —
	 * including the ones that look like duplicates — before anything is saved.
	 */
	type Step = 1 | 2 | 3;

	let accountId = $derived($page.params.id ?? '');
	let account = $state<Account | null>(null);
	let step = $state<Step>(1);

	let file = $state<File | null>(null);
	let busy = $state(false);
	let err = $state('');
	let dragging = $state(false);

	let staged = $state<StatementImport | null>(null);
	let preview = $state<StatementPreviewRow[]>([]);
	let duplicates = $state(0);
	let columns = $state<string[]>([]);
	let detected = $state<DetectedColumn[]>([]);
	let hasHeader = $state(true);
	let delimiter = $state('');

	// role[i] is the Mapping field the user assigned to column i ('' = ignored).
	let roles = $state<string[]>([]);
	let dateFormat = $state('');
	let decimalSeparator = $state('');
	let includeDuplicates = $state(false);
	let result = $state<{ Imported: number; Skipped: number } | null>(null);

	const ROLE_OPTIONS = [
		{ value: '', label: 'Ignore' },
		{ value: 'Date', label: 'Date' },
		{ value: 'Amount', label: 'Amount' },
		{ value: 'Debit', label: 'Money out' },
		{ value: 'Credit', label: 'Money in' },
		{ value: 'Type', label: 'Amount type' },
		{ value: 'Payee', label: 'Payee' },
		{ value: 'Description', label: 'Description' },
		{ value: 'Reference', label: 'Reference' },
		{ value: 'ChequeNumber', label: 'Cheque number' },
		{ value: 'Balance', label: 'Balance' }
	];

	const DATE_FORMATS = [
		{ value: '', label: 'Detect automatically' },
		{ value: '2006-01-02', label: 'YYYY-MM-DD' },
		{ value: '01/02/2006', label: 'MM/DD/YYYY' },
		{ value: '02/01/2006', label: 'DD/MM/YYYY' },
		{ value: '02.01.2006', label: 'DD.MM.YYYY' },
		{ value: '20060102', label: 'YYYYMMDD' },
		{ value: 'Jan 2, 2006', label: 'Mon D, YYYY' },
		{ value: '2 Jan 2006', label: 'D Mon YYYY' }
	];

	const DECIMALS = [
		{ value: '', label: 'Detect automatically' },
		{ value: '.', label: '1,234.56 (dot)' },
		{ value: ',', label: '1.234,56 (comma)' }
	];

	const isCSV = $derived(staged?.Format === 'CSV');
	const currency = $derived(staged?.CurrencyCode || account?.CurrencyCode || '');

	onMount(async () => {
		if (!accountId) return;
		try {
			account = (await accountApi.get(accountId)) ?? null;
		} catch {
			/* the header just falls back to the account id */
		}
	});

	function roleFor(i: number): string {
		return roles[i] ?? '';
	}

	function setRole(i: number, value: string) {
		const next = [...roles];
		// A column can only play one role, so taking a role away from the column
		// that had it avoids two columns feeding the same field.
		if (value) {
			for (let j = 0; j < next.length; j++) {
				if (j !== i && next[j] === value) next[j] = '';
			}
		}
		next[i] = value;
		roles = next;
	}

	/** Build the server's Mapping from the per-column roles the user chose. */
	function buildMapping(): StatementMapping {
		const m: StatementMapping = {
			HasHeader: hasHeader,
			SkipRows: 0,
			AmountMode: 'SIGNED'
		};
		columns.forEach((name, i) => {
			const role = roleFor(i);
			if (!role) return;
			(m as unknown as Record<string, string>)[role] = name;
		});
		if (m.Debit || m.Credit) {
			m.AmountMode = m.Amount ? 'AMOUNT_WITH_TYPE' : 'DEBIT_CREDIT';
		}
		if (dateFormat) m.DateFormat = dateFormat;
		if (decimalSeparator) m.DecimalSeparator = decimalSeparator;
		return m;
	}

	/**
	 * Rebuild the per-column roles from the server's own Mapping. The keys of a
	 * Mapping are the roles (Date, Amount, …) and its values are the column
	 * names, so each column has to be looked for among the values.
	 */
	function rolesFromMapping(mapping: StatementMapping | undefined, names: string[]): string[] {
		if (!mapping) return names.map(() => '');
		const m = mapping as unknown as Record<string, unknown>;
		return names.map((name) => {
			for (const o of ROLE_OPTIONS) {
				if (o.value && m[o.value] === name) return o.value;
			}
			return '';
		});
	}

	/** The sample values the server saw in a column, for the mapping table. */
	function samplesFor(name: string): string[] {
		return detected.find((d) => d.Name === name)?.Samples ?? [];
	}

	function applyParseResponse(res: StatementParseResult) {
		staged = res.Import;
		preview = res.Preview ?? [];
		duplicates = res.Duplicates ?? 0;
		columns = res.Columns ?? [];
		delimiter = res.Delimiter ?? '';
		hasHeader = res.HasHeader ?? true;
		detected = res.Detected ?? [];
		roles = rolesFromMapping(res.Mapping, res.Columns ?? []);
		dateFormat = res.Mapping?.DateFormat ?? '';
		decimalSeparator = res.Mapping?.DecimalSeparator ?? '';
	}

	async function onFileChosen(e: Event) {
		const input = e.target as HTMLInputElement;
		const chosen = input.files?.[0];
		if (!chosen) return;
		file = chosen;
		await upload();
	}

	function onDragOver(e: DragEvent) {
		e.preventDefault();
		dragging = true;
	}

	function onDragLeave() {
		dragging = false;
	}

	async function onFileDropped(e: DragEvent) {
		e.preventDefault();
		dragging = false;
		const dropped = e.dataTransfer?.files?.[0];
		if (!dropped) return;
		file = dropped;
		await upload();
	}

	async function upload() {
		if (!file) return;
		busy = true;
		err = '';
		try {
			const res = await statementImportApi.upload(file, accountId);
			const parsed = res.StatementImports?.[0];
			if (!parsed) throw new Error('The file could not be read');
			applyParseResponse(parsed);
			step = 2;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not read the statement file';
		} finally {
			busy = false;
		}
	}

	/**
	 * Apply a corrected mapping. The file itself is still in the browser, so it
	 * is re-sent — the parse is the server's job, and duplicating it here would
	 * be a second implementation to keep in step.
	 */
	async function reapplyMapping() {
		if (!staged || !file) return;
		busy = true;
		err = '';
		try {
			const res = await statementImportApi.remap(staged.ImportID, file, buildMapping());
			const parsed = res.StatementImports?.[0];
			if (parsed) applyParseResponse(parsed);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not apply that mapping';
		} finally {
			busy = false;
		}
	}

	async function goToReview() {
		// The mapping the user confirmed is what the review shows, so it is
		// pushed to the server before the numbers on step 3 are trusted.
		try {
			if (isCSV) await reapplyMapping();
		} finally {
			step = 3;
		}
	}

	async function commit() {
		if (!staged) return;
		busy = true;
		err = '';
		try {
			const res = await statementImportApi.commit(staged.ImportID, includeDuplicates);
			result = { Imported: res.Imported ?? 0, Skipped: res.Skipped ?? 0 };
			step = 3;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not import the statement';
		} finally {
			busy = false;
		}
	}

	function finish() {
		void goto(`/app/accounting/bank-accounts/${accountId}?tab=statements`);
	}

	const stmtRange = $derived(
		staged?.StatementStart || staged?.StatementEnd
			? `${staged?.StatementStart?.slice(0, 10) ?? '—'} → ${staged?.StatementEnd?.slice(0, 10) ?? '—'}`
			: '—'
	);
</script>

<p class="text-sm mb-2">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
	<span class="muted"> / </span>
	<a href="/app/accounting/bank-accounts/{accountId}" class="text-brand-600 hover:underline"
		>{account?.Name ?? 'Account'}</a
	>
	<span class="muted"> / </span><span class="muted">Import statement</span>
</p>

<header class="mb-5">
	<h1 class="section-title">Import a bank statement</h1>
	<p class="text-sm muted mt-1">
		Upload an OFX, QFX, QBO, QIF or CSV file from {account?.Name ?? 'this account'}. Nothing is
		added to your books until you confirm on the last step.
	</p>
</header>

{#if err}
	<p class="text-sm text-red-700 mb-3" role="alert">{err}</p>
{/if}

<ol class="flex flex-wrap gap-2 mb-5 text-sm">
	{#each [{ n: 1, label: 'Upload file' }, { n: 2, label: 'Import settings' }, { n: 3, label: 'Review & import' }] as s (s.n)}
		<li
			class="px-3 py-1.5 rounded-full border {step === s.n
				? 'border-brand-500 bg-brand-50 text-brand-700 font-semibold'
				: step > s.n
					? 'border-emerald-300 bg-emerald-50 text-emerald-800'
					: 'border-ink-200 text-ink-500'}"
		>
			{s.n}. {s.label}
		</li>
	{/each}
</ol>

{#if step === 1}
	<section class="card p-6">
		<h2 class="content-section-title mb-3">Upload your statement file</h2>
		<label
			class="block border-2 border-dashed rounded-md p-10 text-center cursor-pointer {dragging
				? 'border-brand-500 bg-brand-50'
				: 'border-ink-200 hover:border-brand-400'}"
			ondragover={onDragOver}
			ondragleave={onDragLeave}
			ondrop={onFileDropped}
		>
			<input type="file" class="hidden" accept=".csv,.ofx,.qfx,.qbo,.qif,.txt" onchange={onFileChosen} />
			{#if busy}
				<span class="muted">Reading {file?.name ?? 'your file'}…</span>
			{:else}
				<span class="block font-medium">Choose a file or drop it here</span>
				<span class="block text-sm muted mt-1">
					CSV, OFX, QFX, QBO and QIF are all supported. The format is detected from the file
					itself, not its extension, because banks often mislabel their exports.
				</span>
			{/if}
		</label>
		{#if file && !busy}
			<div class="mt-4 flex items-center gap-3 text-sm">
				<span class="muted">{file.name}</span>
				<button type="button" class="btn-secondary-sm" onclick={() => upload()}>Try again</button>
			</div>
		{/if}
	</section>
{:else if step === 2 && staged}
	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100 flex flex-wrap items-center justify-between gap-2">
			<h2 class="content-section-title">Import settings</h2>
			<div class="text-sm muted">
				{staged.Filename} · {staged.Format} · {staged.LineCount} transactions
			</div>
		</header>

		<dl class="px-5 py-4 grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm border-b border-ink-100">
			<div>
				<dt class="text-xs uppercase muted tracking-wide">Statement period</dt>
				<dd class="tabular-nums">{stmtRange}</dd>
			</div>
			<div>
				<dt class="text-xs uppercase muted tracking-wide">Opening balance</dt>
				<dd class="tabular-nums">
					{staged.OpeningBalance != null ? formatCurrency(staged.OpeningBalance, currency) : '—'}
				</dd>
			</div>
			<div>
				<dt class="text-xs uppercase muted tracking-wide">Closing balance</dt>
				<dd class="tabular-nums">
					{staged.ClosingBalance != null ? formatCurrency(staged.ClosingBalance, currency) : '—'}
				</dd>
			</div>
			<div>
				<dt class="text-xs uppercase muted tracking-wide">Looks like duplicates</dt>
				<dd class="tabular-nums {duplicates > 0 ? 'text-amber-700 font-semibold' : ''}">
					{duplicates}
				</dd>
			</div>
		</dl>

		{#if isCSV}
			<div class="px-5 py-4 border-b border-ink-100">
				<label class="flex items-center gap-2 text-sm">
					<input type="checkbox" bind:checked={hasHeader} />
					<span>The first row contains column headings</span>
				</label>
				{#if delimiter}
					<p class="text-xs muted mt-1">
						Detected delimiter: <code>{delimiter === '\t' ? 'tab' : delimiter}</code>
					</p>
				{/if}
			</div>

			<div class="px-5 py-4 overflow-x-auto">
				<table class="min-w-full text-sm">
					<thead class="text-ink-500 text-xs uppercase">
						<tr>
							<th class="py-2 text-left">Column</th>
							<th class="py-2 text-left">Example values</th>
							<th class="py-2 text-left">Use as</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each columns as name, i (name + i)}
							<tr>
								<td class="py-2 pr-4 font-medium">{name}</td>
								<td class="py-2 pr-4 muted tabular-nums">
									{samplesFor(name).join(', ') || '—'}
								</td>
								<td class="py-2">
									<select
										class="input py-1"
										value={roleFor(i)}
										onchange={(e) => setRole(i, e.currentTarget.value)}
									>
										{#each ROLE_OPTIONS as o (o.value)}
											<option value={o.value}>{o.label}</option>
										{/each}
									</select>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>

			<div class="px-5 py-4 grid sm:grid-cols-2 gap-4 border-t border-ink-100">
				<label class="text-sm">
					<span class="block muted mb-1">Date format</span>
					<select class="input" bind:value={dateFormat}>
						{#each DATE_FORMATS as f (f.value)}
							<option value={f.value}>{f.label}</option>
						{/each}
					</select>
				</label>
				<label class="text-sm">
					<span class="block muted mb-1">Decimal separator</span>
					<select class="input" bind:value={decimalSeparator}>
						{#each DECIMALS as d (d.value)}
							<option value={d.value}>{d.label}</option>
						{/each}
					</select>
				</label>
			</div>
		{:else}
			<p class="px-5 py-4 text-sm muted">
				{staged.Format} files carry their own structure, so there are no columns to map. Continue
				to review the transactions we read from the file.
			</p>
		{/if}

		<footer class="px-5 py-3 bg-ink-50 flex items-center justify-between gap-3">
			<button type="button" class="btn-secondary-sm" onclick={() => (step = 1)} disabled={busy}>
				Back
			</button>
			<button type="button" class="btn-primary" onclick={goToReview} disabled={busy}>
				{busy ? 'Applying…' : 'Continue'}
			</button>
		</footer>
	</section>
{:else if step === 3 && staged}
	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100 flex flex-wrap items-center justify-between gap-2">
			<h2 class="content-section-title">Review &amp; import</h2>
			<div class="text-sm muted">{preview.length} of {staged.LineCount} shown</div>
		</header>

		{#if result}
			<div class="p-6 text-center">
				<p class="text-lg font-semibold text-emerald-700">
					Imported {result.Imported} transaction{result.Imported === 1 ? '' : 's'}.
				</p>
				{#if result.Skipped > 0}
					<p class="text-sm muted mt-1">
						{result.Skipped} row{result.Skipped === 1 ? ' was' : 's were'} skipped as duplicates.
					</p>
				{/if}
				<p class="text-sm muted mt-2">
					They are waiting in the reconcile inbox — nothing has been posted to your accounts yet.
				</p>
				<div class="mt-4 flex justify-center gap-2">
					<button type="button" class="btn-primary" onclick={finish}>Go to the inbox</button>
				</div>
			</div>
		{:else}
			{#if duplicates > 0}
				<div class="px-5 py-3 bg-amber-50 border-b border-amber-200 text-sm text-amber-900">
					<p class="font-medium">
						{duplicates} of these transactions already appear in this account.
					</p>
					<label class="flex items-center gap-2 mt-2">
						<input type="checkbox" bind:checked={includeDuplicates} />
						<span>Import them anyway (only if the bank really did charge twice)</span>
					</label>
				</div>
			{/if}

			<div class="max-h-[28rem] overflow-auto">
				<table class="min-w-full text-sm">
					<thead class="bg-ink-50 text-ink-500 text-xs uppercase sticky top-0">
						<tr>
							<th class="px-3 py-2 text-left">Date</th>
							<th class="px-3 py-2 text-left">Payee</th>
							<th class="px-3 py-2 text-left">Description</th>
							<th class="px-3 py-2 text-left">Reference</th>
							<th class="px-3 py-2 text-left">Source</th>
							<th class="px-3 py-2 text-right">Amount</th>
							<th class="px-3 py-2 text-right">Balance</th>
							<th class="px-3 py-2 text-left"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each preview as row, i (`${row.Date}-${i}`)}
							<tr class={row.Duplicate ? 'bg-amber-50' : ''}>
								<td class="px-3 py-2 tabular-nums">{row.Date}</td>
								<td class="px-3 py-2">{row.Payee ?? '—'}</td>
								<td class="px-3 py-2">{row.Description ?? ''}</td>
								<td class="px-3 py-2">{row.Reference ?? ''}</td>
								<td class="px-3 py-2 muted">Imported</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{formatCurrency(row.Amount, currency)}
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{row.Balance != null ? formatCurrency(row.Balance, currency) : ''}
								</td>
								<td class="px-3 py-2">
									{#if row.Duplicate}
										<span class="text-xs text-amber-800">Possible duplicate</span>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>

			<footer class="px-5 py-3 bg-ink-50 flex items-center justify-between gap-3">
				<button type="button" class="btn-secondary-sm" onclick={() => (step = 2)} disabled={busy}>
					Back
				</button>
				<button type="button" class="btn-primary" onclick={commit} disabled={busy}>
					{busy ? 'Importing…' : `Import ${staged.LineCount} transactions`}
				</button>
			</footer>
		{/if}
	</section>
{/if}
