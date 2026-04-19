<script lang="ts">
	import { onMount } from 'svelte';
	import { accountApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import SettingsHeader from '$lib/components/SettingsHeader.svelte';
	import type { Account, Organisation } from '$lib/types';

	interface Row {
		id: string;
		accountId?: string;
		code: string;
		name: string;
		debit: number;
		credit: number;
	}

	let loading = $state(true);
	let org = $state<Organisation | null>(null);
	let accounts = $state<Account[]>([]);
	let rows = $state<Row[]>([]);
	let conversionDate = $state(new Date().toISOString().slice(0, 10));
	let showAllAccounts = $state(false);
	let locked = $state(false);
	let openStep = $state<number | null>(1);

	async function reload() {
		loading = true;
		try {
			const [o, accs] = await Promise.all([
				orgApi.current().catch(() => null),
				accountApi.list({ status: 'ACTIVE' }).catch(() => [] as Account[])
			]);
			org = o ?? null;
			accounts = accs ?? [];
			if (rows.length === 0) {
				rows = defaultRows(accounts);
			}
		} finally {
			loading = false;
		}
	}

	// Seed with a handful of common accounts so the table isn't empty — mimics
	// Xero's starter rows (Bank, Receivables, Payables).
	function defaultRows(accs: Account[]): Row[] {
		const seeds = ['090', '120', '200'];
		const picked = seeds
			.map((code) => accs.find((a) => a.Code === code))
			.filter((a): a is Account => !!a);
		const result: Row[] =
			picked.length > 0
				? picked.map((a, i) => ({
						id: `seed-${i}`,
						accountId: a.AccountID,
						code: a.Code,
						name: a.Name,
						debit: i === 0 ? 4130.98 : 0,
						credit: 0
					}))
				: [];
		if (result.length === 0) {
			return [{ id: 'seed-0', code: '', name: '', debit: 0, credit: 0 }];
		}
		return result;
	}

	onMount(reload);
	$effect(() => {
		if ($session.tenantId) void reload();
	});

	const totalDebits = $derived(rows.reduce((s, r) => s + (Number(r.debit) || 0), 0));
	const totalCredits = $derived(rows.reduce((s, r) => s + (Number(r.credit) || 0), 0));
	const adjustments = $derived(Math.abs(totalDebits - totalCredits));

	function addLine() {
		rows = [
			...rows,
			{ id: `new-${Date.now()}`, code: '', name: '', debit: 0, credit: 0 }
		];
	}

	function removeLine(id: string) {
		rows = rows.filter((r) => r.id !== id);
	}

	function removeZeroBalances() {
		rows = rows.filter((r) => (Number(r.debit) || 0) !== 0 || (Number(r.credit) || 0) !== 0);
	}

	function setAccount(rowId: string, accountId: string) {
		const acc = accounts.find((a) => a.AccountID === accountId);
		rows = rows.map((r) =>
			r.id === rowId
				? { ...r, accountId, code: acc?.Code || r.code, name: acc?.Name || r.name }
				: r
		);
	}

	const visibleAccounts = $derived(
		showAllAccounts
			? accounts
			: accounts.filter((a) =>
					['BANK', 'CURRENT', 'NONCURRENT', 'CURRLIAB', 'LIABILITY', 'EQUITY', 'FIXED'].includes(
						a.Type
					)
				)
	);

	let saving = $state(false);
	async function save() {
		saving = true;
		try {
			await new Promise((r) => setTimeout(r, 300));
			alert(
				'Conversion balances captured locally. Persisting requires a dedicated /api/v1/conversion-balances endpoint.'
			);
		} finally {
			saving = false;
		}
	}

	const steps = [
		{
			title: "What's this?",
			body: 'Conversion balances are the opening balances for each account as at the date you move into goXero — they make sure your reports tie back to your previous system.'
		},
		{
			title: 'Enter bank balances as they were on this date',
			body: 'Match each bank account to its reconciled closing balance. Unreconciled transactions will be recorded separately so nothing is double counted.'
		},
		{
			title: 'Enter total outstanding invoices on this date',
			body: 'Add the total value of invoices that have been issued but not paid as at your conversion date.'
		},
		{
			title: 'Enter total outstanding bills on this date',
			body: 'Add the total value of bills that have been received but not paid as at your conversion date.'
		},
		{
			title: 'Enter any other balances',
			body: 'Fill in the remaining balance sheet accounts using a trial balance generated from your prior system.'
		},
		{
			title: 'Confirm',
			body: 'Review the trial balance, make sure debits equal credits and lock the conversion balances to prevent accidental changes.'
		}
	];

	function toggleStep(i: number) {
		openStep = openStep === i ? null : i;
	}
</script>

<SettingsHeader title="Conversion balances" description={org?.Name ?? ''} />

<div class="flex flex-wrap items-center gap-3 mb-5">
	<button class="btn-secondary" type="button" disabled>
		<span class="text-brand-600 mr-1">+</span> Add Comparative Balances
	</button>
	<button class="btn-secondary" type="button" disabled>
		<svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
			<path
				d="M4 4h12v3H4zm0 5h12v1.5H4zm0 3.5h8V14H4zM4 16h5v1.5H4z"
			/>
		</svg>
		Conversion Date
	</button>
</div>

<div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
	<!-- Main balance sheet -->
	<section class="lg:col-span-2 space-y-3">
		<div class="flex items-center gap-2 text-sm">
			<span class="inline-block rounded-t-md border border-b-0 border-ink-200 bg-white px-4 py-1.5 font-medium text-ink-900">
				{formatDate(conversionDate)}
			</span>
			<label class="muted ml-2" for="cb-date">
				<span class="sr-only">Conversion date</span>
				<input
					id="cb-date"
					type="date"
					bind:value={conversionDate}
					class="input py-1 text-xs"
				/>
			</label>
		</div>

		<div class="card overflow-hidden">
			<table class="table-auto-xero">
				<thead>
					<tr>
						<th>Account</th>
						<th class="text-right w-36">Debit</th>
						<th class="text-right w-36">Credit</th>
						<th class="w-10"></th>
					</tr>
				</thead>
				<tbody>
					{#if loading}
						<tr><td colspan="4" class="text-center py-8 muted">Loading accounts…</td></tr>
					{:else}
						{#each rows as r (r.id)}
							<tr>
								<td>
									<select
										class="select"
										aria-label="Account"
										value={r.accountId ?? ''}
										onchange={(e) => setAccount(r.id, (e.target as HTMLSelectElement).value)}
									>
										<option value="">Choose an account…</option>
										{#each visibleAccounts as a (a.AccountID)}
											<option value={a.AccountID}>{a.Code} - {a.Name}</option>
										{/each}
									</select>
								</td>
								<td class="text-right">
									<input
										type="number"
										class="input text-right"
										step="0.01"
										aria-label="Debit"
										bind:value={r.debit}
										disabled={locked}
									/>
								</td>
								<td class="text-right">
									<input
										type="number"
										class="input text-right"
										step="0.01"
										aria-label="Credit"
										bind:value={r.credit}
										disabled={locked}
									/>
								</td>
								<td>
									<button
										type="button"
										class="icon-btn"
										aria-label="Remove line"
										onclick={() => removeLine(r.id)}
										disabled={locked}
									>
										×
									</button>
								</td>
							</tr>
						{/each}
					{/if}
				</tbody>
			</table>

			<div class="flex flex-wrap items-center gap-4 px-4 py-3 border-t border-ink-100 text-sm">
				<button
					class="text-brand-600 hover:underline inline-flex items-center gap-1"
					type="button"
					onclick={addLine}
					disabled={locked}
				>
					<span>+</span> Add a new line
				</button>
				<label class="flex items-center gap-2 text-brand-600 cursor-pointer">
					<input type="checkbox" bind:checked={showAllAccounts} class="accent-brand-500" />
					Show all accounts
				</label>
				<button
					class="text-brand-600 hover:underline"
					type="button"
					onclick={removeZeroBalances}
					disabled={locked}
				>
					Remove zero balances
				</button>
			</div>

			<div class="px-4 py-3 border-t border-ink-100 text-sm grid grid-cols-3 gap-2">
				<div class="font-medium">Total Debits</div>
				<div class="text-right tabular-nums font-medium">
					{formatCurrency(totalDebits, org?.BaseCurrency)}
				</div>
				<div></div>
				<div class="font-medium">Total Credits</div>
				<div class="text-right tabular-nums font-medium">
					{formatCurrency(totalCredits, org?.BaseCurrency)}
				</div>
				<div></div>
				<div class="font-medium">
					Adjustments
					<p class="muted text-xs font-normal mt-0.5">
						This accounts for the difference between debits and credits and for FX gains and losses.
					</p>
				</div>
				<div class="text-right tabular-nums font-medium">
					{formatCurrency(adjustments, org?.BaseCurrency)}
				</div>
				<div></div>
			</div>

			<div class="px-4 py-3 border-t border-ink-100 flex items-start gap-2 text-sm">
				<input
					type="checkbox"
					id="cb-lock"
					class="mt-1 accent-brand-500"
					bind:checked={locked}
				/>
				<label for="cb-lock">
					<span class="font-medium text-ink-900">Lock balances at {formatDate(conversionDate)}</span>
					<p class="muted text-xs mt-0.5 max-w-md">
						Locking ensures no accidental edits to balances or transactions are made before this
						date. Only users with Adviser roles will be able to make any changes.
						<a href="/app/settings" class="text-brand-600 hover:underline">Read more</a>
					</p>
				</label>
			</div>

			<div class="px-4 py-3 border-t border-ink-100 flex justify-end gap-2">
				<button class="btn-secondary" type="button" disabled={saving} onclick={reload}>
					Cancel
				</button>
				<button
					class="btn-primary bg-emerald-500 hover:bg-emerald-600"
					type="button"
					disabled={saving}
					onclick={save}
				>
					{saving ? 'Saving…' : 'Save'}
				</button>
			</div>
		</div>
	</section>

	<!-- Right help panel -->
	<aside class="space-y-2">
		<div class="flex items-start gap-2 mb-3">
			<span
				class="inline-flex h-6 w-6 items-center justify-center rounded-full bg-amber-500 text-white text-xs font-bold"
				>?</span
			>
			<div class="flex-1">
				<h3 class="text-sm font-semibold text-ink-900">Starting with the right numbers</h3>
			</div>
		</div>

		<div class="card divide-y divide-ink-100">
			{#each steps as step, i (i)}
				<div>
					<button
						type="button"
						class="w-full flex items-center justify-between gap-3 px-4 py-3 text-left hover:bg-ink-50 transition"
						onclick={() => toggleStep(i)}
						aria-expanded={openStep === i}
					>
						<span class="text-sm text-ink-900">
							{#if i > 0}{i}. {/if}{step.title}
						</span>
						<svg
							class="text-ink-500 transition-transform {openStep === i ? 'rotate-180' : ''}"
							width="12"
							height="12"
							viewBox="0 0 20 20"
							fill="currentColor"
							aria-hidden="true"
						>
							<path d="M5 7l5 5 5-5H5z" />
						</svg>
					</button>
					{#if openStep === i}
						<div class="px-4 pb-4 muted text-xs leading-relaxed">{step.body}</div>
					{/if}
				</div>
			{/each}
		</div>
	</aside>
</div>
