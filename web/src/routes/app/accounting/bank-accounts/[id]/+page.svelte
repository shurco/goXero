<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { accountApi, bankTransactionApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, BankTransaction, Organisation } from '$lib/types';

	type TabId = 'reconcile' | 'cash-coding' | 'statements' | 'transactions' | 'period';

	const TABS: { id: TabId; label: string }[] = [
		{ id: 'reconcile', label: 'Reconcile' },
		{ id: 'cash-coding', label: 'Cash coding' },
		{ id: 'statements', label: 'Bank statements' },
		{ id: 'transactions', label: 'Account transactions' },
		{ id: 'period', label: 'Reconcile period' }
	];

	let accountId = $derived($page.params.id ?? '');
	let account = $state<Account | null>(null);
	let org = $state<Organisation | null>(null);
	let transactions = $state<BankTransaction[]>([]);
	let loading = $state(true);
	let err = $state('');

	let activeTab = $state<TabId>('reconcile');

	$effect(() => {
		const q = ($page.url.searchParams.get('tab') ?? '') as TabId;
		if (TABS.some((t) => t.id === q)) activeTab = q;
	});

	function setTab(t: TabId) {
		activeTab = t;
		const u = new URL($page.url.toString());
		u.searchParams.set('tab', t);
		void goto(u.pathname + '?' + u.searchParams.toString(), {
			replaceState: true,
			keepFocus: true,
			noScroll: true
		});
	}

	async function reload() {
		if (!accountId) return;
		loading = true;
		err = '';
		try {
			const [a, o, tx] = await Promise.all([
				accountApi.get(accountId),
				orgApi.current().catch(() => null),
				bankTransactionApi.list({ accountId, pageSize: '500' }).catch(() => ({
					BankTransactions: []
				}))
			]);
			account = a ?? null;
			org = o ?? null;
			transactions = tx?.BankTransactions ?? [];
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load bank account';
		} finally {
			loading = false;
		}
	}

	onMount(reload);
	$effect(() => {
		if ($session.tenantId && accountId) void reload();
	});

	const currency = $derived(account?.CurrencyCode ?? org?.BaseCurrency ?? 'USD');

	const unreconciled = $derived(transactions.filter((t) => !t.IsReconciled));
	const reconciled = $derived(transactions.filter((t) => t.IsReconciled));

	const statementBalance = $derived(
		transactions.reduce(
			(s, t) => s + Number(t.Total ?? 0) * (t.Type === 'SPEND' ? -1 : 1),
			0
		)
	);
	const xeroBalance = $derived(
		reconciled.reduce(
			(s, t) => s + Number(t.Total ?? 0) * (t.Type === 'SPEND' ? -1 : 1),
			0
		)
	);

	async function markReconciled(tx: BankTransaction) {
		if (!tx.BankTransactionID) return;
		try {
			// Lightweight reconcile: toggle IsReconciled flag via accountApi pipeline.
			// Actual Xero reconcile flow is multi-step; here we flip the flag server-side.
			await bankTransactionApi.create({
				...tx,
				IsReconciled: true
			} as Partial<BankTransaction>);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not update transaction';
		}
	}
</script>

<p class="text-sm mb-2">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
</p>

<header class="card p-5 mb-4 flex flex-wrap items-start justify-between gap-4">
	<div>
		<h1 class="section-title">{account?.Name ?? '—'}</h1>
		<p class="text-sm muted tabular-nums mt-1">
			{account?.BankAccountNumber || account?.Code || '—'}
		</p>
	</div>
	<div class="flex flex-wrap gap-6">
		<div>
			<div class="text-xs uppercase muted tracking-wide">Statement balance</div>
			<div class="text-xl tabular-nums font-semibold">
				{formatCurrency(statementBalance, currency)}
			</div>
		</div>
		<div>
			<div class="text-xs uppercase muted tracking-wide">Balance in {currency}</div>
			<div class="text-xl tabular-nums font-semibold">
				{formatCurrency(xeroBalance, currency)}
			</div>
		</div>
		<div class="flex items-start gap-2">
			<a
				class="btn-secondary-sm"
				href="/app/accounting/bank-accounts/{accountId}/edit"
			>
				Manage account
			</a>
		</div>
	</div>
