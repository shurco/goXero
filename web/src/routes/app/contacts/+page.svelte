<script lang="ts">
	import { contactApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { page as pageStore } from '$app/stores';
	import type { Contact, Pagination } from '$lib/types';
	import { onMount } from 'svelte';
	import { formatDate, statusClass } from '$lib/utils/format';

	let loading = $state(true);
	let contacts = $state<Contact[]>([]);
	let pagination = $state<Pagination>({ page: 1, pageSize: 25, total: 0 });
	let search = $state('');
	let tab = $state<'all' | 'customers' | 'suppliers'>('all');
	let showCreate = $state(false);
	let newContact = $state({ Name: '', FirstName: '', LastName: '', EmailAddress: '', IsCustomer: true, IsSupplier: false });

	$effect(() => {
		const q = $pageStore.url.searchParams.get('type');
		if (q === 'customer') tab = 'customers';
		else if (q === 'supplier') tab = 'suppliers';
		if ($pageStore.url.searchParams.get('new') === '1') showCreate = true;
	});

	async function reload() {
		loading = true;
		try {
			const params: Record<string, string> = {
				page: String(pagination.page),
				pageSize: String(pagination.pageSize)
			};
			if (search) params.search = search;
			if (tab === 'customers') params.isCustomer = 'true';
			if (tab === 'suppliers') params.isSupplier = 'true';
			const res = await contactApi.list(params);
			contacts = res?.Contacts ?? [];
			pagination = res?.Pagination ?? pagination;
		} finally {
			loading = false;
		}
	}

	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	async function create() {
		await contactApi.create(newContact);
		showCreate = false;
		newContact = { Name: '', FirstName: '', LastName: '', EmailAddress: '', IsCustomer: true, IsSupplier: false };
		reload();
	}
</script>

<div class="space-y-6">
	<div class="flex items-start justify-between flex-wrap gap-4">
		<div>
			<h1 class="section-title">Contacts</h1>
			<p class="muted">People and companies you do business with.</p>
		</div>
		<button class="btn-primary" onclick={() => (showCreate = true)}>+ New contact</button>
	</div>

	<div class="flex gap-6 border-b border-ink-100 text-sm">
		{#each [{ v: 'all', l: 'All' }, { v: 'customers', l: 'Customers' }, { v: 'suppliers', l: 'Suppliers' }] as t}
			<button
				class="pb-3 -mb-px border-b-2 {tab === t.v ? 'border-brand-500 text-brand-700 font-semibold' : 'border-transparent text-ink-600 hover:text-ink-900'}"
				onclick={() => { tab = t.v as typeof tab; pagination.page = 1; reload(); }}
			>
				{t.l}
			</button>
		{/each}
	</div>

	<div class="card p-4 flex gap-3">
		<input class="input w-80" placeholder="Search contacts…" bind:value={search} onkeydown={(e) => e.key === 'Enter' && (pagination.page = 1, reload())} />
		<button class="btn-secondary" onclick={() => (pagination.page = 1, reload())}>Search</button>
	</div>

	<div class="card overflow-x-auto">
		<table class="table-auto-xero">
			<thead>
				<tr>
					<th>Name</th>
					<th>Email</th>
					<th>Type</th>
					<th>Status</th>
					<th>Updated</th>
				</tr>
			</thead>
			<tbody>
				{#each contacts as c}
					<tr>
						<td class="font-medium text-ink-900">{c.Name}</td>
						<td class="muted">{c.EmailAddress || '—'}</td>
						<td>
							<div class="flex gap-1">
								{#if c.IsCustomer}<span class="badge badge-active">Customer</span>{/if}
								{#if c.IsSupplier}<span class="badge bg-indigo-100 text-indigo-800">Supplier</span>{/if}
							</div>
						</td>
						<td><span class={statusClass(c.ContactStatus)}>{c.ContactStatus}</span></td>
						<td class="muted">{formatDate(c.UpdatedDateUTC)}</td>
					</tr>
				{/each}
				{#if !loading && contacts.length === 0}
					<tr><td colspan="5" class="text-center py-12 muted">No contacts yet.</td></tr>
				{/if}
			</tbody>
		</table>
	</div>
</div>

{#if showCreate}
	<div class="fixed inset-0 bg-ink-900/50 flex items-center justify-center z-50 p-4">
		<div class="bg-white rounded-xl shadow-pop max-w-lg w-full p-6">
			<h3 class="text-lg font-semibold">New contact</h3>
			<div class="mt-4 grid grid-cols-2 gap-3">
				<div class="col-span-2"><label class="label" for="contact-name">Name *</label><input id="contact-name" class="input" bind:value={newContact.Name} /></div>
				<div><label class="label" for="contact-first">First name</label><input id="contact-first" class="input" bind:value={newContact.FirstName} /></div>
				<div><label class="label" for="contact-last">Last name</label><input id="contact-last" class="input" bind:value={newContact.LastName} /></div>
				<div class="col-span-2"><label class="label" for="contact-email">Email</label><input id="contact-email" class="input" type="email" bind:value={newContact.EmailAddress} /></div>
				<label class="flex items-center gap-2"><input type="checkbox" bind:checked={newContact.IsCustomer} /> Customer</label>
				<label class="flex items-center gap-2"><input type="checkbox" bind:checked={newContact.IsSupplier} /> Supplier</label>
			</div>
			<div class="flex justify-end gap-2 mt-6">
				<button class="btn-secondary" onclick={() => (showCreate = false)}>Cancel</button>
				<button class="btn-primary" onclick={create} disabled={!newContact.Name}>Create</button>
			</div>
		</div>
	</div>
{/if}
