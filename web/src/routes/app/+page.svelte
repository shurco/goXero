<script lang="ts">
	import { onMount } from 'svelte';
	import { accountApi, orgApi, bankTransactionApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, BankTransaction, Organisation } from '$lib/types';

	interface BankTile {
		account: Account;
		statementBalance: number;
		xeroBalance: number;
		lastStatementDate?: string;
		unreconciled: number;
	}

	let loading = $state(true);
	let org = $state<Organisation | null>(null);
	let tiles = $state<BankTile[]>([]);
	let openMenuFor = $state<string | null>(null);

	function toggleMenu(id: string) {
		openMenuFor = openMenuFor === id ? null : id;
	}

	onMount(() => {
		function onDocClick(e: MouseEvent) {
			const t = e.target as HTMLElement;
			if (!t.closest('[data-tile-menu]')) openMenuFor = null;
		}
		function onKey(e: KeyboardEvent) { if (e.key === 'Escape') openMenuFor = null; }
		document.addEventListener('mousedown', onDocClick);
		document.addEventListener('keydown', onKey);
		return () => {
			document.removeEventListener('mousedown', onDocClick);
			document.removeEventListener('keydown', onKey);
		};
	});

	async function reload() {
		loading = true;
		try {
			const [accounts, organisation] = await Promise.all([
				accountApi.list({ status: 'ACTIVE' }).catch(() => [] as Account[]),
				orgApi.current().catch(() => null)
			]);
			org = organisation ?? null;

			const bankAccounts = accounts.filter((a) => a.Type === 'BANK');
			const built: BankTile[] = [];
			for (const acc of bankAccounts) {
				let latest: BankTransaction | undefined;
				let unreconciled = 0;
				try {
					const res = await bankTransactionApi.list({
						accountId: acc.AccountID,
						pageSize: '25'
					});
					latest = res.BankTransactions?.[0];
					unreconciled = res.BankTransactions?.filter(
						(t) => !t.IsReconciled
					).length ?? 0;
				} catch {
					/* ignore — empty */
				}
				built.push({
					account: acc,
					statementBalance: 0,
					xeroBalance: 0,
					lastStatementDate: latest?.Date,
					unreconciled
				});
			}
			tiles = built;
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		if ($session.tenantId) void reload();
	});

	const currency = $derived(org?.BaseCurrency || 'USD');
</script>

<div class="space-y-6">
	<div class="flex items-end justify-between flex-wrap gap-3">
		<div>
			<h1 class="section-title">{org?.Name ?? 'Your business'}</h1>
			<p class="muted mt-0.5 text-sm">
				{#if $session.firstName}
					Last login by <span class="text-ink-700">{$session.firstName}</span>
				{/if}
				· {currency}
			</p>
		</div>
		<div class="flex items-center gap-2">
			<button type="button" class="btn-secondary">
				<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current"><path d="M3 17.25V21h3.75l11-11-3.75-3.75-11 11zM20.7 7.04a1 1 0 0 0 0-1.41L18.37 3.3a1 1 0 0 0-1.41 0l-1.84 1.83 3.75 3.75 1.84-1.84z" /></svg>
				Edit homepage
			</button>
		</div>
	</div>

	<div class="flex items-center justify-between">
		<h2 class="text-lg font-semibold text-ink-900">Business Overview</h2>
	</div>

	<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5">
		{#if loading && tiles.length === 0}
			{#each Array(3) as _}
				<div class="card p-5 animate-pulse h-40"></div>
			{/each}
		{:else if tiles.length === 0}
			<div class="card p-8 col-span-full text-center">
				<div class="muted mb-3">No bank accounts yet.</div>
				<a href="/app/bank-feeds" class="btn-primary">Connect a bank</a>
			</div>
		{:else}
			{#each tiles as tile}
				<div class="card p-5 flex flex-col gap-3">
					<div class="flex items-start justify-between gap-3">
						<div>
							<div class="font-semibold text-ink-900">{tile.account.Name}</div>
							<div class="text-xs muted mt-0.5">
								{tile.account.BankAccountNumber || tile.account.Code}
							</div>
						</div>
						<div class="relative" data-tile-menu>
							<button
								class="text-ink-400 hover:text-ink-600"
								aria-label="More"
								aria-haspopup="menu"
								aria-expanded={openMenuFor === tile.account.AccountID}
								onclick={() => toggleMenu(tile.account.AccountID)}
							>
								<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M12 8a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 6a2 2 0 1 0 0-4 2 2 0 0 0 0 4zm0 6a2 2 0 1 0 0-4 2 2 0 0 0 0 4z" /></svg>
							</button>
							{#if openMenuFor === tile.account.AccountID}
								<div class="absolute right-0 mt-1 min-w-[220px] rounded-lg bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-30" role="menu">
									<a class="nav-dropdown-item" href="/app/bank-transactions?accountId={tile.account.AccountID}">Account transactions</a>
									<a class="nav-dropdown-item" href="/app/bank-feeds">Manage bank feeds</a>
									<a class="nav-dropdown-item" href="/app/accounting/bank-accounts">Edit account details</a>
									<a class="nav-dropdown-item" href="/app/reports/bank-summary">Bank summary report</a>
								</div>
							{/if}
						</div>
					</div>

					<div class="grid grid-cols-2 gap-3 pt-1">
						<div>
							<div class="text-2xl font-semibold tabular-nums">
								{formatCurrency(tile.statementBalance, tile.account.CurrencyCode || currency)}
							</div>
							<div class="text-xs text-brand-600 hover:underline cursor-pointer">
								Statement balance{tile.lastStatementDate ? ` (${formatDate(tile.lastStatementDate)})` : ''}
							</div>
						</div>
						<div>
							<div class="text-2xl font-semibold tabular-nums">
								{formatCurrency(tile.xeroBalance, tile.account.CurrencyCode || currency)}
							</div>
							<div class="text-xs text-brand-600 hover:underline cursor-pointer">
								Balance in Xero
							</div>
						</div>
					</div>

					<div class="mt-auto pt-3 border-t border-ink-100 flex items-center justify-between">
						{#if tile.unreconciled > 0}
							<span class="text-xs muted">
								Balance difference
								<span class="ml-1 font-semibold text-ink-900">
									{formatCurrency(tile.statementBalance - tile.xeroBalance, tile.account.CurrencyCode || currency)}
								</span>
							</span>
							<a href="/app/bank-transactions?accountId={tile.account.AccountID}" class="btn-primary !py-1.5 !px-3 !text-xs rounded-full">
								Reconcile {tile.unreconciled} items
							</a>
						{:else}
							<span class="inline-flex items-center gap-1 text-xs text-emerald-700">
								<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1 14.5-4.5-4.5 1.4-1.4 3.1 3 6.6-6.6 1.4 1.4-8 8z" /></svg>
								Reconciled
							</span>
							<a href="/app/bank-transactions?accountId={tile.account.AccountID}" class="text-xs text-brand-600 hover:underline">
								View account transactions
							</a>
						{/if}
					</div>
				</div>
			{/each}
		{/if}
	</div>
</div>
