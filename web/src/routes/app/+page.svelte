<script lang="ts">
	import { onMount } from 'svelte';
	import { browser } from '$app/environment';
	import {
		accountApi,
		orgApi,
		bankTransactionApi,
		invoiceApi,
		paymentApi
	} from '$lib/api';
	import { session } from '$lib/stores/session';
	import {
		bankBalancesFromTransactions,
		invoiceAgingBuckets,
		monthlyReceiveSpend,
		num,
		ytdPaidTotals
	} from '$lib/dashboard-utils';
	import {
		bankWidgetId,
		parseBankWidgetId,
		loadLayout,
		saveLayout,
		reconcileOrder
	} from '$lib/dashboard-layout';
	import type { BankTile } from '$lib/dashboard-types';
	import DashboardWidgetsModal from '$lib/components/DashboardWidgetsModal.svelte';
	import DashboardHomeGrid from '$lib/components/DashboardHomeGrid.svelte';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { Account, BankTransaction, Invoice, Organisation, Payment } from '$lib/types';

	let loading = $state(true);
	let org = $state<Organisation | null>(null);
	let tiles = $state<BankTile[]>([]);
	let menuOpen = $state<string | null>(null);

	let invoicesRec = $state<Invoice[]>([]);
	let invoicesPay = $state<Invoice[]>([]);
	let payments = $state<Payment[]>([]);
	let allBankTx = $state<BankTransaction[]>([]);
	let accountsWatch = $state<Account[]>([]);

	let layoutEdit = $state(false);
	let lastRefreshAt = $state(Date.now());
	let showAppsStrip = $state(true);
	let widgetsModalOpen = $state(false);
	let dashboardOrder = $state<string[]>([]);
	let dashboardHidden = $state<Record<string, boolean>>({});
	let draggingId = $state<string | null>(null);

	let totalUnreconciled = $state(0);

	function toggleMenu(id: string) {
		menuOpen = menuOpen === id ? null : id;
	}

	/** До 3 букв из слов названия (как DCU на дашборде Xero). */
	function orgBadgeLetters(name: string | undefined): string {
		if (!name?.trim()) return '?';
		const words = name.match(/[A-Za-z][A-Za-z]*/g) ?? [];
		if (words.length >= 3) {
			return words
				.slice(0, 3)
				.map((w) => w[0]!.toUpperCase())
				.join('');
		}
		if (words.length === 2) {
			const a = words[0]!.toUpperCase();
			const b = words[1]!.toUpperCase();
			return (a[0] + b[0] + (b[1] ?? a[1] ?? '')).slice(0, 3);
		}
		if (words.length === 1) return words[0]!.slice(0, 3).toUpperCase();
		return name.slice(0, 3).toUpperCase();
	}

	function userMonogram(email: string | null | undefined) {
		if (!email) return '—';
		const local = email.split('@')[0] ?? '';
		const bits = local.replace(/[^a-zA-Z]/g, ' ').trim().split(/\s+/).filter(Boolean);
		if (bits.length >= 2) return (bits[0]![0] + bits[bits.length - 1]![0]).toUpperCase();
		return local.slice(0, 2).toUpperCase() || 'U';
	}

	function relativeLoginLabel(at: number) {
		const mins = Math.floor((Date.now() - at) / 60000);
		if (mins < 1) return 'just now';
		if (mins < 60) return `${mins} minute${mins === 1 ? '' : 's'} ago`;
		const hrs = Math.floor(mins / 60);
		if (hrs < 24) return `${hrs} hour${hrs === 1 ? '' : 's'} ago`;
		const days = Math.floor(hrs / 24);
		return `${days} day${days === 1 ? '' : 's'} ago`;
	}

	const tzHint = $derived(
		typeof Intl !== 'undefined'
			? Intl.DateTimeFormat().resolvedOptions().timeZone?.replace(/_/g, ' ') ?? ''
			: ''
	);

	onMount(() => {
		function onDocClick(e: MouseEvent) {
			const t = e.target as HTMLElement;
			if (!t.closest('[data-dash-menu]')) menuOpen = null;
		}
		function onKey(e: KeyboardEvent) {
			if (e.key === 'Escape') menuOpen = null;
		}
		document.addEventListener('mousedown', onDocClick);
		document.addEventListener('keydown', onKey);
		return () => {
			document.removeEventListener('mousedown', onDocClick);
			document.removeEventListener('keydown', onKey);
		};
	});

	onMount(() => {
		if (!browser) return;
		lastRefreshAt = Date.now();
	});

	async function reload() {
		loading = true;
		try {
			const [accounts, organisation, invRec, invPay, payRes, txsRes] = await Promise.all([
				accountApi.list({ status: 'ACTIVE' }).catch(() => [] as Account[]),
				orgApi.current().catch(() => null),
				invoiceApi.list({ pageSize: '200', type: 'ACCREC' }).catch(() => null),
				invoiceApi.list({ pageSize: '200', type: 'ACCPAY' }).catch(() => null),
				paymentApi.list({ pageSize: '8' }).catch(() => null),
				bankTransactionApi.list({ pageSize: '500' }).catch(() => null)
			]);
			org = organisation ?? null;
			invoicesRec = invRec?.Invoices ?? [];
			invoicesPay = invPay?.Invoices ?? [];
			payments = payRes?.Payments ?? [];
			allBankTx = txsRes?.BankTransactions ?? [];
			accountsWatch = (accounts ?? [])
				.filter((a) => a.Type === 'REVENUE' || a.Type === 'EXPENSE' || a.Type === 'OVERHEADS')
				.slice(0, 6);

			const bankAccounts = accounts.filter((a) => a.Type === 'BANK');
			const built: BankTile[] = [];
			let unreconAll = 0;

			for (const acc of bankAccounts) {
				const accTxs = allBankTx.filter((t) => t.BankAccount?.AccountID === acc.AccountID);
				const { statement, xero } = bankBalancesFromTransactions(accTxs, org?.BaseCurrency ?? 'USD');
				const unreconciled = accTxs.filter((t) => !t.IsReconciled).length;
				unreconAll += unreconciled;
				const latest = accTxs[0];
				built.push({
					account: acc,
					statementBalance: statement,
					xeroBalance: xero,
					lastStatementDate: latest?.Date,
					unreconciled
				});
			}
			totalUnreconciled = unreconAll;
			tiles = built;
		} finally {
			loading = false;
			if (browser) lastRefreshAt = Date.now();
		}
	}

	$effect(() => {
		if ($session.tenantId) void reload();
	});

	const currency = $derived(org?.BaseCurrency || 'USD');

	const agingSales = $derived(invoiceAgingBuckets(invoicesRec, 'ACCREC'));
	const agingBills = $derived(invoiceAgingBuckets(invoicesPay, 'ACCPAY'));

	const awaitingRec = $derived(
		invoicesRec.filter((i) => i.Status === 'AUTHORISED').reduce((s, i) => s + num(i.AmountDue), 0)
	);
	const overdueRec = $derived(
		invoicesRec
			.filter((i) => i.Status === 'AUTHORISED' && i.DueDate && new Date(i.DueDate) < new Date())
			.reduce((s, i) => s + num(i.AmountDue), 0)
	);

	const awaitingPay = $derived(
		invoicesPay.filter((i) => i.Status === 'AUTHORISED').reduce((s, i) => s + num(i.AmountDue), 0)
	);
	const overduePay = $derived(
		invoicesPay
			.filter((i) => i.Status === 'AUTHORISED' && i.DueDate && new Date(i.DueDate) < new Date())
			.reduce((s, i) => s + num(i.AmountDue), 0)
	);

	const awaitingRecCount = $derived(invoicesRec.filter((i) => i.Status === 'AUTHORISED').length);
	const draftRecCount = $derived(invoicesRec.filter((i) => i.Status === 'DRAFT').length);
	const awaitingPayCount = $derived(invoicesPay.filter((i) => i.Status === 'AUTHORISED').length);
	const draftPayCount = $derived(invoicesPay.filter((i) => i.Status === 'DRAFT').length);

	const overdueInvCount = $derived(
		invoicesRec.filter(
			(i) => i.Status === 'AUTHORISED' && i.DueDate && new Date(i.DueDate) < new Date()
		).length
	);
	const overdueBillCount = $derived(
		invoicesPay.filter(
			(i) => i.Status === 'AUTHORISED' && i.DueDate && new Date(i.DueDate) < new Date()
		).length
	);

	const cashMonthly = $derived(monthlyReceiveSpend(allBankTx, 6));
	const cashMonthMax = $derived(Math.max(1, ...cashMonthly.flatMap((m) => [m.in, m.out])));
	const cashInTotal = $derived(cashMonthly.reduce((s, m) => s + m.in, 0));
	const cashOutTotal = $derived(cashMonthly.reduce((s, m) => s + m.out, 0));
	const cashNetTotal = $derived(cashInTotal - cashOutTotal);

	const ytd = $derived(ytdPaidTotals([...invoicesRec, ...invoicesPay], new Date().getFullYear()));
	const netProxy = $derived(ytd.income - ytd.bills);

	const ytdStart = $derived(new Date(new Date().getFullYear(), 0, 1));
	const netTrendPct = $derived.by(() => {
		const denom = Math.abs(ytd.income) + Math.abs(ytd.bills);
		if (denom < 1) return null;
		const ratio = netProxy / denom;
		return Math.round(ratio * 100);
	});

	function persistDashboard() {
		if (!browser) return;
		const tid = $session.tenantId;
		if (!tid) return;
		saveLayout(tid, { version: 1, order: dashboardOrder, hidden: dashboardHidden });
	}

	$effect(() => {
		if (!browser) return;
		const tid = $session.tenantId;
		if (!tid) return;
		const stored = loadLayout(tid);
		dashboardHidden = { ...(stored?.hidden ?? {}) };
		dashboardOrder = reconcileOrder(stored?.order, tiles);
	});

	const visibleOrderedIds = $derived.by(() => {
		const bankSet = new Set(tiles.map((t) => bankWidgetId(t.account.AccountID)));
		return dashboardOrder.filter((id) => {
			if (dashboardHidden[id]) return false;
			if (parseBankWidgetId(id)) return bankSet.has(id);
			return true;
		});
	});

	function setWidgetShown(id: string, shown: boolean) {
		if (shown) {
			const next = { ...dashboardHidden };
			delete next[id];
			dashboardHidden = next;
		} else {
			dashboardHidden = { ...dashboardHidden, [id]: true };
		}
		persistDashboard();
	}

	function onWidgetDragStart(e: DragEvent, id: string) {
		if (!layoutEdit) return;
		draggingId = id;
		e.dataTransfer?.setData('text/plain', id);
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.dropEffect = 'move';
		}
	}

	function onWidgetDragOver(e: DragEvent) {
		if (!layoutEdit) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
	}

	function onWidgetDrop(e: DragEvent, targetId: string) {
		e.preventDefault();
		const from = e.dataTransfer?.getData('text/plain') || draggingId;
		draggingId = null;
		if (!from || from === targetId || !layoutEdit) return;
		const i = dashboardOrder.indexOf(from);
		const j = dashboardOrder.indexOf(targetId);
		if (i < 0 || j < 0) return;
		const next = [...dashboardOrder];
		next.splice(i, 1);
		next.splice(j, 0, from);
		dashboardOrder = next;
		persistDashboard();
	}

	function onWidgetDragEnd() {
		draggingId = null;
	}

	function bankTileIndex(accountId: string): number {
		return tiles.findIndex((t) => t.account.AccountID === accountId);
	}
