<script lang="ts">
	import { browser } from '$app/environment';
	import AccountFormModal from '$lib/components/AccountFormModal.svelte';
	import { accountApi, orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import {
		COA_TABS,
		displayTypeColumn,
		STANDARD_CHART_IMPORT,
		tabMatches,
		ytdForAccount,
		type CoaTabId
	} from '$lib/chart-of-accounts';
	import type { Account } from '$lib/types';
	import { formatCurrency } from '$lib/utils/format';
	import { onMount } from 'svelte';

	let accounts = $state<Account[]>([]);
	let loading = $state(true);
	let tab = $state<CoaTabId>('all');
	let searchDraft = $state('');
	let searchQuery = $state('');
	let sortKey = $state<'code' | 'name' | 'type' | 'ytd'>('code');
	let sortDir = $state<'asc' | 'desc'>('asc');
	let pageSize = $state(100);
	let page = $state(1);
	let selected = $state<Record<string, boolean>>({});
	let currency = $state('USD');

	let modalOpen = $state(false);
	let modalMode = $state<'add' | 'edit'>('add');
	let modalAccount = $state<Account | null>(null);
	let importing = $state(false);
	let bulkWorking = $state(false);
	let toast = $state<string | null>(null);

	async function reload() {
		loading = true;
		try {
			const list = await accountApi.list({});
			accounts = Array.isArray(list) ? list : [];
		} catch {
			accounts = [];
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void orgApi.current().then((o) => {
			if (o?.BaseCurrency) currency = o.BaseCurrency;
		});
		void reload();
	});
	$effect(() => {
		if ($session.tenantId) void reload();
	});

	const filtered = $derived.by(() => {
		const q = searchQuery.trim().toLowerCase();
		return accounts.filter((a) => {
			if (!tabMatches(tab, a)) return false;
			if (!q) return true;
			return (
				a.Name.toLowerCase().includes(q) ||
				a.Code.toLowerCase().includes(q) ||
				(a.Description && a.Description.toLowerCase().includes(q))
			);
		});
	});

	const sorted = $derived.by(() => {
		const rows = [...filtered];
		const dir = sortDir === 'asc' ? 1 : -1;
		rows.sort((a, b) => {
			let cmp = 0;
			if (sortKey === 'code') cmp = a.Code.localeCompare(b.Code, undefined, { numeric: true });
			else if (sortKey === 'name') cmp = a.Name.localeCompare(b.Name);
			else if (sortKey === 'type') cmp = displayTypeColumn(a).localeCompare(displayTypeColumn(b));
			else {
				const ya = ytdForAccount(a);
				const yb = ytdForAccount(b);
				const va = ya ?? -Infinity;
				const vb = yb ?? -Infinity;
				cmp = va - vb;
			}
			return cmp * dir;
		});
		return rows;
	});

	const totalPages = $derived(Math.max(1, Math.ceil(sorted.length / pageSize)));
	const pageRows = $derived.by(() => {
		const start = (page - 1) * pageSize;
		return sorted.slice(start, start + pageSize);
	});

	$effect(() => {
		if (page > totalPages) page = totalPages;
	});

	function toggleSort(key: typeof sortKey) {
		if (sortKey === key) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
		else {
			sortKey = key;
			sortDir = 'asc';
		}
	}

	function toggleSelect(id: string) {
		selected = { ...selected, [id]: !selected[id] };
	}

	function toggleSelectAllOnPage() {
		const ids = pageRows.map((a) => a.AccountID);
		const allOn = ids.every((id) => selected[id]);
		const next = { ...selected };
		for (const id of ids) next[id] = !allOn;
		selected = next;
	}

	const selectedIds = $derived(Object.keys(selected).filter((id) => selected[id]));

	function openAdd() {
		modalMode = 'add';
		modalAccount = null;
		modalOpen = true;
	}

	function openEdit(a: Account) {
		modalMode = 'edit';
		modalAccount = a;
		modalOpen = true;
	}

	function applySearch() {
		searchQuery = searchDraft;
		page = 1;
	}

	async function bulkArchive() {
		if (selectedIds.length === 0) return;
		const n = selectedIds.length;
		const ids = [...selectedIds];
		bulkWorking = true;
		try {
			for (const id of ids) {
				await accountApi.delete(id);
			}
			selected = {};
			toast = `${n} account(s) moved to archive.`;
			await reload();
		} catch (e) {
			toast = (e as Error).message;
		} finally {
			bulkWorking = false;
		}
	}

	async function bulkRestore() {
		if (selectedIds.length === 0) return;
		const ids = [...selectedIds];
		bulkWorking = true;
		try {
			for (const id of ids) {
				const a = accounts.find((x) => x.AccountID === id);
				if (a) await accountApi.update(id, { ...a, Status: 'ACTIVE' });
			}
			selected = {};
			toast = 'Account(s) restored.';
			await reload();
		} catch (e) {
			toast = (e as Error).message;
		} finally {
			bulkWorking = false;
		}
	}

	async function importStandardChart() {
		if (!browser) return;
		importing = true;
		let added = 0;
		let skipped = 0;
		try {
			for (const row of STANDARD_CHART_IMPORT) {
				try {
					await accountApi.create(row);
					added++;
				} catch {
					skipped++;
				}
			}
			toast = `Import finished: ${added} added, ${skipped} skipped (already exist).`;
			await reload();
		} catch (e) {
			toast = (e as Error).message;
		} finally {
			importing = false;
		}
	}

	function exportCsv() {
		if (!browser) return;
		const headers = ['Code', 'Name', 'Type', 'Status', 'YTD'];
		const lines = [
			headers.join(','),
			...sorted.map((a) => {
				const y = ytdForAccount(a);
				const ytd = y !== null ? String(y) : '';
				return [a.Code, `"${a.Name.replace(/"/g, '""')}"`, displayTypeColumn(a), a.Status, ytd].join(
					','
				);
			})
		];
		const blob = new Blob([lines.join('\n')], { type: 'text/csv;charset=utf-8' });
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = 'chart-of-accounts.csv';
		a.click();
		URL.revokeObjectURL(a.href);
	}

	function printPage() {
		if (browser) window.print();
	}
</script>

<div class="coa-page space-y-0 print:space-y-4">
	<div class="mb-6 flex flex-col gap-2 print:hidden">
		<div class="text-sm text-ink-500">
			<a href="/app/settings" class="text-brand-700 hover:underline">Settings</a>
			<span class="mx-1 text-ink-400">›</span>
			<span class="text-ink-700">Chart of accounts</span>
		</div>
		<div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
			<h1 class="section-title">Chart of accounts</h1>
			<div class="flex flex-wrap items-center gap-2">
				<button type="button" class="btn-primary" onclick={openAdd}>+ Add Account</button>
				<a href="/app/accounting/bank-accounts/new" class="btn-primary">+ Add Bank Account</a>
				<button type="button" class="btn-ghost text-sm print:hidden" onclick={printPage}>Print PDF</button>
				<button
					type="button"
					class="btn-ghost text-sm print:hidden"
					onclick={importStandardChart}
					disabled={importing}
				>
					{importing ? 'Import…' : 'Import'}
				</button>
				<button type="button" class="btn-ghost text-sm print:hidden" onclick={exportCsv}>Export</button>
			</div>
		</div>
	</div>

	{#if toast}
		<p class="mb-4 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-900 print:hidden" role="status">
			{toast}
			<button type="button" class="ml-2 text-emerald-700 underline" onclick={() => (toast = null)}>Dismiss</button>
		</p>
	{/if}

	<!-- Tabs -->
	<nav
		class="flex flex-wrap gap-1 border-b border-ink-200 print:hidden"
		aria-label="Account categories"
	>
		{#each COA_TABS as t (t.id)}
			<button
				type="button"
				class="-mb-px border-b-2 px-3 py-2.5 text-sm font-medium transition {tab === t.id
					? 'border-brand-600 text-brand-700'
					: 'border-transparent text-ink-600 hover:text-ink-900'}"
				onclick={() => {
					tab = t.id;
					page = 1;
					selected = {};
				}}
			>
				{t.label}
			</button>
		{/each}
	</nav>

	<!-- Toolbar -->
	<div
		class="mt-4 flex flex-col gap-3 border border-ink-200 bg-ink-50/80 px-3 py-2 sm:flex-row sm:items-center sm:justify-between print:hidden"
	>
		<div class="flex flex-wrap items-center gap-2">
			<button
				type="button"
				class="btn-secondary btn-secondary-sm"
				disabled={selectedIds.length === 0 || bulkWorking || tab === 'archive'}
				onclick={bulkArchive}
			>
				Delete
			</button>
			<button
				type="button"
				class="btn-secondary btn-secondary-sm"
				disabled={selectedIds.length === 0 || bulkWorking || tab === 'archive'}
				onclick={bulkArchive}
			>
				Archive
			</button>
			<button type="button" class="btn-secondary btn-secondary-sm opacity-50" disabled title="Coming soon">
				Change Tax Rate
			</button>
			<a href="/app/bank-feeds" class="btn-secondary btn-secondary-sm">Refresh Bank Feeds</a>
			{#if tab === 'archive'}
				<button
					type="button"
					class="btn-secondary btn-secondary-sm"
					disabled={selectedIds.length === 0 || bulkWorking}
					onclick={bulkRestore}
				>
					Restore
				</button>
			{/if}
		</div>
		<div class="flex flex-1 items-center justify-end gap-2 sm:max-w-md">
			<input
				class="input min-w-0 flex-1"
				placeholder="Search accounts…"
				bind:value={searchDraft}
				onkeydown={(e) => e.key === 'Enter' && applySearch()}
			/>
			<button type="button" class="btn-secondary btn-secondary-sm shrink-0" onclick={applySearch}>Search</button>
		</div>
	</div>

	<div class="card mt-4 overflow-hidden print:shadow-none print:border-0">
		{#if loading}
			<div class="p-12 text-center text-ink-500">Loading…</div>
		{:else if sorted.length === 0}
			<div class="p-12 text-center text-ink-500">
				No accounts in this view.
				<button type="button" class="ml-2 text-brand-700 underline" onclick={importStandardChart}>
					Import standard chart
				</button>
			</div>
		{:else}
			<div class="overflow-x-auto">
				<table class="table-auto-xero text-sm">
					<thead>
						<tr>
							<th class="w-10">
								<input
									type="checkbox"
									class="rounded border-ink-300"
									checked={pageRows.length > 0 && pageRows.every((a) => selected[a.AccountID])}
									onchange={toggleSelectAllOnPage}
									aria-label="Select all on page"
								/>
							</th>
							<th class="cursor-pointer select-none" onclick={() => toggleSort('code')}>
								Code {sortKey === 'code' ? (sortDir === 'asc' ? '↑' : '↓') : ''}
							</th>
							<th class="cursor-pointer select-none" onclick={() => toggleSort('name')}>
								Name {sortKey === 'name' ? (sortDir === 'asc' ? '↑' : '↓') : ''}
							</th>
							<th class="cursor-pointer select-none" onclick={() => toggleSort('type')}>
								Type {sortKey === 'type' ? (sortDir === 'asc' ? '↑' : '↓') : ''}
							</th>
							<th class="cursor-pointer text-right select-none" onclick={() => toggleSort('ytd')}>
								YTD {sortKey === 'ytd' ? (sortDir === 'asc' ? '↑' : '↓') : ''}
							</th>
						</tr>
					</thead>
					<tbody>
						{#each pageRows as a (a.AccountID)}
							<tr>
								<td>
									<input
										type="checkbox"
										class="rounded border-ink-300"
										checked={!!selected[a.AccountID]}
										onchange={() => toggleSelect(a.AccountID)}
										aria-label="Select {a.Name}"
									/>
								</td>
								<td class="font-mono tabular-nums text-ink-900">{a.Code}</td>
								<td>
									<div class="flex items-start gap-2">
										{#if a.Type === 'BANK'}
											<span class="mt-0.5 text-ink-400" title="Bank" aria-hidden="true">
												<svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor"
													><path
														d="M12 3L4 9v12h16V9l-8-6zm0 2.2l5 3.75V19H7v-9.95l5-3.85zM9 14h6v2H9v-2z"
													/></svg
												>
											</span>
										{/if}
										<div>
											<button
												type="button"
												class="text-left font-medium text-brand-700 hover:underline"
												onclick={() => openEdit(a)}
											>
												{a.Name}
											</button>
											{#if a.Description}
												<div class="text-xs text-ink-500">{a.Description}</div>
											{/if}
										</div>
									</div>
								</td>
								<td class="text-ink-800">{displayTypeColumn(a)}</td>
								<td class="text-right tabular-nums font-medium">
									{#if ytdForAccount(a) !== null}
										{formatCurrency(ytdForAccount(a)!, currency)}
									{:else}
										<span class="text-ink-400">—</span>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>

			<div
				class="flex flex-col gap-2 border-t border-ink-100 px-4 py-3 text-sm text-ink-600 sm:flex-row sm:items-center sm:justify-between print:hidden"
			>
				<div class="flex items-center gap-2">
					<button
						type="button"
						class="rounded border border-ink-200 px-2 py-1 disabled:opacity-40"
						disabled={page <= 1}
						onclick={() => (page = Math.max(1, page - 1))}
					>
						‹
					</button>
					<span>Page {page} of {totalPages}</span>
					<button
						type="button"
						class="rounded border border-ink-200 px-2 py-1 disabled:opacity-40"
						disabled={page >= totalPages}
						onclick={() => (page = Math.min(totalPages, page + 1))}
					>
						›
					</button>
				</div>
				<label class="inline-flex items-center gap-2">
					<span class="text-ink-500">Show</span>
					<select class="select !py-1 !text-sm" bind:value={pageSize} onchange={() => (page = 1)}>
						<option value={25}>25</option>
						<option value={50}>50</option>
						<option value={100}>100</option>
					</select>
					<span>items per page</span>
				</label>
			</div>
		{/if}
	</div>
</div>

<AccountFormModal
	open={modalOpen}
	mode={modalMode}
	account={modalAccount}
	onClose={() => (modalOpen = false)}
	onSaved={() => {
		void reload();
		toast = 'Saved.';
	}}
/>
