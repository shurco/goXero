<script lang="ts">
	import { onMount } from 'svelte';
	import { accountApi, bankTransactionApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { runningBalanceSeries } from '$lib/bank-balance-chart';
	import { bankBalancesFromTransactions } from '$lib/dashboard-utils';
	import BankBalanceChart from '$lib/components/BankBalanceChart.svelte';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, BankTransaction, Organisation } from '$lib/types';

	interface BankAccountRow {
		account: Account;
		reconcileCount: number;
		statementBalance: number;
		xeroBalance: number;
		chartSeries: { t: number; balance: number }[];
		lastActivityDate?: string;
		hasTransactions: boolean;
	}

	let accounts = $state<Account[]>([]);
	let rows = $state<BankAccountRow[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);
	let err = $state('');
	let headerMenuOpen = $state(false);
	let cardMenuId = $state<string | null>(null);

	function fmtBal(value: number, cur: string) {
		const abs = formatCurrency(Math.abs(value), cur);
		return value < 0 ? `(${abs})` : abs;
	}

	async function reload() {
		loading = true;
		err = '';
		try {
			const [accs, o] = await Promise.all([
				accountApi.list({ status: 'ACTIVE' }).catch(() => [] as Account[]),
				orgApi.current().catch(() => null)
			]);
			accounts = accs.filter((a) => a.Type === 'BANK');
			org = o ?? null;
			const cur = org?.BaseCurrency || 'USD';

			const results = await Promise.all(
				accounts.map(async (a) => {
					try {
						const res = await bankTransactionApi.list({
							accountId: a.AccountID ?? '',
							pageSize: '500'
						});
						const txs = (res?.BankTransactions ?? []) as BankTransaction[];
						const unrec = txs.filter((t) => !t.IsReconciled).length;
						const { statement, xero } = bankBalancesFromTransactions(txs, cur);
						const dated = txs.filter((t) => t.Date).sort((x, y) => new Date(x.Date!).getTime() - new Date(y.Date!).getTime());
						const lastActivityDate = dated[dated.length - 1]?.Date;
						const chartSeries = runningBalanceSeries(txs);
						const hasTransactions = txs.length > 0;
						return {
							account: a,
							reconcileCount: unrec,
							statementBalance: statement,
							xeroBalance: xero,
							chartSeries,
							lastActivityDate,
							hasTransactions
						} satisfies BankAccountRow;
					} catch {
						return {
							account: a,
							reconcileCount: 0,
							statementBalance: 0,
							xeroBalance: 0,
							chartSeries: [],
							hasTransactions: false
						} satisfies BankAccountRow;
					}
				})
			);
			rows = results;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load bank accounts';
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void reload();
		function closeMenus(e: MouseEvent) {
			const el = e.target as HTMLElement;
			if (!el.closest('[data-bank-acct-menu]')) cardMenuId = null;
			if (!el.closest('[data-bank-header-menu]')) headerMenuOpen = false;
		}
		document.addEventListener('click', closeMenus);
		return () => document.removeEventListener('click', closeMenus);
	});
	$effect(() => {
		if ($session.tenantId) void reload();
	});

	const currency = $derived(org?.BaseCurrency || 'USD');
	const totalReconcile = $derived(rows.reduce((s, r) => s + r.reconcileCount, 0));
</script>

