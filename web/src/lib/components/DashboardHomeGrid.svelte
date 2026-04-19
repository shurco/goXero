<script lang="ts">
	import BankDashboardCard from '$lib/components/BankDashboardCard.svelte';
	import { dashboardWidgetHeightClass, parseBankWidgetId } from '$lib/dashboard-layout';
	import type { BankTile } from '$lib/dashboard-types';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, Payment } from '$lib/types';

	interface AgingB {
		parts: { label: string; value: number }[];
		max: number;
	}

	interface Props {
		visibleOrderedIds: string[];
		layoutEdit: boolean;
		draggingId: string | null;
		tiles: BankTile[];
		currency: string;
		menuOpen: string | null;
		toggleMenu: (id: string) => void;
		setWidgetShown: (id: string, shown: boolean) => void;
		onWidgetDragStart: (e: DragEvent, id: string) => void;
		onWidgetDragOver: (e: DragEvent) => void;
		onWidgetDrop: (e: DragEvent, targetId: string) => void;
		onWidgetDragEnd: () => void;
		bankTileIndex: (accountId: string) => number;
		agingSales: AgingB;
		agingBills: AgingB;
		awaitingPayCount: number;
		awaitingPay: number;
		overdueBillCount: number;
		overduePay: number;
		draftPayCount: number;
		awaitingRecCount: number;
		awaitingRec: number;
		overdueInvCount: number;
		overdueRec: number;
		draftRecCount: number;
		payments: Payment[];
		totalUnreconciled: number;
		cashMonthly: { label: string; in: number; out: number }[];
		cashMonthMax: number;
		cashInTotal: number;
		cashOutTotal: number;
		cashNetTotal: number;
		ytd: { income: number; bills: number };
		netProxy: number;
		netTrendPct: number | null;
		ytdStart: Date;
		accountsWatch: Account[];
		userEmail: string | null | undefined;
		userMonogram: (email: string | null | undefined) => string;
		num: (v: string | number | undefined) => number;
	}
	let {
		visibleOrderedIds,
		layoutEdit,
		draggingId,
		tiles,
		currency,
		menuOpen,
		toggleMenu,
		setWidgetShown,
		onWidgetDragStart,
		onWidgetDragOver,
		onWidgetDrop,
		onWidgetDragEnd,
		bankTileIndex,
		agingSales,
		agingBills,
		awaitingPayCount,
		awaitingPay,
		overdueBillCount,
		overduePay,
		draftPayCount,
		awaitingRecCount,
		awaitingRec,
		overdueInvCount,
		overdueRec,
		draftRecCount,
		payments,
		totalUnreconciled,
		cashMonthly,
		cashMonthMax,
		cashInTotal,
		cashOutTotal,
		cashNetTotal,
		ytd,
		netProxy,
		netTrendPct,
		ytdStart,
		accountsWatch,
		userEmail,
		userMonogram,
		num
	}: Props = $props();

	const DASHBOARD_XL_COLUMNS = 3;

	function splitIntoColumns(ids: string[], columnCount: number): string[][] {
		const cols: string[][] = Array.from({ length: columnCount }, () => []);
		for (let i = 0; i < ids.length; i++) {
			cols[i % columnCount]!.push(ids[i]!);
		}
		return cols;
	}

	/** Независимые колонки: без «высоты строки» grid — следующий блок идёт сразу под предыдущим в своей колонке. */
	const desktopColumns = $derived(splitIntoColumns(visibleOrderedIds, DASHBOARD_XL_COLUMNS));
</script>

