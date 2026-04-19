<script lang="ts">
	import { onMount } from 'svelte';
	import { get } from 'svelte/store';
	import { orgFileApi, downloadOrgFileContent } from '$lib/api';
	import { session } from '$lib/stores/session';
	import type { OrgFile } from '$lib/types';
	import { formatDate } from '$lib/utils/format';

	type FolderTab = 'inbox' | 'archive';

	let tab = $state<FolderTab>('inbox');
	let loading = $state(true);
	let err = $state('');
	let files = $state<OrgFile[]>([]);
	let pagination = $state({ page: 1, pageSize: 50, total: 0 });
	let inboxTotal = $state(0);
	let archiveTotal = $state(0);
	let selected = $state<Set<string>>(new Set());
	let dragOver = $state(false);
	let fileInput: HTMLInputElement | undefined;
	let addMenuOpen = $state(false);
	let archiveMenuOpen = $state(false);
	let emailHint = $state(false);

	function formatBytes(n: number) {
		if (n < 1024) return `${n} B`;
		const u = ['KB', 'MB', 'GB'];
		let v = n / 1024;
		let i = 0;
		while (v >= 1024 && i < u.length - 1) {
			v /= 1024;
			i++;
		}
		return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
	}

	function filesInboxEmail(): string {
		const tid = get(session).tenantId || 'org';
		return `files+${tid}@files.inbox.local`;
	}

	async function copyInboxEmail() {
		const addr = filesInboxEmail();
		try {
			await navigator.clipboard.writeText(addr);
			emailHint = true;
			setTimeout(() => (emailHint = false), 2500);
		} catch {
			emailHint = true;
		}
	}

	async function refreshCounts() {
		const [inboxRes, archRes] = await Promise.all([
			orgFileApi.list({ folder: 'inbox', page: '1', pageSize: '1' }),
			orgFileApi.list({ folder: 'archive', page: '1', pageSize: '1' })
		]);
		inboxTotal = inboxRes.Pagination.total;
		archiveTotal = archRes.Pagination.total;
	}

	async function loadList(opts: { refreshTotals?: boolean } = {}) {
		loading = true;
		err = '';
		try {
			const res = await orgFileApi.list({
				folder: tab,
				page: String(pagination.page),
				pageSize: String(pagination.pageSize)
			});
			files = res.Files ?? [];
			pagination = res.Pagination;
			selected = new Set();
			if (opts.refreshTotals) await refreshCounts();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load files';
			files = [];
		} finally {
			loading = false;
		}
	}

	function setTab(next: FolderTab) {
		tab = next;
		pagination = { ...pagination, page: 1 };
		loadList();
	}

	async function uploadFiles(list: FileList | File[]) {
		const arr = Array.from(list);
		for (const f of arr) {
			await orgFileApi.upload(f, tab);
		}
		await loadList({ refreshTotals: true });
	}

	function onPickFiles(e: Event) {
		const input = e.target as HTMLInputElement;
		if (input.files?.length) uploadFiles(input.files);
		input.value = '';
	}

	function toggle(id: string) {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selected = next;
	}

	function toggleAll() {
		if (selected.size === files.length) {
			selected = new Set();
		} else {
			selected = new Set(files.map((f) => f.AttachmentID));
		}
	}

	async function moveSelected(to: 'INBOX' | 'ARCHIVE') {
		if (selected.size === 0) return;
		addMenuOpen = false;
		archiveMenuOpen = false;
		await orgFileApi.move([...selected], to);
		await loadList({ refreshTotals: true });
	}

	async function deleteSelected() {
		if (selected.size === 0) return;
		if (!confirm(`Delete ${selected.size} file(s)?`)) return;
		await orgFileApi.delete([...selected]);
		await loadList({ refreshTotals: true });
	}

	function onDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;
		if (tab !== 'inbox') return;
		const dt = e.dataTransfer?.files;
		if (dt?.length) uploadFiles(dt);
	}

	onMount(async () => {
		await refreshCounts();
		await loadList();
	});
</script>