<div class="mb-6 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
	<div>
		<h1 class="section-title">Bank accounts</h1>
	</div>
	<div class="flex flex-wrap items-center gap-2" data-bank-header-menu>
		<a href="/app/accounting/bank-rules" class="btn-secondary">Manage bank rules</a>
		<a href="/app/accounting/bank-accounts/new" class="btn-primary">Add bank account</a>
		<div class="relative">
			<button
				type="button"
				class="inline-flex h-10 w-10 items-center justify-center rounded-md border border-ink-200 bg-white text-ink-600 hover:bg-ink-50"
				aria-label="More actions"
				aria-expanded={headerMenuOpen}
				onclick={() => (headerMenuOpen = !headerMenuOpen)}
			>
				<svg class="h-5 w-5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
					<circle cx="12" cy="6" r="1.5" />
					<circle cx="12" cy="12" r="1.5" />
					<circle cx="12" cy="18" r="1.5" />
				</svg>
			</button>
			{#if headerMenuOpen}
				<div
					class="absolute right-0 z-40 mt-1 min-w-[200px] rounded-lg border border-ink-100 bg-white py-1 shadow-pop"
					role="menu"
				>
					<a class="nav-dropdown-item block" href="/app/bank-feeds" role="menuitem">Bank feeds</a>
					<a class="nav-dropdown-item block" href="/app/reports/bank-summary" role="menuitem">Bank summary report</a>
				</div>
			{/if}
		</div>
	</div>
</div>

{#if err}
	<p class="mb-3 text-sm text-red-700" role="alert">{err}</p>
{/if}

{#if loading}
	<div class="card p-12 text-center text-ink-500">Loading…</div>
{:else if rows.length === 0}
	<div class="card p-12 text-center">
		<h2 class="content-section-title mb-1">No bank accounts yet</h2>
		<p class="mb-6 text-sm text-ink-600">Connect a bank or add one manually to start reconciling transactions.</p>
		<a href="/app/accounting/bank-accounts/new" class="btn-primary">Add bank account</a>
	</div>
{:else}
	<p class="mb-4 text-xs text-ink-600">
		{rows.length} account{rows.length === 1 ? '' : 's'}
		{#if totalReconcile > 0}
			<span class="text-ink-400"> · </span>
			{totalReconcile} item{totalReconcile === 1 ? '' : 's'} to reconcile
		{/if}
	</p>

	<div class="flex w-full min-w-0 flex-col gap-4">
		{#each rows as r (r.account.AccountID)}
			{@const id = r.account.AccountID ?? ''}
			{@const cur = r.account.CurrencyCode ?? currency}
			<article
				class="overflow-hidden rounded-md border border-ink-200 bg-white shadow-sm"
			>
				<div class="flex items-start justify-between gap-2 border-b border-ink-200 px-4 py-3">
					<div class="min-w-0">
						<a
							class="text-sm font-semibold leading-snug text-brand-500 hover:underline"
							href={`/app/accounting/bank-accounts/${id}`}
						>
							{r.account.Name}
						</a>
						<div class="mt-0.5 text-[11px] leading-normal text-ink-600">
							{r.account.BankAccountNumber || r.account.Code || '—'}
						</div>
					</div>
					<div class="relative shrink-0" data-bank-acct-menu>
						<button
							type="button"
							class="rounded p-1 text-ink-400 hover:bg-ink-50 hover:text-ink-500"
							aria-label="Account menu"
							aria-expanded={cardMenuId === id}
							onclick={(e) => {
								e.stopPropagation();
								cardMenuId = cardMenuId === id ? null : id;
							}}
						>
							<svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
								<circle cx="12" cy="6" r="1.5" />
								<circle cx="12" cy="12" r="1.5" />
								<circle cx="12" cy="18" r="1.5" />
							</svg>
						</button>
						{#if cardMenuId === id}
							<div
								class="absolute right-0 z-30 mt-1 w-[min(100vw-2rem,220px)] rounded-xl border border-ink-100 bg-white py-1 shadow-pop"
								role="menu"
							>
								<a class="nav-dropdown-item" href={`/app/bank-transactions?accountId=${id}`}>Account transactions</a>
								<a class="nav-dropdown-item" href="/app/bank-feeds">Manage bank feeds</a>
								<a class="nav-dropdown-item" href={`/app/accounting/bank-accounts/${id}?tab=statements`}>Import bank statement</a>
								<a class="nav-dropdown-item" href={`/app/accounting/bank-accounts/${id}/edit`}>Edit account details</a>
								<a class="nav-dropdown-item" href="/app/reports/bank-summary">Bank summary report</a>
							</div>
						{/if}
					</div>
				</div>

				{#if !r.hasTransactions}
					<div class="flex flex-col items-center px-5 py-12 text-center">
						<p class="text-base font-semibold text-ink-900">No transactions imported</p>
						<a
							href={`/app/accounting/bank-accounts/${id}?tab=statements`}
							class="btn-primary mt-5"
						>
							Import a bank statement
						</a>
					</div>
				{:else}
					<div
						class="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
					>
						<div class="flex min-w-0 shrink-0 flex-col">
							{#if r.reconcileCount > 0}
								<a
									href={`/app/accounting/bank-accounts/${id}?tab=reconcile`}
									class="inline-flex w-fit items-center rounded bg-brand-500 px-3 py-1.5 text-xs font-semibold text-white shadow-sm hover:bg-brand-600"
								>
									Reconcile {r.reconcileCount} item{r.reconcileCount === 1 ? '' : 's'}
								</a>
							{:else}
								<span
									class="inline-flex w-fit items-center gap-1.5 rounded-full bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-800"
								>
									<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
										><path
											d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1 14.5-4.5-4.5 1.4-1.4 3.1 3 6.6-6.6 1.4 1.4-8 8z"
										/></svg
									>
									Up to date
								</span>
							{/if}
						</div>
						<div
							class="grid w-full min-w-0 flex-1 grid-cols-[1fr_auto] gap-x-4 gap-y-1 sm:ml-auto sm:max-w-[17rem]"
						>
							<span class="text-[11px] leading-tight text-ink-600">Balance in goXero</span>
							<span class="text-right text-xs font-semibold tabular-nums text-ink-900">{fmtBal(r.xeroBalance, cur)}</span>
							<span class="text-[11px] leading-tight text-ink-600">
								Statement balance{#if r.lastActivityDate}<span class="text-ink-500">
										({formatDate(r.lastActivityDate, 'MMM D')})</span
									>{/if}
							</span>
							<span class="text-right text-xs font-semibold tabular-nums text-ink-900">
								{formatCurrency(r.statementBalance, cur)}
							</span>
						</div>
					</div>

					<BankBalanceChart series={r.chartSeries} compact={true} />
				{/if}
			</article>
		{/each}
	</div>
{/if}
