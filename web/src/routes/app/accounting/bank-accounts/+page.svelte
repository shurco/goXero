<script lang="ts">
	import { onMount } from 'svelte';
	import { accountApi, orgApi, statementApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import BankBalanceChart from '$lib/components/BankBalanceChart.svelte';
	import { formatAmount, formatCurrency, xeroDate } from '$lib/utils/format';
	import type { Account, LedgerBalancePoint, Organisation, StatementBalance } from '$lib/types';

	interface BankAccountRow {
		account: Account;
		balance: StatementBalance | null;
		chartSeries: LedgerBalancePoint[];
	}

	let accounts = $state<Account[]>([]);
	let rows = $state<BankAccountRow[]>([]);
	let org = $state<Organisation | null>(null);
	let loading = $state(true);
	let err = $state('');
	let headerMenuOpen = $state(false);
	let cardMenuId = $state<string | null>(null);

	/**
	 * The card's two figures are the two the reconcile screen already shows, so
	 * they are read from the endpoint that computes them rather than re-derived
	 * here from a page of bank transactions. Xero's card is the same two numbers
	 * for the same reason.
	 */
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

			rows = await Promise.all(
				accounts.map(async (a) => {
					const id = a.AccountID ?? '';
					if (!id) return { account: a, balance: null, chartSeries: [] } satisfies BankAccountRow;
					const [bal, series] = await Promise.all([
						statementApi
							.balance(id)
							.then((r) => r?.Balance ?? null)
							.catch(() => null),
						statementApi
							.balanceSeries(id)
							.then((r) => r?.BalanceSeries ?? [])
							.catch(() => [] as LedgerBalancePoint[])
					]);
					return {
						account: a,
						balance: bal,
						chartSeries: series
					} satisfies BankAccountRow;
				})
			);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load bank accounts';
		} finally {
			loading = false;
		}
	}

	onMount(() => {
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

	/**
	 * Xero prints a balance held in the organisation's base currency as a bare
	 * number — "7,430.22", not "US$7,430.22" — and names the currency only once
	 * the account is held in something else. The shared formatCurrency always
	 * prefixes, so the card formats its own two figures rather than changing
	 * what every other page shows.
	 */
	function formatBalance(value: number | string | undefined, accountCurrency: string) {
		if (accountCurrency !== currency) return formatCurrency(value, accountCurrency);
		return formatAmount(value);
	}

	/**
	 * Xero's empty card stands for an account the bank has sent nothing for.
	 * The statement-line counts are the test: an account with no reconciled and
	 * no unreconciled lines has had nothing imported into it.
	 */
	function isEmpty(row: BankAccountRow) {
		const b = row.balance;
		if (!b) return false;
		return Number(b.ReconciledCount ?? 0) + Number(b.UnreconciledCount ?? 0) === 0;
	}

</script>

<!-- Xero's page surface (rgb(242,243,244)), bled to the viewport so the grey is
     the page and not a panel sitting on goXero's own background. -->
<div
	class="-mt-6 -mb-6 ml-[calc(50%-50vw)] w-screen min-h-[calc(100vh-3.5rem)] bg-[#f2f3f4] px-5 pt-6 pb-6 lg:-mt-8 lg:-mb-8 lg:pt-8 lg:pb-8"
>
	<div class="mx-auto w-full max-w-[1360px]">
		<div class="mb-5 flex h-[60px] items-center justify-between">
			<h1 class="pr-3 text-[17px] leading-6 font-bold text-xero-ink">Bank accounts</h1>
			<div class="flex items-center gap-3" data-bank-header-menu>
				<a
					href="/app/accounting/bank-rules"
					class="inline-flex h-8 items-center justify-center rounded-[3px] border border-[#a6a9b0] bg-white px-3 py-[5px] text-[13px] leading-5 font-bold text-xero-blue hover:bg-[#f5f7f9]"
				>
					Manage bank rules
				</a>
				<a
					href="/app/accounting/bank-accounts/new"
					class="inline-flex h-8 items-center justify-center rounded-[3px] border border-xero-blue bg-xero-blue px-3 py-[5px] text-[13px] leading-5 font-bold text-white hover:bg-xero-blueDark"
				>
					Add bank account
				</a>
				<div class="relative -mr-1">
					<button
						type="button"
						class="flex h-8 w-8 items-center justify-center rounded-full text-[rgba(0,10,30,0.65)] hover:bg-black/5"
						aria-label="Additional actions"
						aria-expanded={headerMenuOpen}
						onclick={() => (headerMenuOpen = !headerMenuOpen)}
					>
						<svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
							<circle cx="12" cy="6" r="1.5" />
							<circle cx="12" cy="12" r="1.5" />
							<circle cx="12" cy="18" r="1.5" />
						</svg>
					</button>
					{#if headerMenuOpen}
						<div
							class="absolute right-0 z-40 mt-1 min-w-[200px] rounded-[3px] border border-[#ccced2] bg-white py-1 shadow-pop"
							role="menu"
						>
							<a class="nav-dropdown-item block" href="/app/bank-feeds" role="menuitem">Bank feeds</a>
							<a class="nav-dropdown-item block" href="/app/reports/bank-summary" role="menuitem"
								>Bank summary report</a
							>
						</div>
					{/if}
				</div>
			</div>
		</div>

		{#if err}
			<p class="mb-3 text-[13px] text-red-700" role="alert">{err}</p>
		{/if}

		{#if loading}
			<div
				class="rounded-[3px] bg-white p-12 text-center text-[13px] text-[rgba(0,10,30,0.65)] shadow-[0_0_0_1px_rgba(0,10,30,0.2)]"
			>
				Loading…
			</div>
		{:else if rows.length === 0}
			<div
				class="rounded-[3px] bg-white p-12 text-center shadow-[0_0_0_1px_rgba(0,10,30,0.2)]"
			>
				<h2 class="mb-1 text-[15px] leading-6 font-bold text-xero-ink">No bank accounts yet</h2>
				<p class="mb-6 text-[13px] leading-5 text-[rgb(50,70,90)]">
					Connect a bank or add one manually to start reconciling transactions.
				</p>
				<a
					href="/app/accounting/bank-accounts/new"
					class="inline-flex h-8 items-center justify-center rounded-[3px] border border-xero-blue bg-xero-blue px-3 py-[5px] text-[13px] leading-5 font-bold text-white hover:bg-xero-blueDark"
				>
					Add bank account
				</a>
			</div>
		{:else}
			<div class="flex w-full min-w-0 flex-col gap-6">
				{#each rows as r (r.account.AccountID)}
					{@const id = r.account.AccountID ?? ''}
					{@const cur = r.account.CurrencyCode ?? currency}
					{@const bal = r.balance}
					{@const empty = isEmpty(r)}
					<article class="rounded-[3px] bg-white shadow-[0_0_0_1px_rgba(0,10,30,0.2)]">
						<div class="flex h-[90px] px-5 py-4">
							<div class="min-w-0 flex-1">
								<a
									class="block truncate text-[17px] leading-7 font-bold text-xero-blue hover:underline"
									href={`/app/accounting/bank-accounts/${id}`}
								>
									{r.account.Name}
								</a>
								<span class="mt-2 block text-[13px] leading-4 font-bold text-[rgba(0,10,30,0.75)]">
									{r.account.BankAccountNumber || r.account.Code || '—'}
								</span>
							</div>
							<div class="relative shrink-0 self-center" data-bank-acct-menu>
								<button
									type="button"
									class="flex h-10 w-10 items-center justify-center rounded-full text-[rgba(0,10,30,0.65)] hover:bg-black/5"
									aria-label="Account menu"
									aria-expanded={cardMenuId === id}
									onclick={(e) => {
										e.stopPropagation();
										cardMenuId = cardMenuId === id ? null : id;
									}}
								>
									<svg class="h-[13px] w-[3px]" viewBox="0 0 3 13" fill="currentColor" aria-hidden="true">
										<circle cx="1.5" cy="1.5" r="1.5" />
										<circle cx="1.5" cy="6.5" r="1.5" />
										<circle cx="1.5" cy="11.5" r="1.5" />
									</svg>
								</button>
								{#if cardMenuId === id}
									<div
										class="absolute right-0 z-30 mt-1 w-[min(100vw-2rem,220px)] rounded-[3px] border border-[#ccced2] bg-white py-1 shadow-pop"
										role="menu"
									>
										<a class="nav-dropdown-item" href={`/app/bank-transactions?accountId=${id}`}
											>Account transactions</a
										>
										<a class="nav-dropdown-item" href="/app/bank-feeds">Manage bank feeds</a>
										<a class="nav-dropdown-item" href={`/app/accounting/bank-accounts/${id}?tab=statements`}
											>Import bank statement</a
										>
										<a class="nav-dropdown-item" href={`/app/accounting/bank-accounts/${id}/edit`}
											>Edit account details</a
										>
										<a class="nav-dropdown-item" href="/app/reports/bank-summary">Bank summary report</a>
									</div>
								{/if}
							</div>
						</div>

						<!-- Xero rules off the 90px header with a 1px line across the whole card,
						     so the card reads as header + body rather than one block. -->
						<div class="border-t border-[#ccced2]">
							<div class="p-5">
								{#if empty}
									<div class="pt-0">
										<div class="text-[15px] leading-6 font-bold text-xero-ink">No transactions imported</div>
										<a
											href={`/app/accounting/bank-accounts/${id}?tab=statements`}
											class="mt-3 inline-flex h-8 items-center justify-center rounded-[3px] border border-xero-blue bg-xero-blue px-3 py-[5px] text-[13px] leading-5 font-bold text-white hover:bg-xero-blueDark"
										>
											Import a bank statement
										</a>
									</div>
								{:else}
									<div>
										{#if bal && Number(bal.UnreconciledCount) > 0}
											<a
												href={`/app/accounting/bank-accounts/${id}?tab=reconcile`}
												class="mb-3 inline-flex h-8 items-center justify-center rounded-[3px] border border-xero-blue bg-xero-blue px-3 py-[5px] text-[13px] leading-5 font-bold text-white hover:bg-xero-blueDark"
											>
												Reconcile {bal.UnreconciledCount} item{Number(bal.UnreconciledCount) === 1
													? ''
													: 's'}
											</a>
										{/if}
										<table class="w-full border-collapse">
											<tbody>
												<tr class="h-6">
													<td
														class="w-[615px] pt-0 pr-[25px] pb-2 pl-0 text-left align-middle text-[13px] leading-4 font-normal whitespace-nowrap text-[rgb(50,70,90)]"
													>
														Balance in goXero
													</td>
													<td class="w-[187px] pt-0 pr-0 pb-2 pl-0 text-right align-middle text-[13px] leading-4 font-normal text-[rgb(50,70,90)]">
														{formatBalance(Number(bal?.LedgerBalance ?? 0), cur)}
													</td>
												</tr>
												<tr class="h-6">
													<td
														class="w-[615px] pt-0 pr-[25px] pb-2 pl-0 text-left align-middle text-[13px] leading-4 font-normal text-[rgb(50,70,90)]"
													>
														Statement balance{#if bal?.LastStatementEnd}{' '}<span>({xeroDate(
																bal.LastStatementEnd
															)})</span>{/if}
													</td>
													<td class="w-[187px] pt-0 pr-0 pb-2 pl-0 text-right align-middle text-[13px] leading-4 font-normal text-[rgb(50,70,90)]">
														{formatBalance(Number(bal?.StatementBalance ?? 0), cur)}
													</td>
												</tr>
											</tbody>
										</table>
									</div>
								{/if}
							</div>

							<!-- Xero draws one balance graph per account, from the ledger balance and
							     not from the statement lines: the two are different numbers on any
							     account with an unworked inbox. -->
							{#if !empty}
								<div class="px-5">
									<BankBalanceChart series={r.chartSeries} currency={cur} baseCurrency={currency} />
								</div>
							{/if}
						</div>
					</article>
				{/each}
			</div>
		{/if}
	</div>
</div>
