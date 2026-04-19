<script lang="ts">
	import { quoteApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { page as pageStore } from '$app/stores';
	import { goto } from '$app/navigation';
	import { formatCurrency, formatDate, statusClass, statusLabel } from '$lib/utils/format';
	import type { Pagination, Quote, QuoteStatus } from '$lib/types';
	import { onMount } from 'svelte';

	type Tab = 'ALL' | QuoteStatus;

	let loading = $state(true);
	let quotes = $state<Quote[]>([]);
	let pagination = $state<Pagination>({ page: 1, pageSize: 25, total: 0 });
	let search = $state('');
	let committedSearch = $state('');

	// Computed counts for tabs — we run a lightweight second call without filter
	// so the status pills show totals the same way Xero does.
	let allQuotes = $state<Quote[]>([]);

	let tab: Tab = $state('ALL');

	function readUrl() {
		const sp = $pageStore.url.searchParams;
		const raw = (sp.get('status') || '').toUpperCase();
		const allowed: Tab[] = ['DRAFT', 'SENT', 'DECLINED', 'ACCEPTED', 'INVOICED'];
		tab = (allowed as string[]).includes(raw) ? (raw as Tab) : 'ALL';
		const p = Number(sp.get('page') || '1');
		if (Number.isFinite(p) && p > 0) pagination.page = p;
		committedSearch = sp.get('search') || '';
		search = committedSearch;
	}

	async function reload() {
		loading = true;
		try {
			const params: Record<string, string> = {
				page: String(pagination.page),
				pageSize: String(pagination.pageSize)
			};
			if (tab !== 'ALL') params.status = tab;
			if (committedSearch) params.search = committedSearch;

			const [list, full] = await Promise.all([
				quoteApi.list(params).catch(() => null),
				quoteApi.list({ pageSize: '200' }).catch(() => null)
			]);
			quotes = list?.Quotes ?? [];
			pagination = list?.Pagination ?? pagination;
			allQuotes = full?.Quotes ?? [];
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		readUrl();
		reload();
	});

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
			if (v === null || v === '' || (k === 'page' && v === '1') || (k === 'status' && v === 'ALL'))
				sp.delete(k);
			else sp.set(k, v);
		}
		const qs = sp.toString();
		void goto(qs ? `/app/sales/quotes?${qs}` : '/app/sales/quotes', {
			replaceState: true,
			noScroll: true,
			keepFocus: true
		});
	}

	function setTab(t: Tab) {
		pushParams({ status: t === 'ALL' ? null : t, page: '1' });
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

	// Tabs counts (computed from full fetch).
	function countBy(status: QuoteStatus): number {
		return allQuotes.filter((q) => q.Status === status).length;
	}

	const tabs: { id: Tab; label: string; count: number }[] = $derived([
		{ id: 'ALL', label: 'All', count: allQuotes.length },
		{ id: 'DRAFT', label: 'Draft', count: countBy('DRAFT') },
		{ id: 'SENT', label: 'Sent', count: countBy('SENT') },
		{ id: 'DECLINED', label: 'Declined', count: countBy('DECLINED') },
		{ id: 'ACCEPTED', label: 'Accepted', count: countBy('ACCEPTED') },
		{ id: 'INVOICED', label: 'Invoiced', count: countBy('INVOICED') }
	]);

	const totalPages = $derived(Math.max(1, Math.ceil(pagination.total / pagination.pageSize)));
</script>

<div class="space-y-5">
	<!-- Breadcrumb-lite header like the screenshot -->
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<div class="text-sm">
				<a href="/app/sales" class="text-brand-600 hover:underline">Sales overview</a>
				<span class="muted">›</span>
			</div>
			<h1 class="section-title mt-1">Quotes</h1>
		</div>
		<div class="flex items-center gap-2">
			<a href="/app/sales/quotes/new" class="btn-primary">+ New quote</a>
			<button
				class="icon-btn border border-ink-200 bg-white"
				type="button"
				aria-label="More actions"
				disabled
				title="More actions"
			>
				<svg width="16" height="16" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
					<circle cx="4" cy="10" r="1.5" />
					<circle cx="10" cy="10" r="1.5" />
					<circle cx="16" cy="10" r="1.5" />
				</svg>
			</button>
		</div>
	</div>

	<!-- Status tabs with counts -->
	<div class="status-tabs">
		{#each tabs as t (t.id)}
			<button
				type="button"
				class="status-tab {tab === t.id ? 'status-tab-active' : ''}"
				onclick={() => setTab(t.id)}
			>
				<span>{t.label}</span>
				<span class="status-tab-count">{t.count}</span>
			</button>
		{/each}
	</div>

	<!-- Search + Filter button row -->
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
				placeholder="Enter a contact, amount, reference or quote number"
				bind:value={search}
				onkeydown={(e) => {
					if (e.key === 'Enter') applySearch();
				}}
			/>
		</div>
		<button class="btn-secondary" type="button" onclick={applySearch} disabled title="Advanced filters are coming soon">
			<svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
				<path d="M3 5h14l-5 7v4l-4 2v-6z" />
			</svg>
			Filter
		</button>

		{#if committedSearch}
			<span class="chip">
				“{committedSearch}”
				<button type="button" class="chip-remove" aria-label="Clear search" onclick={clearSearch}>
					<svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor" aria-hidden="true">
						<path
							d="M1 1l8 8M9 1l-8 8"
							stroke="currentColor"
							stroke-width="2"
							stroke-linecap="round"
						/>
					</svg>
				</button>
			</span>
		{/if}

		<div class="ml-auto muted text-sm whitespace-nowrap">{pagination.total} items</div>
	</div>

	<!-- Table -->
	<div class="card overflow-x-auto">
		<table class="table-auto-xero">
			<thead>
				<tr>
					<th>Number</th>
					<th>Reference</th>
					<th>Customer</th>
					<th>Issue date</th>
					<th>Expiry date</th>
					<th>Status</th>
					<th class="text-right">Amount</th>
				</tr>
			</thead>
			<tbody>
				{#if loading}
					<tr><td colspan="7" class="text-center py-12 muted">Loading…</td></tr>
				{:else if quotes.length === 0}
					<tr>
						<td colspan="7" class="text-center py-12 muted">
							No quotes match your filters.
						</td>
					</tr>
				{:else}
					{#each quotes as q (q.QuoteID)}
						<tr>
							<td class="font-medium">
								<a
									class="text-brand-600 hover:underline"
									href="/app/sales/quotes/{q.QuoteID}"
								>
									{q.QuoteNumber || '—'}
								</a>
							</td>
							<td class="muted">{q.Reference || ''}</td>
							<td class="font-medium text-ink-900">{q.Contact?.Name ?? '—'}</td>
							<td>{formatDate(q.Date)}</td>
							<td>{formatDate(q.ExpiryDate)}</td>
							<td><span class={statusClass(q.Status)}>{statusLabel(q.Status)}</span></td>
							<td class="text-right tabular-nums">
								{formatCurrency(q.Total, q.CurrencyCode)}
							</td>
						</tr>
					{/each}
				{/if}
			</tbody>
		</table>
	</div>

	<!-- Pagination -->
	{#if totalPages > 1}
		<div class="flex items-center justify-between text-sm">
			<div class="muted">Page {pagination.page} of {totalPages}</div>
			<div class="flex gap-2">
				<button
					class="btn-secondary"
					type="button"
					disabled={pagination.page <= 1}
					onclick={() => setPage(pagination.page - 1)}
				>
					‹ Prev
				</button>
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
	{/if}
</div>