<div class="w-full min-w-0 space-y-0">
	<p class="text-sm mb-1">
		<a href="/app" class="text-brand-600 hover:underline">Home</a>
	</p>

	<div class="flex flex-col gap-4 sm:flex-row sm:flex-wrap sm:items-start sm:justify-between mb-4">
		<h1 class="section-title shrink-0">Files</h1>
		<div class="flex flex-wrap items-stretch sm:items-center gap-2 w-full sm:w-auto">
			<button
				type="button"
				class="btn-secondary text-sm"
				onclick={() => copyInboxEmail()}
				title="Copy a unique inbox address for email forwarding"
			>
				Email to Files Inbox
			</button>
			<button
				type="button"
				class="inline-flex items-center justify-center gap-2 rounded-md px-4 py-2 text-sm font-medium text-white shadow-card transition bg-[#13854E] hover:bg-[#0f6d40] focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-emerald-600"
				onclick={() => fileInput?.click()}
			>
				{tab === 'inbox' ? 'Upload to Files Inbox' : 'Upload to Archive'}
			</button>
			<input
				bind:this={fileInput}
				type="file"
				multiple
				class="sr-only"
				aria-hidden="true"
				onchange={onPickFiles}
			/>
		</div>
	</div>

	{#if emailHint}
		<p class="text-sm text-emerald-800 mb-2" role="status">Inbox address copied: {filesInboxEmail()}</p>
	{/if}

	<div class="flex gap-4 sm:gap-8 border-b border-ink-200 mb-0 overflow-x-auto">
		<button
			type="button"
			class="pb-3 text-sm font-medium border-b-2 -mb-px transition-colors {tab === 'inbox'
				? 'border-brand-500 text-brand-700'
				: 'border-transparent text-ink-600 hover:text-ink-900'}"
			onclick={() => setTab('inbox')}
		>
			Inbox {inboxTotal}
		</button>
		<button
			type="button"
			class="pb-3 text-sm font-medium border-b-2 -mb-px transition-colors {tab === 'archive'
				? 'border-brand-500 text-brand-700'
				: 'border-transparent text-ink-600 hover:text-ink-900'}"
			onclick={() => setTab('archive')}
		>
			Archive {archiveTotal}
		</button>
	</div>

	<div
		class="flex flex-wrap items-center gap-2 py-2 px-2 sm:px-3 bg-ink-50 border border-t-0 border-ink-100 rounded-b-lg w-full min-w-0"
	>
		<details class="relative" bind:open={addMenuOpen}>
			<summary
				class="btn-secondary-sm list-none cursor-pointer select-none [&::-webkit-details-marker]:hidden"
			>
				Add to new <span class="text-ink-400" aria-hidden="true">▾</span>
			</summary>
			<div
				class="absolute left-0 top-full mt-1 min-w-[200px] rounded-md border border-ink-200 bg-white py-1 shadow-pop z-20 text-sm"
			>
				<p class="px-3 py-2 text-ink-500">Attach to a new document (soon)</p>
				<button
					type="button"
					class="w-full text-left px-3 py-2 text-ink-400 cursor-not-allowed"
					disabled
				>
					Bill
				</button>
				<button
					type="button"
					class="w-full text-left px-3 py-2 text-ink-400 cursor-not-allowed"
					disabled
				>
					Invoice
				</button>
				<button
					type="button"
					class="w-full text-left px-3 py-2 text-ink-400 cursor-not-allowed"
					disabled
				>
					Expense
				</button>
			</div>
		</details>

		<details class="relative" bind:open={archiveMenuOpen}>
			<summary
				class="btn-secondary-sm list-none cursor-pointer select-none [&::-webkit-details-marker]:hidden"
			>
				Archive to <span class="text-ink-400" aria-hidden="true">▾</span>
			</summary>
			<div
				class="absolute left-0 top-full mt-1 min-w-[180px] rounded-md border border-ink-200 bg-white py-1 shadow-pop z-20 text-sm"
			>
				{#if tab === 'inbox'}
					<button
						type="button"
						class="w-full text-left px-3 py-2 hover:bg-ink-50 disabled:opacity-40"
						disabled={selected.size === 0}
						onclick={() => moveSelected('ARCHIVE')}
					>
						Archive
					</button>
				{:else}
					<button
						type="button"
						class="w-full text-left px-3 py-2 hover:bg-ink-50 disabled:opacity-40"
						disabled={selected.size === 0}
						onclick={() => moveSelected('INBOX')}
					>
						Inbox
					</button>
				{/if}
			</div>
		</details>

		<button
			type="button"
			class="text-sm text-ink-700 hover:underline disabled:opacity-40 disabled:no-underline"
			disabled={selected.size === 0}
			onclick={deleteSelected}
		>
			Delete
		</button>
	</div>

	{#if err}
		<p class="text-sm text-red-700 mt-4" role="alert">{err}</p>
	{/if}

	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="mt-4 w-full min-w-0 rounded-lg border border-dashed min-h-[min(320px,55vh)] sm:min-h-[320px] transition-colors {dragOver && tab === 'inbox'
			? 'border-brand-400 bg-brand-50/40'
			: 'border-ink-200 bg-white'}"
		ondragover={(e) => {
			e.preventDefault();
			if (tab === 'inbox') dragOver = true;
		}}
		ondragleave={() => (dragOver = false)}
		ondrop={onDrop}
	>
		{#if loading}
			<div class="p-16 text-center muted">Loading…</div>
		{:else if files.length === 0}
			<div class="flex flex-col items-center justify-center py-16 px-6 text-center">
				<div class="mb-6 relative w-40 h-40" aria-hidden="true">
					<div
						class="absolute inset-0 rounded-full bg-sky-100 border border-sky-200/80 shadow-inner"
					></div>
					<svg class="absolute inset-4 w-32 h-32" viewBox="0 0 120 120" fill="none">
						<ellipse cx="60" cy="88" rx="28" ry="8" fill="#E0F0FA" />
						<path
							d="M60 24 L88 52 L88 78 L32 78 L32 52 Z"
							fill="#1E8BCB"
							stroke="#0F6FA3"
							stroke-width="2"
						/>
						<rect x="38" y="56" width="44" height="22" rx="2" fill="white" stroke="#94C9E8" />
						<rect x="42" y="60" width="36" height="4" rx="1" fill="#E8F4FC" />
						<rect x="42" y="68" width="24" height="4" rx="1" fill="#E8F4FC" />
						<circle cx="24" cy="28" r="3" fill="#F5D547" opacity="0.9" />
						<circle cx="98" cy="36" r="2" fill="#F5D547" opacity="0.7" />
					</svg>
				</div>
				<h2 class="content-section-title">Upload files to start review</h2>
				<p class="mt-2 text-sm text-ink-600 max-w-md">
					{#if tab === 'inbox'}
						Your inbox is empty. Drag and drop files here to start your uploads.
					{:else}
						No archived files yet. Move files from the inbox or upload here.
					{/if}
				</p>
				<button
					type="button"
					class="btn-primary mt-6"
					onclick={() => fileInput?.click()}
				>
					Upload files
				</button>
			</div>
		{:else}
			<div class="overflow-x-auto">
				<table class="table-auto-xero">
					<thead>
						<tr>
							<th class="w-10">
								<input
									type="checkbox"
									checked={files.length > 0 && selected.size === files.length}
									onchange={toggleAll}
									aria-label="Select all"
								/>
							</th>
							<th>Name</th>
							<th>Size</th>
							<th>Uploaded</th>
							<th class="text-right">Actions</th>
						</tr>
					</thead>
					<tbody>
						{#each files as f}
							<tr>
								<td>
									<input
										type="checkbox"
										checked={selected.has(f.AttachmentID)}
										onchange={() => toggle(f.AttachmentID)}
										aria-label="Select {f.FileName}"
									/>
								</td>
								<td class="font-medium text-ink-900">{f.FileName}</td>
								<td class="muted tabular-nums">{formatBytes(f.ContentLength)}</td>
								<td class="muted text-sm">{formatDate(f.CreatedDateUTC)}</td>
								<td class="text-right">
									<button
										type="button"
										class="text-brand-600 hover:underline text-sm"
										onclick={() => downloadOrgFileContent(f.AttachmentID, f.FileName)}
									>
										Download
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>
</div>
