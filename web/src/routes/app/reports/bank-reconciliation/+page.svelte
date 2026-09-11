<script lang="ts">
	import { session } from '$lib/stores/session';
	import { accountApi, orgApi, statementApi } from '$lib/api';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, Organisation, StatementBalance } from '$lib/types';

	/**
	 * One row per bank account: what the bank says, what the ledger says, and
	 * what is left in the reconcile inbox. The difference column is the point of
	 * the report — it is zero exactly when nothing is outstanding.
	 */
	interface Row {
		account: Account;
		balance: StatementBalance | null;
	}

	let rows = $state<Row[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);
	let err = $state('');

	async function load() {
		loading = true;
		err = '';
		try {
			const [accounts, o] = await Promise.all([
				accountApi.list({ type: 'BANK', status: 'ACTIVE' }),
				orgApi.current().catch(() => null)
			]);
			org = o ?? null;
			rows = await Promise.all(
				(accounts ?? []).map(async (account) => ({
					account,
					balance: (await statementApi.balance(account.AccountID).catch(() => null))?.Balance ?? null
				}))
			);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not load the bank accounts';
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if ($session.tenantId) void load();
	});

	// Amounts are shown in the organisation's own currency; a per-account code
	// is only a fallback for the rare account that carries none.
	const baseCurrency = $derived(org?.BaseCurrency ?? 'USD');

	// Null until the account has a statement balance to compare against, so the
	// row prints "—" rather than a zero it invented.
	const unreconciled = (r: Row) =>
		r.balance ? Number(r.balance.StatementBalance) - Number(r.balance.LedgerBalance) : null;

	const money = (value: string | number | null | undefined) =>
		value === undefined || value === null ? '—' : formatCurrency(Number(value), baseCurrency);
</script>

<header class="mb-5 flex flex-wrap items-start justify-between gap-3">
	<div>
		<h1 class="section-title">Bank Reconciliation</h1>
		<p class="text-sm muted mt-1">
			Compare your balance in Xero with your bank balance for every bank account, and see how
			many statement lines are still waiting.
		</p>
	</div>
	<button class="btn-secondary" onclick={load} disabled={loading}>Refresh</button>
</header>

{#if err}
	<p class="text-sm text-red-700 mb-3" role="alert">{err}</p>
{/if}

<div class="card overflow-hidden">
	{#if loading}
		<p class="p-8 text-center muted">Loading…</p>
	{:else if rows.length === 0}
		<p class="p-8 text-center muted">No active bank accounts yet.</p>
	{:else}
		<div class="overflow-x-auto">
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-4 py-2 text-left">Bank account</th>
						<th class="px-4 py-2 text-left">Last statement</th>
						<th class="px-4 py-2 text-right">Statement balance</th>
						<th class="px-4 py-2 text-right">Balance in Xero</th>
						<th class="px-4 py-2 text-right">Unreconciled</th>
						<th class="px-4 py-2 text-right">Reconciled</th>
						<th class="px-4 py-2 text-left">Feed</th>
						<th class="px-4 py-2 text-left"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each rows as r (r.account.AccountID)}
						{@const currency = r.account.CurrencyCode || baseCurrency}
						<tr class="hover:bg-ink-50">
							<td class="px-4 py-2">
								<div>{r.account.Name}</div>
								<div class="text-xs muted tabular-nums">
									{r.account.BankAccountNumber || r.account.Code}
								</div>
							</td>
							<td class="px-4 py-2 tabular-nums">
								{r.balance?.LastStatementEnd ? formatDate(r.balance.LastStatementEnd) : '—'}
							</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{money(r.balance?.StatementBalance)}
							</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{money(r.balance?.LedgerBalance)}
							</td>
							<td
								class="px-4 py-2 text-right tabular-nums {unreconciled(r) !== null &&
								Math.abs(unreconciled(r) ?? 0) > 0.004
									? 'text-amber-700 font-semibold'
									: 'text-emerald-700'}"
							>
								{money(unreconciled(r) ?? undefined)}
							</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{r.balance ? r.balance.ReconciledCount : '—'}
							</td>
							<td class="px-4 py-2 muted">
								{r.balance?.LastSyncAt ? formatDate(r.balance.LastSyncAt) : 'Not connected'}
							</td>
							<td class="px-4 py-2 text-right">
								<a
									class="btn-secondary-sm"
									href="/app/accounting/bank-accounts/{r.account.AccountID}?tab=reconcile"
								>
									Reconcile
								</a>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