{#snippet widgetSlot(wid: string)}
		{@const bankAid = parseBankWidgetId(wid)}
		{@const slotH = dashboardWidgetHeightClass(wid, bankTileIndex)}
		<div
			class="dashboard-widget-slot card relative flex min-h-0 min-w-0 flex-col overflow-hidden p-5 shadow-sm transition {slotH} {draggingId === wid
				? 'opacity-60'
				: ''} {layoutEdit
				? 'cursor-grab active:cursor-grabbing hover:bg-white/90 hover:shadow-md'
				: ''}"
			role="listitem"
			draggable={layoutEdit}
			ondragstart={(e) => onWidgetDragStart(e, wid)}
			ondragover={onWidgetDragOver}
			ondrop={(e) => onWidgetDrop(e, wid)}
			ondragend={onWidgetDragEnd}
		>
			{#if layoutEdit}
				<button
					type="button"
					class="dashboard-widget-edit-control absolute -left-1 -top-1 z-20 flex h-7 w-7 items-center justify-center rounded-full border border-ink-300 bg-white text-ink-600 shadow-sm hover:bg-ink-50"
					aria-label="Hide widget"
					onclick={() => setWidgetShown(wid, false)}
				>
					<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
						><path d="M5 12h14" stroke-linecap="round" /></svg
					>
				</button>
			{/if}

			<div class="flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-auto">
			{#if bankAid}
				{@const tile = tiles.find((t) => t.account.AccountID === bankAid)}
				{#if tile}
					{@const bi = bankTileIndex(bankAid)}
					<BankDashboardCard
						{tile}
						variant={bi === 0 ? 'first' : bi === 1 ? 'second' : 'rest'}
						{currency}
						{menuOpen}
						{toggleMenu}
					/>
				{/if}
			{:else if wid === 'bills-pay'}
				<div class="flex items-start justify-between gap-2">
					<h3 class="font-semibold text-ink-900">Bills to pay</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'bills'}
							onclick={() => toggleMenu('bills')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'bills'}
							<div
								class="absolute right-0 z-30 mt-1 min-w-[200px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop"
							>
								<a class="nav-dropdown-item" href="/app/purchases/bills">View bills</a>
								<a class="nav-dropdown-item" href="/app/reports/aged-payables">Aged payables report</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="grid grid-cols-2 gap-3 text-sm">
					<div>
						<div class="text-xs text-ink-500">{awaitingPayCount} awaiting payment</div>
						<div class="text-xl font-semibold tabular-nums text-ink-900">
							{formatCurrency(awaitingPay, currency)}
						</div>
					</div>
					<div>
						<div class="text-xs text-ink-500">
							{overdueBillCount} of {awaitingPayCount || 1} overdue
						</div>
						<div class="text-xl font-semibold tabular-nums text-red-600">
							{formatCurrency(overduePay, currency)}
						</div>
					</div>
				</div>
				<div class="flex h-16 items-end gap-1 border-b border-ink-100 pb-2">
					{#each agingBills.parts as b}
						<div class="flex min-w-0 flex-1 flex-col items-center gap-1">
							<div
								class="w-full rounded-t bg-sky-600 transition-all"
								style="height: {Math.max(8, (b.value / agingBills.max) * 100)}%"
								title={b.label}
							></div>
							<span class="w-full truncate text-center text-[10px] text-ink-500">{b.label}</span>
						</div>
					{/each}
				</div>
				<div class="flex flex-wrap gap-3 text-xs">
					<a href="/app/purchases/bills?status=DRAFT" class="text-brand-700 hover:underline">{draftPayCount} drafts</a>
					<span class="text-ink-400">0 awaiting approval</span>
				</div>
				<div class="mt-auto flex flex-wrap gap-2 pt-1">
					<a href="/app/purchases/bills/new" class="btn-secondary btn-secondary-sm inline-flex items-center gap-1">
						Add bills
						<svg class="h-3 w-3 text-ink-400" viewBox="0 0 24 24" fill="currentColor"
							><path d="M7 10l5 5 5-5z" /></svg
						>
					</a>
					<a href="/app/payments?type=ACCPAY" class="btn-secondary btn-secondary-sm inline-flex items-center gap-1">
						Pay bills
						<svg class="h-3 w-3 text-ink-400" viewBox="0 0 24 24" fill="currentColor"
							><path d="M7 10l5 5 5-5z" /></svg
						>
					</a>
				</div>
			{:else if wid === 'net-profit'}
				<div class="flex items-start justify-between">
					<h3 class="font-semibold text-ink-900">Net profit or loss · Year to date</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'net'}
							onclick={() => toggleMenu('net')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'net'}
							<div class="absolute right-0 z-30 mt-1 min-w-[200px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop">
								<a class="nav-dropdown-item" href="/app/reports/profit-and-loss">Profit and Loss report</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="text-3xl font-semibold tabular-nums text-ink-900">
					{formatCurrency(netProxy, currency)}
				</div>
				{#if netTrendPct !== null}
					<p class="flex items-center gap-1 text-sm text-red-600">
						<svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
							><path d="M16 18l2.3-2.3a1 1 0 000-1.4L13 9l-3 3-4-4-6 6v3h16v-3z" /></svg
						>
						<span
							>Down {Math.abs(netTrendPct)}% from {formatDate(
								ytdStart.toISOString().slice(0, 10)
							)} — {formatDate(new Date().toISOString().slice(0, 10))}</span
						>
					</p>
				{:else}
					<p class="text-sm text-ink-500">Based on paid sales and paid bills this calendar year.</p>
				{/if}
				<div class="mt-2 flex h-28 items-end gap-4">
					<div class="flex min-h-0 flex-1 flex-col items-stretch justify-end gap-2">
						<div
							class="w-full rounded-t bg-brand-700"
							style="height: {ytd.income + ytd.bills > 0
								? Math.max(12, (ytd.income / (ytd.income + ytd.bills)) * 100)
								: 40}%"
						></div>
						<div class="text-center text-xs text-ink-600">Income {formatCurrency(ytd.income, currency)}</div>
					</div>
					<div class="flex min-h-0 flex-1 flex-col items-stretch justify-end gap-2">
						<div
							class="w-full rounded-t bg-sky-300"
							style="height: {ytd.income + ytd.bills > 0
								? Math.max(12, (ytd.bills / (ytd.income + ytd.bills)) * 100)
								: 40}%"
						></div>
						<div class="text-center text-xs text-ink-600">Expenses {formatCurrency(ytd.bills, currency)}</div>
					</div>
				</div>
				<a href="/app/reports/profit-and-loss" class="mt-2 text-sm font-medium text-brand-700 hover:underline">
					Go to Income Statement report
				</a>
			{:else if wid === 'tasks'}
				<h3 class="mb-3 font-semibold text-ink-900">Tasks</h3>
				<ul class="space-y-0 text-sm">
					<li class="border-b border-ink-50 last:border-0">
						<a
							href="/app/bank-transactions"
							class="-mx-1 flex items-center justify-between rounded px-1 py-3 hover:bg-ink-50/80"
						>
							<span>{totalUnreconciled} Items to reconcile</span>
							<span class="text-brand-600" aria-hidden="true">›</span>
						</a>
					</li>
					<li class="border-b border-ink-50 last:border-0">
						<a
							href="/app/invoices?overdue=1"
							class="-mx-1 flex items-center justify-between rounded px-1 py-3 hover:bg-ink-50/80"
						>
							<span>{overdueInvCount} Overdue Invoices</span>
							<span class="text-brand-600" aria-hidden="true">›</span>
						</a>
					</li>
					<li>
						<a
							href="/app/purchases/bills"
							class="-mx-1 flex items-center justify-between rounded px-1 py-3 hover:bg-ink-50/80"
						>
							<span>{overdueBillCount} Overdue bills</span>
							<span class="text-brand-600" aria-hidden="true">›</span>
						</a>
					</li>
				</ul>
			{:else if wid === 'recent-payments'}
				<div class="mb-3 flex min-w-0 items-start justify-between gap-2">
					<h3 class="min-w-0 flex-1 pr-2 font-semibold leading-snug text-ink-900">Recent invoice payments</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'pay'}
							onclick={() => toggleMenu('pay')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'pay'}
							<div class="absolute right-0 z-30 mt-1 min-w-[180px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop">
								<a class="nav-dropdown-item" href="/app/invoices?status=PAID">Paid invoices</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="min-w-0 w-full overflow-x-auto">
					<table class="w-full min-w-[320px] text-sm">
						<thead
							class="border-b border-ink-100 text-left text-[11px] font-semibold uppercase tracking-wide text-ink-500"
						>
							<tr>
								<th class="max-w-[28%] pb-2 pr-2">Invoice #</th>
								<th class="max-w-[28%] pb-2 pr-2">Contact</th>
								<th class="whitespace-nowrap pb-2 pr-3">
									<span class="inline-flex items-center gap-0.5">Date received <span class="text-ink-300">↕</span></span>
								</th>
								<th class="whitespace-nowrap pb-2 pl-2 text-right">Amount</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-ink-50">
							{#if payments.length === 0}
								<tr><td colspan="4" class="py-8 text-center text-ink-500">No payments yet.</td></tr>
							{:else}
								{#each payments as p}
									<tr>
										<td class="max-w-0 truncate py-2.5 pr-2 font-medium text-brand-700">—</td>
										<td class="max-w-0 truncate py-2.5 pr-2 text-ink-800">—</td>
										<td class="whitespace-nowrap py-2.5 pr-3 tabular-nums text-ink-700">{formatDate(p.Date)}</td>
										<td class="whitespace-nowrap py-2.5 pl-2 text-right font-medium tabular-nums">{formatCurrency(num(p.Amount), currency)}</td>
									</tr>
								{/each}
							{/if}
						</tbody>
					</table>
				</div>
				<a href="/app/invoices?status=PAID" class="mt-3 inline-block text-sm font-medium text-brand-700 hover:underline">
					View paid invoices
				</a>
			{:else if wid === 'expenses-review'}
				<div class="mb-3 flex items-start justify-between gap-2">
					<h3 class="font-semibold text-ink-900">Expenses to review · Last 365 days</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'exp'}
							onclick={() => toggleMenu('exp')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'exp'}
							<div class="absolute right-0 z-30 mt-1 min-w-[180px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop">
								<a class="nav-dropdown-item" href="/app/bank-transactions">Bank transactions</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="grid grid-cols-2 gap-3 border-b border-ink-100 pb-3 text-sm">
					<div>
						<div class="text-xs text-ink-500">Queued</div>
						<div class="text-lg font-semibold tabular-nums">{formatCurrency(0, currency)}</div>
					</div>
					<div>
						<div class="text-xs text-ink-500">To categorise</div>
						<div class="text-lg font-semibold tabular-nums text-ink-400">—</div>
					</div>
				</div>
				<div class="mt-3 flex items-center justify-between gap-3 rounded-md bg-ink-50/80 px-3 py-2 text-sm">
					<div class="flex items-center gap-2">
						<span
							class="flex h-8 w-8 items-center justify-center rounded-full bg-brand-600 text-xs font-bold text-white"
							>{userMonogram(userEmail)}</span
						>
						<span class="font-medium text-ink-900">{userEmail?.split('@')[0] ?? 'You'}</span>
					</div>
					<div class="text-right">
						<div class="text-xs text-ink-500">{totalUnreconciled} items</div>
						<div class="font-semibold tabular-nums">{formatCurrency(0, currency)}</div>
					</div>
				</div>
				<a href="/app/bank-transactions" class="mt-3 inline-block text-sm font-medium text-brand-700 hover:underline">
					View all expenses
				</a>
			{:else if wid === 'invoices-owed'}
				<div class="flex items-start justify-between gap-2">
					<h3 class="font-semibold text-ink-900">Invoices owed to you</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'inv'}
							onclick={() => toggleMenu('inv')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'inv'}
							<div class="absolute right-0 z-30 mt-1 min-w-[200px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop">
								<a class="nav-dropdown-item" href="/app/invoices">All invoices</a>
								<a class="nav-dropdown-item" href="/app/reports/aged-receivables">Aged receivables</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="grid grid-cols-2 gap-3 text-sm">
					<div>
						<div class="text-xs text-ink-500">{awaitingRecCount} awaiting payment</div>
						<div class="text-xl font-semibold tabular-nums text-ink-900">
							{formatCurrency(awaitingRec, currency)}
						</div>
					</div>
					<div>
						<div class="text-xs text-ink-500">{overdueInvCount} of {awaitingRecCount || 1} overdue</div>
						<div class="text-xl font-semibold tabular-nums text-red-600">
							{formatCurrency(overdueRec, currency)}
						</div>
					</div>
				</div>
				<div class="flex h-16 items-end gap-1 border-b border-ink-100 pb-2">
					{#each agingSales.parts as b}
						<div class="flex min-w-0 flex-1 flex-col items-center gap-1">
							<div
								class="w-full rounded-t bg-brand-600 transition-all"
								style="height: {Math.max(8, (b.value / agingSales.max) * 100)}%"
							></div>
							<span class="w-full truncate text-center text-[10px] text-ink-500">{b.label}</span>
						</div>
					{/each}
				</div>
				<div class="flex flex-wrap gap-3 text-xs">
					<a href="/app/invoices?status=DRAFT" class="text-brand-700 hover:underline">{draftRecCount} drafts</a>
					<span class="text-ink-400">0 awaiting approval</span>
				</div>
				<div class="mt-auto flex flex-wrap gap-2 pt-1">
					<a href="/app/invoices/new" class="btn-primary !rounded-full !px-4 !py-2 !text-xs font-semibold">
						New invoice
					</a>
					<a href="/app/invoices" class="btn-secondary !rounded-full !px-4 !py-2 !text-xs font-semibold">
						View all invoices
					</a>
				</div>
			{:else if wid === 'cash-in-out'}
				<div class="mb-3 flex items-start justify-between gap-2">
					<h3 class="font-semibold text-ink-900">Cash in and out · Last 6 months</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'cash'}
							onclick={() => toggleMenu('cash')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'cash'}
							<div class="absolute right-0 z-30 mt-1 min-w-[200px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop">
								<a class="nav-dropdown-item" href="/app/reports/bank-summary">Bank Summary report</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="mb-4 grid grid-cols-3 gap-2 text-center text-sm">
					<div>
						<div class="text-xs text-ink-500">Cash in</div>
						<div class="font-semibold tabular-nums text-ink-900">{formatCurrency(cashInTotal, currency)}</div>
					</div>
					<div>
						<div class="text-xs text-ink-500">Cash out</div>
						<div class="font-semibold tabular-nums text-ink-900">{formatCurrency(-cashOutTotal, currency)}</div>
					</div>
					<div>
						<div class="text-xs text-ink-500">Difference</div>
						<div class="font-semibold tabular-nums text-ink-900">{formatCurrency(cashNetTotal, currency)}</div>
					</div>
				</div>
				<div class="flex h-40 items-end gap-2 border-b border-ink-200 pb-1">
					{#each cashMonthly as m}
						<div class="flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-1">
							<div class="flex h-32 w-full max-w-[52px] items-end justify-center gap-1">
								<div
									class="w-1/2 rounded-t bg-brand-700"
									style="height: {Math.max(4, (m.in / cashMonthMax) * 100)}%"
								></div>
								<div
									class="w-1/2 rounded-t bg-sky-300"
									style="height: {Math.max(4, (m.out / cashMonthMax) * 100)}%"
								></div>
							</div>
							<span class="text-[10px] text-ink-500">{m.label}</span>
						</div>
					{/each}
				</div>
				<div class="mt-3 flex gap-4 text-xs">
					<span class="inline-flex items-center gap-1.5"
						><span class="h-2 w-2 rounded-sm bg-brand-700"></span> Cash in</span
					>
					<span class="inline-flex items-center gap-1.5"
						><span class="h-2 w-2 rounded-sm bg-sky-300"></span> Cash out</span
					>
				</div>
				{#if layoutEdit}
					<button
						type="button"
						class="absolute bottom-2 right-2 flex items-center gap-1 rounded border border-ink-200 bg-white px-2 py-1 text-[11px] text-ink-500 shadow-sm hover:bg-ink-50"
					>
						<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
							><path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" stroke-linecap="round" /></svg
						>
						Make larger
					</button>
				{/if}
				<a href="/app/reports/bank-summary" class="mt-4 inline-block text-sm font-medium text-brand-700 hover:underline">
					Go to Bank Summary report
				</a>
			{:else if wid === 'coa-watchlist'}
				<div class="mb-3 flex items-center justify-between gap-2">
					<h3 class="font-semibold text-ink-900">Chart of accounts watchlist</h3>
					<div class="relative" data-dash-menu>
						<button
							class="rounded p-1 text-ink-400 hover:bg-ink-100"
							aria-label="More"
							aria-expanded={menuOpen === 'watch'}
							onclick={() => toggleMenu('watch')}
						>
							<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
								><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
							>
						</button>
						{#if menuOpen === 'watch'}
							<div class="absolute right-0 z-30 mt-1 min-w-[200px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop">
								<a class="nav-dropdown-item" href="/app/accounts">Chart of accounts</a>
							</div>
						{/if}
					</div>
				</div>
				<div class="overflow-x-auto">
					<table class="min-w-full text-sm">
						<thead
							class="border-b border-ink-100 text-left text-[11px] font-semibold uppercase tracking-wide text-ink-500"
						>
							<tr>
								<th class="pb-2 pr-3">Code</th>
								<th class="pb-2 pr-3">Account</th>
								<th class="pb-2 text-right">This month</th>
								<th class="pb-2 text-right">YTD</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-ink-50">
							{#if accountsWatch.length === 0}
								<tr><td colspan="4" class="py-8 text-center text-ink-500">No accounts in watchlist yet.</td></tr>
							{:else}
								{#each accountsWatch as a}
									<tr>
										<td class="py-2.5 pr-3 tabular-nums text-ink-700">{a.Code}</td>
										<td class="py-2.5 pr-3 font-medium text-ink-900">{a.Name}</td>
										<td class="py-2.5 text-right text-ink-400">—</td>
										<td class="py-2.5 text-right text-ink-400">—</td>
									</tr>
								{/each}
							{/if}
						</tbody>
					</table>
				</div>
				{#if layoutEdit}
					<button
						type="button"
						class="absolute bottom-2 right-2 flex items-center gap-1 rounded border border-ink-200 bg-white px-2 py-1 text-[11px] text-ink-500 shadow-sm hover:bg-ink-50"
					>
						<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
							><path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" stroke-linecap="round" /></svg
						>
						Make larger
					</button>
				{/if}
				<a href="/app/accounts" class="mt-3 inline-block text-sm font-medium text-brand-700 hover:underline">
					Go to full chart of accounts
				</a>
			{/if}
			</div>
		</div>
{/snippet}

<div class="flex flex-col gap-4 xl:hidden" role="list">
	{#each visibleOrderedIds as wid (wid)}
		{@render widgetSlot(wid)}
	{/each}
</div>

<div class="hidden gap-4 xl:flex xl:flex-row xl:items-start" role="list">
	{#each desktopColumns as col, colIdx (colIdx)}
		<div class="flex min-h-0 min-w-0 flex-1 flex-col gap-4">
			{#each col as wid (wid)}
				{@render widgetSlot(wid)}
			{/each}
		</div>
	{/each}
</div>
