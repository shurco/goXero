<script lang="ts">
	import { session } from '$lib/stores/session';
	import { accountApi, orgApi, statementApi } from '$lib/api';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, BankStatementLine, Organisation } from '$lib/types';

	/**
	 * Every statement line still waiting for a decision, across all bank
	 * accounts. This is what Xero calls the "Uncoded Statement Lines" report:
	 * the list you hand to a client so they can tell you what each line was.
	 */
	let lines = $state<BankStatementLine[]>([]);
	let accounts = $state<Account[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);
	let err = $state('');
	let search = $state('');
	let accountFilter = $state('');

	// Statement lines carry an empty currency code; fall through to the account's
	// and then the organisation's rather than printing a symbol-less amount.
	const currencyOf = (l: BankStatementLine) =>
		l.CurrencyCode ||
		accounts.find((a) => a.AccountID === l.BankAccountID)?.CurrencyCode ||
		org?.BaseCurrency ||
		'USD';

	const accountName = (id: string | undefined) => {
		const a = accounts.find((x) => x.AccountID === id);
		return a ? `${a.Code} · ${a.Name}` : '—';
	};

	const rows = $derived(
		lines
			.filter((l) => (accountFilter ? l.BankAccountID === accountFilter : true))
			.filter((l) => {
				if (!search.trim()) return true;
				const q = search.toLowerCase();
				return `${l.Payee} ${l.Description} ${l.Reference} ${l.Counterparty}`
					.toLowerCase()
					.includes(q);
			})
			.sort((a, b) => (a.PostedAt < b.PostedAt ? -1 : 1))
	);

	// Totals are shown per direction rather than netted: a client reading this
	// list wants to see how much is waiting each way, and netting hides the size
	// of the pile when money in and money out happen to be similar.
	const moneyOut = $derived(
		rows.reduce((sum, l) => sum + Math.min(0, Number(l.Amount ?? 0)), 0)
	);
	const moneyIn = $derived(rows.reduce((sum, l) => sum + Math.max(0, Number(l.Amount ?? 0)), 0));

	async function load() {
		loading = true;
		err = '';
		try {
			const [res, accs, o] = await Promise.all([
				statementApi.list({ status: 'NEW', pageSize: '500' }),
				accountApi.list({}).catch(() => []),
				orgApi.current().catch(() => null)
			]);
			lines = res?.StatementLines ?? [];
			accounts = accs ?? [];
			org = o ?? null;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not load the statement lines';
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if ($session.tenantId) void load();
	});

	const bankAccounts = $derived(accounts.filter((a) => a.Type === 'BANK'));
	// The list spans accounts, so the totals are shown in the organisation's own
	// currency rather than in whichever account a row happens to belong to.
	const baseCurrency = $derived(org?.BaseCurrency ?? 'USD');
</script>

<header class="mb-5 flex flex-wrap items-start justify-between gap-3">
	<div>
		<h1 class="section-title">Uncoded Statement Lines</h1>
		<p class="text-sm muted mt-1">
			Statement lines from the bank feed and from imported statements that have not been coded
			yet. Once a line is coded it becomes a transaction and leaves this list.
		</p>
	</div>
	<div class="flex flex-wrap items-end gap-2">
		<label class="block">
			<span class="label">Bank account</span>
			<select class="input" bind:value={accountFilter}>
				<option value="">All accounts</option>
				{#each bankAccounts as a (a.AccountID)}
					<option value={a.AccountID}>{a.Code} · {a.Name}</option>
				{/each}
			</select>
		</label>
		<label class="block">
			<span class="label">Search</span>
			<input class="input" placeholder="Payee or reference" bind:value={search} />
		</label>
		<button class="btn-secondary" onclick={load} disabled={loading}>Refresh</button>
	</div>
</header>

{#if err}
	<p class="text-sm text-red-700 mb-3" role="alert">{err}</p>
{/if}

<div class="card overflow-hidden">
	{#if loading}
		<p class="p-8 text-center muted">Loading…</p>
	{:else if rows.length === 0}
		<p class="p-8 text-center muted">
			Nothing is waiting to be coded — every statement line has been dealt with.
		</p>
	{:else}
		<div class="overflow-x-auto">
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-4 py-2 text-left">Bank account</th>
						<th class="px-4 py-2 text-left">Date</th>
						<th class="px-4 py-2 text-left">Payee</th>
						<th class="px-4 py-2 text-left">Description</th>
						<th class="px-4 py-2 text-left">Reference</th>
						<th class="px-4 py-2 text-left">Source</th>
						<th class="px-4 py-2 text-right">Spent</th>
						<th class="px-4 py-2 text-right">Received</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each rows as l (l.StatementLineID)}
						{@const amount = Number(l.Amount ?? 0)}
						<tr class="hover:bg-ink-50">
							<td class="px-4 py-2">
								{#if l.BankAccountID}
									<a
										class="text-brand-600 hover:underline"
										href="/app/accounting/bank-accounts/{l.BankAccountID}?tab=reconcile"
									>
										{accountName(l.BankAccountID)}
									</a>
								{:else}
									{accountName(l.BankAccountID)}
								{/if}
							</td>
							<td class="px-4 py-2 tabular-nums">{formatDate(l.PostedAt)}</td>
							<td class="px-4 py-2">{l.Payee || l.Counterparty || '—'}</td>
							<td class="px-4 py-2">{l.Description ?? ''}</td>
							<td class="px-4 py-2">{l.Reference ?? ''}</td>
							<td class="px-4 py-2 muted">{l.Source === 'FEED' ? 'Bank feed' : 'Imported'}</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{amount < 0 ? formatCurrency(Math.abs(amount), currencyOf(l)) : ''}
							</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{amount >= 0 ? formatCurrency(amount, currencyOf(l)) : ''}
							</td>
						</tr>
					{/each}
				</tbody>
				<tfoot class="bg-ink-50">
					<tr>
						<td class="px-4 py-2 muted" colspan="6">{rows.length} statement line(s) waiting</td>
						<td class="px-4 py-2 text-right tabular-nums">
							{formatCurrency(Math.abs(moneyOut), baseCurrency)}
						</td>
						<td class="px-4 py-2 text-right tabular-nums">
							{formatCurrency(moneyIn, baseCurrency)}
						</td>
					</tr>
				</tfoot>
			</table>
		</div>
	{/if}
</div>
