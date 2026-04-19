<script lang="ts">
	import { onMount } from 'svelte';
	import {
		bankFeedApi,
		accountApi,
		type BankFeedConnection,
		type BankFeedAccount,
		type BankFeedInstitution
	} from '$lib/api';
	import type { Account } from '$lib/types';
	import { session } from '$lib/stores/session';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let providers = $state<string[]>([]);
	let connections = $state<BankFeedConnection[]>([]);
	let feedAccounts = $state<BankFeedAccount[]>([]);
	let bankAccounts = $state<Account[]>([]);
	let loading = $state(true);

	let showConnect = $state(false);
	let selectedProvider = $state('');
	let country = $state('');
	let institutions = $state<BankFeedInstitution[]>([]);
	let instFilter = $state('');
	let error = $state('');

	async function reload() {
		loading = true;
		try {
			const [p, c, accs] = await Promise.all([
				bankFeedApi.providers().catch(() => ({ Providers: [] })),
				bankFeedApi
					.listConnections()
					.catch(() => ({ Connections: [], Accounts: [] })),
				accountApi.list({ status: 'ACTIVE' }).catch(() => [])
			]);
			providers = p.Providers ?? [];
			connections = c.Connections ?? [];
			feedAccounts = c.Accounts ?? [];
			bankAccounts = (accs as Account[]).filter((a) => a.Type === 'BANK');
		} finally {
			loading = false;
		}
	}
	onMount(reload);
	$effect(() => { if ($session.tenantId) void reload(); });

	async function openConnect(p: string) {
		selectedProvider = p;
		showConnect = true;
		error = '';
		institutions = [];
		try {
			const res = await bankFeedApi.institutions(p, country);
			institutions = res.Institutions ?? [];
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function reloadInstitutions() {
		if (!selectedProvider) return;
		try {
			const res = await bankFeedApi.institutions(selectedProvider, country);
			institutions = res.Institutions ?? [];
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function startConnection(inst: BankFeedInstitution) {
		try {
			const res = await bankFeedApi.createConnection({
				provider: selectedProvider,
				institutionId: inst.ID,
				institutionName: inst.Name
			});
			showConnect = false;
			const authURL = res.Connections?.[0]?.AuthURL;
			if (authURL) {
				window.open(authURL, '_blank', 'noopener');
			}
			await reload();
		} catch (e) {
			error = (e as Error).message;
		}
	}

	async function finalize(c: BankFeedConnection) {
		try {
			await bankFeedApi.finalize(c.FeedConnectionID);
			await reload();
		} catch (e) {
			alert((e as Error).message);
		}
	}
	async function sync(c: BankFeedConnection) {
		try {
			const res = await bankFeedApi.sync(c.FeedConnectionID);
			alert(`Fetched ${res.Fetched} line${res.Fetched === 1 ? '' : 's'} (${res.NewLines} new)`);
			await reload();
		} catch (e) {
			alert((e as Error).message);
		}
	}
	async function remove(c: BankFeedConnection) {
		if (!confirm(`Disconnect from ${c.InstitutionName ?? c.Provider}?`)) return;
		await bankFeedApi.deleteConnection(c.FeedConnectionID);
		await reload();
	}
	async function bind(fa: BankFeedAccount, accountId: string) {
		if (!accountId) return;
		await bankFeedApi.bindAccount(fa.FeedAccountID, accountId);
		await reload();
	}

	const filteredInstitutions = $derived(
		institutions.filter(
			(i) => !instFilter || i.Name.toLowerCase().includes(instFilter.toLowerCase())
		)
	);
</script>

<ModuleHeader
	title="Bank feeds"
	subtitle="Connect your bank once — transactions flow in automatically via Open Banking."
/>

{#if providers.length === 0 && !loading}
	<div class="card p-6 mb-6">
		<div class="muted">No bank feed providers configured on the server.</div>
	</div>
{:else}
	<div class="card p-5 mb-6">
		<div class="flex items-center justify-between mb-3">
			<h2 class="content-section-title">Connect a bank</h2>
		</div>
		<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
			{#each providers as name}
				<button
					type="button"
					class="card p-4 text-left hover:border-brand-400 transition"
					onclick={() => openConnect(name)}
				>
					<div class="font-semibold capitalize">{name}</div>
					<div class="text-xs mt-1 text-emerald-700">Configured</div>
				</button>
			{/each}
		</div>
	</div>
{/if}

<div class="card p-5">
	<h2 class="content-section-title mb-4">Connections</h2>
	{#if loading}
		<div class="muted">Loading…</div>
	{:else if connections.length === 0}
		<div class="muted">No connections yet.</div>
	{:else}
		<div class="space-y-4">
			{#each connections as c}
				<div class="border border-ink-100 rounded-lg p-4">
					<div class="flex items-start justify-between gap-3 flex-wrap">
						<div>
							<div class="font-semibold">{c.InstitutionName ?? 'Institution'}</div>
							<div class="text-xs muted">
								{c.Provider} · <span class="uppercase">{c.Status}</span>
								{#if c.Country} · {c.Country}{/if}
							</div>
						</div>
						<div class="flex gap-2">
							{#if c.Status === 'PENDING' && c.AuthURL}
								<a href={c.AuthURL} target="_blank" rel="noopener noreferrer" class="btn-primary !py-1.5 !px-3 !text-xs">Open consent link</a>
								<button class="btn-secondary !py-1.5 !px-3 !text-xs" onclick={() => finalize(c)}>I've finished</button>
							{:else if c.Status === 'LINKED'}
								<button class="btn-primary !py-1.5 !px-3 !text-xs" onclick={() => sync(c)}>Sync now</button>
							{/if}
							<button class="btn-secondary !py-1.5 !px-3 !text-xs" onclick={() => remove(c)}>Disconnect</button>
						</div>
					</div>

					{#if feedAccounts.filter((a) => a.FeedConnectionID === c.FeedConnectionID).length}
						<table class="table-auto-xero mt-4">
							<thead>
								<tr>
									<th>Bank account</th>
									<th>IBAN</th>
									<th>Currency</th>
									<th>Linked ledger account</th>
								</tr>
							</thead>
							<tbody>
								{#each feedAccounts.filter((a) => a.FeedConnectionID === c.FeedConnectionID) as fa}
									<tr>
										<td class="font-medium">{fa.DisplayName ?? fa.ExternalAccountID}</td>
										<td class="tabular-nums">{fa.IBAN ?? '—'}</td>
										<td>{fa.CurrencyCode ?? '—'}</td>
										<td>
											<select
												class="input"
												value={fa.AccountID ?? ''}
												onchange={(e) => bind(fa, (e.target as HTMLSelectElement).value)}
											>
												<option value="">— select account —</option>
												{#each bankAccounts as acc}
													<option value={acc.AccountID}>{acc.Name} ({acc.Code})</option>
												{/each}
											</select>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>

{#if showConnect}
	<div class="fixed inset-0 bg-ink-900/40 flex items-center justify-center z-50 p-4">
		<div class="bg-white rounded-xl shadow-pop w-full max-w-2xl max-h-[90vh] flex flex-col">
			<div class="p-5 border-b border-ink-100 flex items-center justify-between">
				<h3 class="font-semibold">Connect via {selectedProvider}</h3>
				<button class="btn-ghost" onclick={() => (showConnect = false)}>Close</button>
			</div>
			<div class="p-5 border-b border-ink-100 flex gap-3">
				<input class="input flex-1" placeholder="Filter institutions…" bind:value={instFilter} />
				<input class="input w-24" placeholder="Country" bind:value={country} onchange={reloadInstitutions} />
			</div>
			<div class="overflow-y-auto flex-1 p-2">
				{#if error}
					<div class="p-3 text-sm text-red-700">{error}</div>
				{/if}
				{#each filteredInstitutions as inst}
					<button
						class="w-full flex items-center gap-3 p-3 rounded-md hover:bg-ink-50 text-left"
						onclick={() => startConnection(inst)}
					>
						{#if inst.LogoURL}
							<img src={inst.LogoURL} alt="" class="h-8 w-8 rounded object-contain bg-ink-100" />
						{:else}
							<div class="h-8 w-8 rounded bg-ink-100"></div>
						{/if}
						<div>
							<div class="font-medium">{inst.Name}</div>
							<div class="text-xs muted">{(inst.Countries ?? []).join(', ')}</div>
						</div>
					</button>
				{/each}
				{#if filteredInstitutions.length === 0}
					<div class="p-6 muted text-center text-sm">No institutions found.</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