</script>

<div class="dashboard-home space-y-4 pb-10" class:dashboard-home--edit={layoutEdit}>
	<!-- Sub-header: org + last login (Xero-style white strip) -->
	<div
		class="flex flex-col gap-3 border-b border-ink-100 bg-white px-4 py-4 sm:flex-row sm:items-center sm:justify-between"
	>
		<div class="flex items-center gap-3">
			<div
				class="flex h-11 w-11 shrink-0 items-center justify-center rounded border border-rose-200 bg-rose-100 text-xs font-bold tracking-tight text-ink-900 shadow-sm"
				aria-hidden="true"
			>
				{orgBadgeLetters(org?.Name)}
			</div>
			<h1 class="text-base font-bold leading-snug text-ink-900">{org?.Name ?? 'Your organisation'}</h1>
		</div>
		<div class="text-sm sm:ml-auto sm:text-right">
			<span class="text-ink-500">Last login: </span><span class="font-normal text-brand-600"
				>{relativeLoginLabel(lastRefreshAt)}</span
			>{#if tzHint}<span class="whitespace-nowrap font-normal text-brand-600"> from {tzHint}</span>{/if}
		</div>
	</div>

	<!-- Business Overview: заголовок слева, действия справа -->
	<div class="flex w-full min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
		<h2 class="section-title min-w-0">Business Overview</h2>
		<div class="flex shrink-0 flex-wrap items-center justify-start gap-2 sm:justify-end sm:gap-3">
			{#if layoutEdit}
				<button
					type="button"
					class="btn-secondary border-brand-200 text-brand-700"
					onclick={() => (widgetsModalOpen = true)}
				>
					<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
						><path d="M12 5v14M5 12h14" stroke-linecap="round" /></svg
					>
					Add widget
				</button>
				<button
					type="button"
					class="btn-primary inline-flex items-center gap-2"
					onclick={() => (layoutEdit = false)}
				>
					<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"
						><path d="M5 13l4 4L19 7" stroke-linecap="round" stroke-linejoin="round" /></svg
					>
					Done
				</button>
			{:else}
				<button
					type="button"
					class="btn-primary inline-flex items-center gap-2"
					onclick={() => (layoutEdit = true)}
				>
					<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
						><path
							d="M12 20h9M16.5 3.5a2.12 2.12 0 013 3L7 19l-4 1 1-4L16.5 3.5z"
							stroke-linecap="round"
							stroke-linejoin="round"
						/></svg
					>
					Edit homepage
				</button>
			{/if}
		</div>
	</div>

	{#if loading && tiles.length === 0}
		<div class="grid grid-cols-1 gap-4 xl:grid-cols-3 xl:items-start">
			{#each Array(6) as _}
				<div class="card h-[251px] w-full animate-pulse bg-ink-100/80"></div>
			{/each}
		</div>
	{:else}
		<DashboardHomeGrid
			visibleOrderedIds={visibleOrderedIds}
			layoutEdit={layoutEdit}
			draggingId={draggingId}
			tiles={tiles}
			currency={currency}
			menuOpen={menuOpen}
			toggleMenu={toggleMenu}
			setWidgetShown={setWidgetShown}
			onWidgetDragStart={onWidgetDragStart}
			onWidgetDragOver={onWidgetDragOver}
			onWidgetDrop={onWidgetDrop}
			onWidgetDragEnd={onWidgetDragEnd}
			bankTileIndex={bankTileIndex}
			agingSales={agingSales}
			agingBills={agingBills}
			awaitingPayCount={awaitingPayCount}
			awaitingPay={awaitingPay}
			overdueBillCount={overdueBillCount}
			overduePay={overduePay}
			draftPayCount={draftPayCount}
			awaitingRecCount={awaitingRecCount}
			awaitingRec={awaitingRec}
			overdueInvCount={overdueInvCount}
			overdueRec={overdueRec}
			draftRecCount={draftRecCount}
			payments={payments}
			totalUnreconciled={totalUnreconciled}
			cashMonthly={cashMonthly}
			cashMonthMax={cashMonthMax}
			cashInTotal={cashInTotal}
			cashOutTotal={cashOutTotal}
			cashNetTotal={cashNetTotal}
			ytd={ytd}
			netProxy={netProxy}
			netTrendPct={netTrendPct}
			ytdStart={ytdStart}
			accountsWatch={accountsWatch}
			userEmail={$session.email}
			userMonogram={userMonogram}
			num={num}
		/>
		<DashboardWidgetsModal
			open={widgetsModalOpen}
			tiles={tiles}
			hidden={dashboardHidden}
			onClose={() => (widgetsModalOpen = false)}
			onToggle={(id, vis) => setWidgetShown(id, vis)}
		/>
	{/if}

	<!-- Apps strip -->
	{#if showAppsStrip}
		<section
			class="card flex flex-col gap-4 p-5 shadow-sm sm:flex-row sm:items-center sm:justify-between"
		>
			<div>
				<h3 class="font-semibold text-ink-900">Apps that connect with goXero</h3>
				<p class="mt-1 text-sm text-ink-600">Extend payroll, inventory, CRM and more.</p>
			</div>
			<div class="flex flex-wrap items-center gap-3">
				<a href="/app/settings/connected-apps" class="btn-secondary">Explore more apps</a>
				<button
					type="button"
					class="text-sm font-medium text-ink-600 hover:underline"
					onclick={() => (showAppsStrip = false)}
				>
					Hide
				</button>
			</div>
		</section>
	{:else}
		<div class="flex justify-end">
			<button
				type="button"
				class="text-sm font-medium text-brand-700 hover:underline"
				onclick={() => (showAppsStrip = true)}
			>
				Show apps
			</button>
		</div>
	{/if}
</div>
