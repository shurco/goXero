<script lang="ts">
	import {
		bankFeedApi,
		accountApi,
		type BankFeedConnection,
		type BankFeedAccount,
		type BankFeedInstitution,
		type BankFeedProvider
	} from '$lib/api';
	import BankInstitutionPicker, {
		defaultCountry
	} from '$lib/components/BankInstitutionPicker.svelte';
	import type { Account } from '$lib/types';
	import { session } from '$lib/stores/session';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let providers = $state<BankFeedProvider[]>([]);
	let connections = $state<BankFeedConnection[]>([]);
	let bankAccounts = $state<Account[]>([]);
	let loading = $state(true);

	let showConnect = $state(false);
	let selectedProvider = $state('');
	/** The country the search runs under, bound into the picker. */
	let country = $state('');
	let error = $state('');

	async function reload() {
		loading = true;
		try {
			const [p, c, accs] = await Promise.all([
				bankFeedApi.providers().catch(() => ({ Providers: [] })),
				bankFeedApi.listConnections().catch(() => ({ Connections: [] })),
				accountApi.list({ status: 'ACTIVE' }).catch(() => [])
			]);
			providers = p.Providers ?? [];
			connections = c.Connections ?? [];
			bankAccounts = (accs as Account[]).filter((a) => a.Type === 'BANK');
		} finally {
			loading = false;
		}
	}
	$effect(() => { if ($session.tenantId) void reload(); });

	function openConnect(p: string) {
		selectedProvider = p;
		showConnect = true;
		error = '';
		// Reset per provider — the country one adapter was searched under is not
		// the country to search the next one in.
		country = defaultCountry(p);
	}

	/**
	 * Start consent. The institution is optional: a provider whose own flow has a
	 * picker (Plaid's Hosted Link is the whole Link flow) accepts a request that
	 * names no bank and asks the user there. That is the only way in on an account
	 * that refuses a pre-selected institution, and the way to reach a bank our
	 * catalogue has not listed — the connection is labelled with whatever they
	 * pick once consent completes.
	 */
	async function startConnection(inst?: BankFeedInstitution) {
		try {
			const res = await bankFeedApi.createConnection({
				provider: selectedProvider,
				institutionId: inst?.ID,
				institutionName: inst?.Name,
				country
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
			await bankFeedApi.finalize(c.ConnectionID);
			await reload();
		} catch (e) {
			alert((e as Error).message);
		}
	}
	async function sync(c: BankFeedConnection) {
		try {
			const res = await bankFeedApi.sync(c.ConnectionID);
			const extra: string[] = [];
			if (res.AutoMatched) extra.push(`auto-matched ${res.AutoMatched}`);
			if (res.UpstreamChanges) extra.push(`${res.UpstreamChanges} line(s) the bank changed`);
			alert(
				`Fetched ${res.Fetched} line${res.Fetched === 1 ? '' : 's'} (${res.NewLines} new)` +
					(extra.length ? `, ${extra.join(', ')}` : '')
			);
			await reload();
		} catch (e) {
			alert((e as Error).message);
		}
	}
	/**
	 * Send a broken connection back to the bank to be repaired where it stands.
	 * The user then consents in a new tab, and either the redirect or the
	 * provider's own webhook finishes the job — both land on `finalize`.
	 */
	async function reconnect(c: BankFeedConnection) {
		try {
			const res = await bankFeedApi.reconnect(c.ConnectionID);
			const authURL = res.Connections?.[0]?.AuthURL;
			if (authURL) window.open(authURL, '_blank', 'noopener');
			await reload();
		} catch (e) {
			alert((e as Error).message);
		}
	}
	async function remove(c: BankFeedConnection) {
		if (!confirm(`Disconnect from ${c.InstitutionName ?? c.Provider}?`)) return;
		await bankFeedApi.deleteConnection(c.ConnectionID);
		await reload();
	}
	async function bind(fa: BankFeedAccount, accountId: string) {
		if (!accountId) return;
		await bankFeedApi.bindAccount(fa.FeedAccountID, accountId);
		await reload();
	}

	/**
	 * What the provider registered under this slug can do. False for a provider
	 * the server no longer offers, which is the safe answer: it hides the offer
	 * rather than showing a button that cannot work.
	 */
	function providerCapabilities(slug: string) {
		return providers.find((p) => p.Slug === slug);
	}

	/** Whether the provider's own consent flow can ask for the bank itself. */
	const providerPicksInstitution = $derived(
		providerCapabilities(selectedProvider)?.PicksInstitution ?? false
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
			{#each providers as p (p.Slug)}
				<button
					type="button"
					class="card p-4 text-left hover:border-brand-400 transition"
					onclick={() => openConnect(p.Slug)}
				>
					<div class="font-semibold capitalize">{p.Slug}</div>
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
			{#each connections as c (c.ConnectionID)}
				<div class="border border-ink-100 rounded-lg p-4">
					<div class="flex items-start justify-between gap-3 flex-wrap">
						<div>
							<div class="font-semibold">{c.InstitutionName ?? 'Institution'}</div>
							<div class="text-xs muted">
								{c.Provider} · <span class="uppercase">{c.Status}</span>
								{#if c.Country} · {c.Country}{/if}
							</div>
							{#if c.LastError}
								<div class="text-xs text-red-700 mt-1">{c.LastError}</div>
							{/if}
						</div>
						<div class="flex gap-2">
							{#if (c.Status === 'PENDING' || c.Status === 'ERROR') && c.AuthURL}
								<!-- A session is open: either a first-time consent or a repair. -->
								<a href={c.AuthURL} target="_blank" rel="noopener noreferrer" class="btn-primary !py-1.5 !px-3 !text-xs">Open consent link</a>
								<button class="btn-secondary !py-1.5 !px-3 !text-xs" onclick={() => finalize(c)}>I've finished</button>
							{:else if c.Status === 'LINKED'}
								<button class="btn-primary !py-1.5 !px-3 !text-xs" onclick={() => sync(c)}>Sync now</button>
							{:else if c.Status === 'ERROR' && providerCapabilities(c.Provider)?.CanRepair}
								<!-- Broken at the bank: re-authenticate this connection rather than
								     connecting the same account a second time. Only a provider whose
								     adapter can repair in place offers it — the rest report the
								     consent as gone and expect a fresh connection, so for them the
								     way back is Disconnect. -->
								<button class="btn-primary !py-1.5 !px-3 !text-xs" onclick={() => reconnect(c)}>Reconnect</button>
							{/if}
							<button class="btn-secondary !py-1.5 !px-3 !text-xs" onclick={() => remove(c)}>Disconnect</button>
						</div>
					</div>

					{#if c.Accounts?.length}
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
								{#each c.Accounts as fa (fa.FeedAccountID)}
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
												{#each bankAccounts as acc (acc.AccountID)}
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
			<div class="p-5 overflow-y-auto">
				{#if error}
					<div class="mb-3 text-sm text-red-700">{error}</div>
				{/if}
				<!-- The search, the results and the provider's own picker are one
				     component, shared with the Add bank account screen. -->
				<BankInstitutionPicker
					provider={selectedProvider}
					bind:country
					picksInstitution={providerPicksInstitution}
					onselect={startConnection}
				/>
			</div>
		</div>
	</div>
{/if}
