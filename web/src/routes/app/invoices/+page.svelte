<script lang="ts">
	import { invoiceApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { page as pageStore } from '$app/stores';
	import { goto } from '$app/navigation';
	import { formatCurrency, formatDate, statusClass, statusLabel } from '$lib/utils/format';
	import type { Invoice, InvoiceStatus, InvoiceSummary, Pagination } from '$lib/types';
	import { onMount } from 'svelte';

	type StatusTab = 'ALL' | 'DRAFT' | 'SUBMITTED' | 'AUTHORISED' | 'PAID' | 'REPEATING';
	type SortKey = 'Number' | 'Date' | 'DueDate' | 'Total' | 'Due' | 'Contact';

	// ── State ──────────────────────────────────────────────────────────────
	let loading = $state(true);
	let invoices = $state<Invoice[]>([]);
	let summary = $state<InvoiceSummary | null>(null);
	let pagination = $state<Pagination>({ page: 1, pageSize: 25, total: 0 });
	let selected = $state(new Set<string>());
	let search = $state('');
	let committedSearch = $state('');
	let sortBy = $state<SortKey>('Date');
	let sortDir = $state<'asc' | 'desc'>('desc');
	let showNewMenu = $state(false);

	// ── URL → state sync ──────────────────────────────────────────────────
	let urlType = $state<'ACCREC' | 'ACCPAY'>('ACCREC');
	let urlTab = $state<StatusTab>('ALL');

	function readUrl() {
		const sp = $pageStore.url.searchParams;
		urlType = sp.get('type') === 'ACCPAY' ? 'ACCPAY' : 'ACCREC';
		const raw = (sp.get('status') || '').toUpperCase();
		urlTab = (['DRAFT', 'SUBMITTED', 'AUTHORISED', 'PAID', 'REPEATING'] as StatusTab[]).includes(
			raw as StatusTab
		)
			? (raw as StatusTab)
			: 'ALL';
		const p = Number(sp.get('page') || '1');
		if (Number.isFinite(p) && p > 0) pagination.page = p;
		const ps = Number(sp.get('pageSize') || '25');
		if ([10, 25, 50, 100, 200].includes(ps)) pagination.pageSize = ps;
		committedSearch = sp.get('search') || '';
		search = committedSearch;
	}

	async function reload() {
		loading = true;
		try {
			const params: Record<string, string> = {
				page: String(pagination.page),
				pageSize: String(pagination.pageSize),
				type: urlType
			};
			if (urlTab !== 'ALL' && urlTab !== 'REPEATING') params.status = urlTab;
			if (committedSearch) params.search = committedSearch;

			const [list, sum] = await Promise.all([
				urlTab === 'REPEATING'
					? Promise.resolve({
							Invoices: [] as Invoice[],
							Pagination: { page: 1, pageSize: pagination.pageSize, total: 0 }
						})
					: invoiceApi.list(params),
				invoiceApi.summary().catch(() => null)
			]);
			invoices = list?.Invoices ?? [];
			pagination = list?.Pagination ?? pagination;
			summary = sum ?? null;
			selected = new Set();
		} catch {
			invoices = [];
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		readUrl();
		reload();
	});

	// React to URL changes (tab clicks, pagination, sales-overview links).
	let lastSearch = '';
	$effect(() => {
		const cur = $pageStore.url.search;
		if (cur === lastSearch) return;
		lastSearch = cur;
		if ($session.tenantId) {
			readUrl();
			void reload();
		}
	});

	function pushParams(updates: Record<string, string | null>) {
		const sp = new URLSearchParams($pageStore.url.searchParams);
		for (const [k, v] of Object.entries(updates)) {
			const shouldDelete =
				v === null ||
				v === '' ||
				(k === 'page' && v === '1') ||
				(k === 'type' && v === 'ACCREC') ||
				(k === 'pageSize' && v === '25') ||
				(k === 'status' && v === 'ALL');
			if (shouldDelete) sp.delete(k);
			else sp.set(k, v);
		}
		const qs = sp.toString();
		void goto(qs ? `/app/invoices?${qs}` : '/app/invoices', {
			replaceState: true,
			noScroll: true,
			keepFocus: true
		});
	}

	function setTab(t: StatusTab) {
		pushParams({ status: t === 'ALL' ? null : t, page: '1' });
	}

	function setType(t: 'ACCREC' | 'ACCPAY') {
		pushParams({ type: t, page: '1' });
	}

	function applySearch() {
		pushParams({ search: search || null, page: '1' });
	}

	function clearSearch() {
		search = '';
		pushParams({ search: null, page: '1' });
	}

	function setPage(p: number) {
		pushParams({ page: String(Math.max(1, p)) });
	}

	function setPageSize(ps: number) {
		pushParams({ pageSize: String(ps), page: '1' });
	}

	function toggleSort(key: SortKey) {
		if (sortBy === key) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
		else {
			sortBy = key;
			sortDir = key === 'Date' || key === 'DueDate' ? 'desc' : 'asc';
		}
	}

	// Client-side sort (server only paginates).
	const sortedInvoices = $derived.by(() => {
		const factor = sortDir === 'asc' ? 1 : -1;
		const src = invoices.slice();
		src.sort((a, b) => {
			const key = sortBy;
			let av: number | string = '';
			let bv: number | string = '';
			switch (key) {
				case 'Number':
					av = a.InvoiceNumber || '';
					bv = b.InvoiceNumber || '';
					break;
				case 'Contact':
					av = a.Contact?.Name || '';
					bv = b.Contact?.Name || '';
					break;
				case 'Date':
					av = a.Date || '';
					bv = b.Date || '';
					break;
				case 'DueDate':
					av = a.DueDate || '';
					bv = b.DueDate || '';
					break;
				case 'Total':
					av = Number(a.Total || 0);
					bv = Number(b.Total || 0);
					break;
				case 'Due':
					av = Number(a.AmountDue || 0);
					bv = Number(b.AmountDue || 0);
					break;
			}
			if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * factor;
			return String(av).localeCompare(String(bv)) * factor;
		});
		return src;
	});

	// ── Selection ──────────────────────────────────────────────────────────
	const allSelected = $derived(
		invoices.length > 0 && invoices.every((i) => selected.has(i.InvoiceID))
	);

	function toggleRow(id: string) {
		if (selected.has(id)) selected.delete(id);
		else selected.add(id);
		selected = new Set(selected);
	}

	function toggleAll() {
		if (allSelected) selected = new Set();
		else {
			const s = new Set(selected);
			invoices.forEach((i) => s.add(i.InvoiceID));
			selected = s;
		}
	}

	function clearSelection() {
		selected = new Set();
	}

	// ── Bulk actions ───────────────────────────────────────────────────────
	let bulkBusy = $state(false);

	async function bulkUpdateStatus(target: InvoiceStatus) {
		const ids = [...selected];
		if (ids.length === 0) return;
		bulkBusy = true;
		try {
			await Promise.all(ids.map((id) => invoiceApi.updateStatus(id, target).catch(() => null)));
			clearSelection();
			await reload();
		} finally {
			bulkBusy = false;
		}
	}

	async function bulkDelete() {
		const ids = [...selected];
		if (ids.length === 0) return;
		if (!confirm(`Void ${ids.length} invoice(s)? GL entries will be reversed.`)) return;
		bulkBusy = true;
		try {
			await Promise.all(ids.map((id) => invoiceApi.delete(id).catch(() => null)));
			clearSelection();
			await reload();
		} finally {
			bulkBusy = false;
		}
	}

	async function bulkEmail() {
		const ids = [...selected];
		if (ids.length === 0) return;
		bulkBusy = true;
		try {
			await Promise.all(ids.map((id) => invoiceApi.email(id).catch(() => null)));
			clearSelection();
			alert(`Queued ${ids.length} invoice(s) for email.`);
		} finally {
			bulkBusy = false;
		}
	}

	function bulkPrint() {
		window.print();
	}

	// ── Tab metadata ───────────────────────────────────────────────────────
	const counts = $derived({
		all: summary?.totalInvoices ?? 0,
		draft: summary?.draft ?? 0,
		submitted: null as number | null,
		authorised: summary?.authorised ?? 0,
		paid: summary?.paid ?? 0,
		repeating: null as number | null
	});

	const totalPages = $derived(Math.max(1, Math.ceil(pagination.total / pagination.pageSize)));
	const pageFrom = $derived(
		pagination.total === 0 ? 0 : (pagination.page - 1) * pagination.pageSize + 1
	);
	const pageTo = $derived(Math.min(pagination.total, pagination.page * pagination.pageSize));

	const titleForType = $derived(urlType === 'ACCPAY' ? 'Bills to pay' : 'Sales invoices');
	const newLabel = $derived(urlType === 'ACCPAY' ? 'New bill' : 'New invoice');
	const newHref = $derived(`/app/invoices/new${urlType === 'ACCPAY' ? '?type=ACCPAY' : ''}`);

	const statusTabs: { id: StatusTab; label: string; count: number | null }[] = $derived([
		{ id: 'ALL', label: 'All', count: counts.all },
		{ id: 'DRAFT', label: 'Draft', count: counts.draft },
		{ id: 'SUBMITTED', label: 'Awaiting Approval', count: counts.submitted },
		{ id: 'AUTHORISED', label: 'Awaiting Payment', count: counts.authorised },
		{ id: 'PAID', label: 'Paid', count: counts.paid },
		{ id: 'REPEATING', label: 'Repeating', count: counts.repeating }
	]);

	function closeMenuOn(e: KeyboardEvent) {
		if (e.key === 'Escape') showNewMenu = false;
	}
</script>

<svelte:window on:keydown={closeMenuOn} />

<div class="space-y-5">
	<!-- Page title + top-right actions -->
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<h1 class="section-title">{titleForType}</h1>
			<p class="muted mt-1 text-sm">
				{urlType === 'ACCPAY'
					? 'Bills you owe suppliers — approve, pay and reconcile.'
					: 'Invoices you send to customers — create, send and track.'}
			</p>
		</div>
		<div class="flex items-center gap-2">
			<button class="btn-secondary" type="button" disabled title="Import is coming soon">Import</button>
			<button class="btn-secondary" type="button" disabled title="Export is coming soon">Export</button>

			<div class="relative">
				<div class="flex">
					<a href={newHref} class="btn-primary rounded-r-none">+ {newLabel}</a>
					<button
						type="button"
						class="btn-primary rounded-l-none border-l border-white/20 px-2"
						aria-label="More create options"
						aria-haspopup="menu"
						aria-expanded={showNewMenu}
						onclick={() => (showNewMenu = !showNewMenu)}
					>
						<svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
							<path d="M5.5 7.5 10 12l4.5-4.5z" />
						</svg>
					</button>
				</div>
				{#if showNewMenu}
					<div
						class="nav-dropdown right-0 left-auto"
						role="menu"
						tabindex="-1"
						onmouseleave={() => (showNewMenu = false)}
					>
						<a class="nav-dropdown-item" href={newHref} role="menuitem">
							<span>{newLabel}</span>
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/invoices/new?repeating=1{urlType === 'ACCPAY' ? '&type=ACCPAY' : ''}"
							role="menuitem"
						>
							<span>Repeating invoice</span>
						</a>
						<a class="nav-dropdown-item" href="/app/invoices?status=DRAFT" role="menuitem">
							<span>View drafts</span>
						</a>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Type toggle (Sales invoices / Bills to pay) -->
	<div class="flex gap-6 border-b border-ink-100 text-sm">
		{#each [{ v: 'ACCREC', l: 'Sales invoices' }, { v: 'ACCPAY', l: 'Bills to pay' }] as t (t.v)}
			<button
				type="button"
				class="pb-2 -mb-px border-b-2 {urlType === t.v
					? 'border-brand-500 text-brand-700 font-semibold'
					: 'border-transparent text-ink-600 hover:text-ink-900'}"
				onclick={() => setType(t.v as 'ACCREC' | 'ACCPAY')}
			>
				{t.l}
			</button>
		{/each}
	</div>

	<!-- Status tab row with counts -->
	<div class="status-tabs">
		{#each statusTabs as t (t.id)}
			<button
				type="button"
				class="status-tab {urlTab === t.id ? 'status-tab-active' : ''}"
				onclick={() => setTab(t.id)}
			>
				<span>{t.label}</span>
				{#if t.count !== null}
					<span class="status-tab-count">{t.count}</span>
				{/if}
			</button>
		{/each}
	</div>

	<!-- Search row + active filter chips -->
	<div class="card p-3 flex flex-wrap items-center gap-3">
		<div class="relative flex-1 min-w-[240px]">
			<svg
				class="absolute left-3 top-1/2 -translate-y-1/2 text-ink-400"
				width="16"
				height="16"
				viewBox="0 0 20 20"
				fill="currentColor"
				aria-hidden="true"
			>
				<path
					fill-rule="evenodd"
					d="M9 3a6 6 0 104.47 10.03l3.25 3.25a1 1 0 001.41-1.41l-3.25-3.25A6 6 0 009 3zm-4 6a4 4 0 118 0 4 4 0 01-8 0z"
					clip-rule="evenodd"
				/>
			</svg>
			<input
				class="input pl-9"
				placeholder="Search by invoice number, reference or contact…"
				bind:value={search}
				onkeydown={(e) => {
					if (e.key === 'Enter') applySearch();
				}}
			/>
		</div>
		<button class="btn-secondary" type="button" onclick={applySearch}>Search</button>

		{#if committedSearch}
			<span class="chip">
				“{committedSearch}”
				<button
					type="button"
					class="chip-remove"
					aria-label="Clear search"
					onclick={clearSearch}
				>
					<svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor" aria-hidden="true">
						<path d="M1 1l8 8M9 1l-8 8" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
					</svg>
				</button>
			</span>
		{/if}

		<div class="ml-auto muted text-sm whitespace-nowrap">
			{pagination.total === 0 ? 'No items' : `${pageFrom}–${pageTo} of ${pagination.total}`}
		</div>
	</div>

	<!-- Table -->
	<div class="card overflow-hidden">
		{#if selected.size > 0}
			<div class="action-bar">
				<span class="font-medium">{selected.size} selected</span>
				<span class="text-ink-300">|</span>
				<button
					class="action-bar-btn"
					type="button"
					disabled={bulkBusy}
					onclick={() => bulkUpdateStatus('AUTHORISED')}
				>
					Approve
				</button>
				<button class="action-bar-btn" type="button" disabled={bulkBusy} onclick={bulkEmail}>
					Email
				</button>
				<button class="action-bar-btn" type="button" onclick={bulkPrint}>Print</button>
				<button
					class="action-bar-btn-danger"
					type="button"
					disabled={bulkBusy}
					onclick={bulkDelete}
				>
					Delete
				</button>
				<button class="ml-auto action-bar-btn" type="button" onclick={clearSelection}>
					Clear selection
				</button>
			</div>
		{/if}

		<div class="overflow-x-auto">
			<table class="table-auto-xero">
				<thead>
					<tr>
						<th class="w-10 pl-4">
							<input
								type="checkbox"
								class="rounded border-ink-300 accent-brand-500"
								aria-label="Select all rows on this page"
								checked={allSelected}
								onchange={toggleAll}
								disabled={invoices.length === 0}
							/>
						</th>
						<th
							class="th-sort {sortBy === 'Number'
								? sortDir === 'asc'
									? 'th-sort-asc'
									: 'th-sort-desc'
								: ''}"
							onclick={() => toggleSort('Number')}
						>
							Number
						</th>
						<th>Ref</th>
						<th
							class="th-sort {sortBy === 'Contact'
								? sortDir === 'asc'
									? 'th-sort-asc'
									: 'th-sort-desc'
								: ''}"
							onclick={() => toggleSort('Contact')}
						>
							To
						</th>
						<th
							class="th-sort {sortBy === 'Date'
								? sortDir === 'asc'
									? 'th-sort-asc'
									: 'th-sort-desc'
								: ''}"
							onclick={() => toggleSort('Date')}
						>
							Date
						</th>
						<th
							class="th-sort {sortBy === 'DueDate'
								? sortDir === 'asc'
									? 'th-sort-asc'
									: 'th-sort-desc'
								: ''}"
							onclick={() => toggleSort('DueDate')}
						>
							Due Date
						</th>
						<th class="text-right">Paid</th>
						<th
							class="th-sort text-right {sortBy === 'Due'
								? sortDir === 'asc'
									? 'th-sort-asc'
									: 'th-sort-desc'
								: ''}"
							onclick={() => toggleSort('Due')}
						>
							Due
						</th>
						<th
							class="th-sort text-right {sortBy === 'Total'
								? sortDir === 'asc'
									? 'th-sort-asc'
									: 'th-sort-desc'
								: ''}"
							onclick={() => toggleSort('Total')}
						>
							Amount
						</th>
						<th>Status</th>
					</tr>
				</thead>
				<tbody>
					{#if loading}
						<tr>
							<td colspan="10" class="text-center py-12 muted">Loading…</td>
						</tr>
					{:else if urlTab === 'REPEATING'}
						<tr>
							<td colspan="10" class="text-center py-16">
								<div class="mx-auto max-w-md">
									<p class="text-ink-900 font-medium">Repeating invoices</p>
									<p class="muted text-sm mt-1">
										Templates that automatically create invoices on a schedule will appear here.
										Set one up from the <a
											class="text-brand-600 hover:underline"
											href="/app/invoices/new?repeating=1">New repeating invoice</a
										> form.
									</p>
								</div>
							</td>
						</tr>
					{:else if sortedInvoices.length === 0}
						<tr>
							<td colspan="10" class="text-center py-12 muted">
								No invoices match your filters.
							</td>
						</tr>
					{:else}
						{#each sortedInvoices as inv (inv.InvoiceID)}
							{@const isSelected = selected.has(inv.InvoiceID)}
							<tr class={isSelected ? 'bg-brand-50/60' : ''}>
								<td class="pl-4">
									<input
										type="checkbox"
										class="rounded border-ink-300 accent-brand-500"
										aria-label={`Select invoice ${inv.InvoiceNumber ?? ''}`}
										checked={isSelected}
										onchange={() => toggleRow(inv.InvoiceID)}
									/>
								</td>
								<td class="font-medium text-ink-900">
									<a class="text-brand-600 hover:underline" href="/app/invoices/{inv.InvoiceID}">
										{inv.InvoiceNumber || '—'}
									</a>
								</td>
								<td class="muted">{inv.Reference || '—'}</td>
								<td>{inv.Contact?.Name ?? '—'}</td>
								<td>{formatDate(inv.Date)}</td>
								<td>{formatDate(inv.DueDate)}</td>
								<td class="text-right tabular-nums">
									{formatCurrency(inv.AmountPaid, inv.CurrencyCode)}
								</td>
								<td class="text-right tabular-nums">
									{formatCurrency(inv.AmountDue, inv.CurrencyCode)}
								</td>
								<td class="text-right tabular-nums font-medium">
									{formatCurrency(inv.Total, inv.CurrencyCode)}
								</td>
								<td>
									<span class={statusClass(inv.Status)}>{statusLabel(inv.Status)}</span>
								</td>
							</tr>
						{/each}
					{/if}
				</tbody>
			</table>
		</div>
	</div>

	<!-- Pagination + page-size -->
	<div class="flex flex-wrap items-center justify-between gap-3 text-sm">
		<div class="flex items-center gap-2">
			<label class="muted" for="page-size">Items per page</label>
			<select
				id="page-size"
				class="select w-auto py-1.5"
				value={String(pagination.pageSize)}
				onchange={(e) => setPageSize(Number((e.target as HTMLSelectElement).value))}
			>
				{#each [10, 25, 50, 100, 200] as n (n)}
					<option value={String(n)}>{n}</option>
				{/each}
			</select>
			<span class="muted ml-2">
				{pagination.total === 0
					? 'Showing 0 of 0'
					: `Showing ${pageFrom}–${pageTo} of ${pagination.total}`}
			</span>
		</div>

		<div class="flex gap-2">
			<button
				class="btn-secondary"
				type="button"
				disabled={pagination.page <= 1}
				onclick={() => setPage(pagination.page - 1)}
			>
				‹ Prev
			</button>
			<span class="inline-flex items-center px-3 py-1 rounded-md bg-ink-100 text-ink-700">
				{pagination.page} / {totalPages}
			</span>
			<button
				class="btn-secondary"
				type="button"
				disabled={pagination.page >= totalPages}
				onclick={() => setPage(pagination.page + 1)}
			>
				Next ›
			</button>
		</div>
	</div>
</div>
