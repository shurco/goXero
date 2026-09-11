<script lang="ts">
	import { accountApi, conversionBalanceApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import SettingsHeader from '$lib/components/SettingsHeader.svelte';
	import { BALANCE_SHEET_CLASSES } from '$lib/chart-of-accounts';
	import type { Account, ConversionBalanceLine, Organisation } from '$lib/types';

	interface Row {
		id: string;
		/**
		 * Empty on a row the person has just added. The rest of the row is the
		 * two columns the screen edits; the API takes one signed amount, and the
		 * two are the same figure seen twice (see toLine).
		 */
		accountId: string;
		/** The ledger's own name for the account, so a saved row still names it
		 *  when the picker's active chart no longer lists it. */
		code: string;
		debit: number;
		credit: number;
	}

	let loading = $state(true);
	let err = $state('');
	let org = $state<Organisation | null>(null);
	let accounts = $state<Account[]>([]);
	let rows = $state<Row[]>([]);
	let conversionDate = $state(new Date().toISOString().slice(0, 10));
	let showAllAccounts = $state(false);
	let locked = $state(false);
	let openStep = $state<number | null>(1);
	let nextRowID = 1;

	/** A row the person has not entered the API yet. Ids keep their identity when
	 *  one is removed, and are local to this form. */
	function newRow(): Row {
		return { id: `new-${nextRowID++}`, accountId: '', code: '', debit: 0, credit: 0 };
	}

	async function reload() {
		loading = true;
		err = '';
		try {
			const [o, accs, cb] = await Promise.all([
				orgApi.current().catch(() => null),
				accountApi.list({ status: 'ACTIVE' }).catch(() => [] as Account[]),
				conversionBalanceApi.get()
			]);
			org = o ?? null;
			accounts = accs ?? [];
			locked = cb.Locked;
			if (cb.ConversionDate) conversionDate = cb.ConversionDate;
			// The saved balances win over the seeded rows: an organisation that has
			// converted has opening balances to show, and one that has not has none
			// to make up.
			rows = cb.Lines.length > 0 ? cb.Lines.map(toRow) : defaultRows(accounts);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load conversion balances';
		} finally {
			loading = false;
		}
	}

	// Xero's conversion-balances screen starts with the accounts an opening
	// balance is actually entered against: every account money sits in, plus the
	// two document control accounts. Both are read from the organisation's own
	// chart — the bank accounts by their type, the control accounts by the role
	// they declare — so this seeds a chart it has never seen, not the codes of
	// one it has. No balance is filled in: an opening balance is the one figure
	// in the books that cannot come from anywhere but the person entering it.
	function defaultRows(accs: Account[]): Row[] {
		const picked = accs.filter(
			(a) => a.Type === 'BANK' || a.SystemAccount === 'DEBTORS' || a.SystemAccount === 'CREDITORS'
		);
		if (picked.length === 0) return [newRow()];
		return picked.map((a) => ({ ...newRow(), accountId: a.AccountID ?? '', code: a.Code }));
	}

	/**
	 * A saved line is one signed amount — a debit positive, a credit negative —
	 * while the screen edits two columns. The credit column is exactly the
	 * amount's negation, so the split loses nothing and the two columns cannot
	 * disagree with a third figure that says something else.
	 */
	function toRow(line: ConversionBalanceLine): Row {
		const amount = Number(line.Amount) || 0;
		return {
			id: `saved-${line.AccountID}`,
			accountId: line.AccountID,
			code: line.Code ?? '',
			debit: amount > 0 ? amount : 0,
			credit: amount < 0 ? -amount : 0
		};
	}

	function toLine(r: Row) {
		return {
			AccountID: r.accountId,
			Amount: (Number(r.debit || 0) - Number(r.credit || 0)).toFixed(2)
		};
	}

	$effect(() => {
		if ($session.tenantId) void reload();
	});

	const totalDebits = $derived(rows.reduce((s, r) => s + (Number(r.debit) || 0), 0));
	const totalCredits = $derived(rows.reduce((s, r) => s + (Number(r.credit) || 0), 0));
	const adjustments = $derived(Math.abs(totalDebits - totalCredits));

	function addLine() {
		rows = [...rows, newRow()];
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
			r.id === rowId ? { ...r, accountId, code: acc?.Code || r.code } : r
		);
	}

	const visibleAccounts = $derived(
		showAllAccounts
			? accounts
			: accounts.filter((a) => BALANCE_SHEET_CLASSES.includes(a.Class ?? ''))
	);

	/**
	 * The picker offers the active chart, which need not hold the account a saved
	 * balance sits on — an archived account, or one outside the balance-sheet
	 * tabs. Without this the row would open on "Choose an account…" and its
	 * account would be lost the moment anything was saved.
	 */
	function accountInPicker(row: Row): boolean {
		return visibleAccounts.some((a) => a.AccountID === row.accountId);
	}

	let saving = $state(false);
	async function save() {
		saving = true;
		err = '';
		try {
			const cb = await conversionBalanceApi.save({
				ConversionDate: conversionDate,
				Locked: locked,
				Lines: rows.filter((r) => r.accountId).map(toLine)
			});
			// The screen takes the server's answer rather than assuming its own: the
			// balances are posted as a journal, and a set whose columns differed comes
			// back with the adjustment line the server added. What the reports will
			// print is what the screen now shows.
			locked = cb.Locked;
			if (cb.ConversionDate) conversionDate = cb.ConversionDate;
			rows = cb.Lines.map(toRow);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Save failed';
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

{#if err}
	<div class="rounded-lg bg-red-50 text-red-700 text-sm px-4 py-3 border border-red-100 mb-5">{err}</div>
{/if}

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
										value={r.accountId}
										onchange={(e) => setAccount(r.id, (e.target as HTMLSelectElement).value)}
									>
										<option value="">Choose an account…</option>
										{#each visibleAccounts as a (a.AccountID)}
											<option value={a.AccountID}>{a.Code} - {a.Name}</option>
										{/each}
										{#if r.accountId && !accountInPicker(r)}
											<option value={r.accountId}>{r.code}</option>
										{/if}
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