</header>

<nav class="flex flex-wrap gap-6 border-b border-ink-200 mb-5 text-sm">
	{#each TABS as t (t.id)}
		<button
			type="button"
			class="relative py-2 transition {activeTab === t.id
				? 'text-brand-600 font-semibold'
				: 'text-ink-600 hover:text-ink-900'}"
			onclick={() => setTab(t.id)}
		>
			{t.label}
			{#if t.id === 'reconcile' && unreconciled.length > 0}
				<span class="ml-1 text-xs">({unreconciled.length})</span>
			{/if}
			{#if activeTab === t.id}
				<span class="absolute left-0 right-0 -bottom-[1px] h-0.5 bg-brand-500"></span>
			{/if}
		</button>
	{/each}
</nav>

{#if err}
	<p class="text-sm text-red-700 mb-3" role="alert">{err}</p>
{/if}

{#if loading}
	<div class="card p-8 muted text-center">Loading…</div>
{:else if activeTab === 'reconcile'}
	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100 flex items-center justify-between">
			<h2 class="content-section-title">
				Reconcile <span class="muted font-normal">({unreconciled.length})</span>
			</h2>
			<div class="text-sm muted">Match statement lines to transactions in {account?.Name ?? '—'}.</div>
		</header>
		{#if unreconciled.length === 0}
			<p class="p-8 text-center muted">Nothing to reconcile — you're all caught up.</p>
		{:else}
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-4 py-2 text-left">Date</th>
						<th class="px-4 py-2 text-left">Contact</th>
						<th class="px-4 py-2 text-left">Reference</th>
						<th class="px-4 py-2 text-right">Spent</th>
						<th class="px-4 py-2 text-right">Received</th>
						<th class="px-4 py-2 text-right"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each unreconciled as t (t.BankTransactionID)}
						<tr class="hover:bg-ink-50">
							<td class="px-4 py-2">{formatDate(t.Date)}</td>
							<td class="px-4 py-2">{t.Contact?.Name ?? '—'}</td>
							<td class="px-4 py-2">{t.Reference ?? '—'}</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{#if t.Type === 'SPEND'}
									{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? currency)}
								{/if}
							</td>
							<td class="px-4 py-2 text-right tabular-nums">
								{#if t.Type === 'RECEIVE'}
									{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? currency)}
								{/if}
							</td>
							<td class="px-4 py-2 text-right">
								<button
									type="button"
									class="btn-secondary-sm"
									onclick={() => markReconciled(t)}
								>
									Reconcile
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
{:else if activeTab === 'cash-coding'}
	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100 flex items-center justify-between flex-wrap gap-2">
			<h2 class="content-section-title">Cash coding</h2>
			<div class="flex items-center gap-2">
				<button type="button" class="btn-secondary-sm" disabled>Uncheck all</button>
				<button type="button" class="btn-secondary-sm" disabled>Apply rule</button>
				<button type="button" class="btn-primary" disabled>Save &amp; Reconcile All</button>
			</div>
		</header>
		{#if unreconciled.length === 0}
			<p class="p-8 text-center muted">No statement lines to code.</p>
		{:else}
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-3 py-2 text-left w-8"><input type="checkbox" disabled /></th>
						<th class="px-3 py-2 text-left">Date</th>
						<th class="px-3 py-2 text-left">Payee</th>
						<th class="px-3 py-2 text-left">Reference</th>
						<th class="px-3 py-2 text-left">Description</th>
						<th class="px-3 py-2 text-left">Account</th>
						<th class="px-3 py-2 text-left">Tax Rate</th>
						<th class="px-3 py-2 text-right">Spent</th>
						<th class="px-3 py-2 text-right">Received</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each unreconciled as t (t.BankTransactionID)}
						<tr class="hover:bg-ink-50">
							<td class="px-3 py-2"><input type="checkbox" /></td>
							<td class="px-3 py-2 tabular-nums">{formatDate(t.Date)}</td>
							<td class="px-3 py-2">{t.Contact?.Name ?? '—'}</td>
							<td class="px-3 py-2">{t.Reference ?? ''}</td>
							<td class="px-3 py-2">{t.LineItems?.[0]?.Description ?? ''}</td>
							<td class="px-3 py-2"><input class="input py-1" placeholder="Select" /></td>
							<td class="px-3 py-2"><input class="input py-1" placeholder="Tax Rate" /></td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'SPEND'}{formatCurrency(t.Total ?? 0, currency)}{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'RECEIVE'}{formatCurrency(t.Total ?? 0, currency)}{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
{:else if activeTab === 'statements'}
	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100 flex items-center justify-between">
			<h2 class="content-section-title">Bank statements</h2>
			<button type="button" class="btn-secondary-sm" disabled>Import statement</button>
		</header>
		{#if transactions.length === 0}
			<p class="p-8 text-center muted">No statement lines have been imported yet.</p>
		{:else}
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-3 py-2 text-left">Date</th>
						<th class="px-3 py-2 text-left">Type</th>
						<th class="px-3 py-2 text-left">Payee</th>
						<th class="px-3 py-2 text-left">Reference</th>
						<th class="px-3 py-2 text-right">Spent</th>
						<th class="px-3 py-2 text-right">Received</th>
						<th class="px-3 py-2 text-left">Status</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each transactions as t (t.BankTransactionID)}
						<tr class="hover:bg-ink-50">
							<td class="px-3 py-2 tabular-nums">{formatDate(t.Date)}</td>
							<td class="px-3 py-2">{t.Type === 'SPEND' ? 'Debit' : 'Credit'}</td>
							<td class="px-3 py-2">{t.Contact?.Name ?? '—'}</td>
							<td class="px-3 py-2">{t.Reference ?? ''}</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'SPEND'}{formatCurrency(t.Total ?? 0, currency)}{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'RECEIVE'}{formatCurrency(t.Total ?? 0, currency)}{/if}
							</td>
							<td class="px-3 py-2">
								{#if t.IsReconciled}
									<span class="text-emerald-700">Reconciled</span>
								{:else}
									<span class="text-amber-600">Unreconciled</span>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
{:else if activeTab === 'transactions'}
	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100 flex items-center justify-between">
			<h2 class="content-section-title">Account transactions</h2>
			<a
				class="btn-secondary-sm"
				href={`/app/bank-transactions?accountId=${accountId}`}
			>
				Open full view
			</a>
		</header>
		{#if transactions.length === 0}
			<p class="p-8 text-center muted">No transactions.</p>
		{:else}
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-3 py-2 text-left">Date</th>
						<th class="px-3 py-2 text-left">Description</th>
						<th class="px-3 py-2 text-left">Reference</th>
						<th class="px-3 py-2 text-right">Spent</th>
						<th class="px-3 py-2 text-right">Received</th>
						<th class="px-3 py-2 text-left">Status</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each transactions as t (t.BankTransactionID)}
						<tr class="hover:bg-ink-50">
							<td class="px-3 py-2 tabular-nums">{formatDate(t.Date)}</td>
							<td class="px-3 py-2">{t.Contact?.Name ?? t.Type}</td>
							<td class="px-3 py-2">{t.Reference ?? ''}</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'SPEND'}{formatCurrency(t.Total ?? 0, currency)}{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'RECEIVE'}{formatCurrency(t.Total ?? 0, currency)}{/if}
							</td>
							<td class="px-3 py-2">
								{#if t.IsReconciled}
									<span class="text-emerald-700">Reconciled</span>
								{:else}
									<span class="text-amber-600">Unreconciled</span>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
{:else if activeTab === 'period'}
	<section class="card p-6 space-y-4">
		<header class="flex items-center justify-between">
			<h2 class="content-section-title">Reconcile period</h2>
			<button type="button" class="btn-primary" disabled>Create period</button>
		</header>
		<p class="text-sm text-ink-700 bg-amber-50 border-l-4 border-amber-400 p-3 rounded">
			For the most accurate records it's best to start this process at the beginning of your
			financial year.
		</p>

		<div class="border border-ink-100 rounded-md">
			<header class="px-4 py-2 font-semibold text-sm bg-ink-50 rounded-t-md">All periods</header>
			<p class="p-6 text-center muted text-sm">
				No periods have been reconciled yet. Reconciled periods let you lock ranges so that only
				authorised users can alter them.
			</p>
		</div>
	</section>
{/if}
