<script lang="ts">
	import { goto } from '$app/navigation';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import BankInstitutionPicker, {
		defaultCountry
	} from '$lib/components/BankInstitutionPicker.svelte';
	import {
		bankFeedApi,
		type BankFeedConnection,
		type BankFeedInstitution,
		type BankFeedProvider
	} from '$lib/api';
	import { BANK_BRANDS, BANK_COUNTRIES, type BankBrand } from '$lib/bank-brand';
	import { session } from '$lib/stores/session';

	// ── A live connection, for the providers this server has credentials for ──
	let providers = $state<BankFeedProvider[]>([]);
	let loadingProviders = $state(true);
	let provider = $state('');
	/** The country the search runs under, bound into the picker. */
	let country = $state('');
	let error = $state('');
	let busy = $state(false);
	/**
	 * The consent session this page opened. It is held so the page can offer the
	 * link again if the popup was blocked, and so "I've finished" knows which
	 * connection to complete.
	 */
	let pending = $state<BankFeedConnection | null>(null);

	$effect(() => {
		if ($session.tenantId) void loadProviders();
	});

	async function loadProviders() {
		try {
			const res = await bankFeedApi.providers();
			providers = res.Providers ?? [];
			if (providers.length > 0 && !provider) selectProvider(providers[0].Slug);
		} catch {
			// A server with no aggregator configured answers with an empty list;
			// an unreachable one leaves the manual path below as the way through.
			providers = [];
		} finally {
			loadingProviders = false;
		}
	}

	function selectProvider(slug: string) {
		provider = slug;
		country = defaultCountry(slug);
	}

	const providerPicksInstitution = $derived(
		providers.find((p) => p.Slug === provider)?.PicksInstitution ?? false
	);

	/**
	 * Start a consent. The bank is optional: a provider whose own flow has a
	 * picker (Plaid's Hosted Link is the whole Link flow) accepts a request that
	 * names no bank and asks the user there — the only way in on an account that
	 * refuses a pre-selected institution, and the way to reach a bank our search
	 * has not listed. The connection is labelled with whatever they pick.
	 */
	async function startConnection(inst?: BankFeedInstitution) {
		error = '';
		busy = true;
		try {
			const res = await bankFeedApi.createConnection({
				provider,
				institutionId: inst?.ID,
				institutionName: inst?.Name,
				country
			});
			const conn = res.Connections?.[0] ?? null;
			pending = conn;
			if (conn?.AuthURL) window.open(conn.AuthURL, '_blank', 'noopener');
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not start the connection.';
		} finally {
			busy = false;
		}
	}

	/**
	 * The result of a consent is only reachable server-side — for Plaid the
	 * public token never reaches the browser — so this page cannot complete the
	 * connection itself, only ask the API to. What comes back is the accounts the
	 * bank authorised, which are bound to ledger accounts on the bank feeds
	 * screen; that is where the user is sent next.
	 */
	async function finish() {
		if (!pending) return;
		error = '';
		busy = true;
		try {
			await bankFeedApi.finalize(pending.ConnectionID);
			await goto('/app/bank-feeds');
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not finish the connection.';
		} finally {
			busy = false;
		}
	}

	// ── No provider configured: the manual path, with a brand to name it after ──
	let brandQuery = $state('');
	let brandCountry = $state('US');

	const brands = $derived.by(() => {
		const q = brandQuery.trim().toLowerCase();
		return BANK_BRANDS.filter((b) => {
			if (brandCountry && b.country !== brandCountry) return false;
			if (!q) return true;
			return b.name.toLowerCase().includes(q);
		});
	});
	const popular = $derived(brands.filter((b) => b.popular));
	const others = $derived(brands.filter((b) => !b.popular));

	function selectBrand(b: BankBrand) {
		const n = encodeURIComponent(b.name);
		void goto(`/app/accounting/bank-accounts/new/manual?name=${n}`);
	}
</script>

<ModuleHeader
	title="Add bank account"
	subtitle="Connect a bank feed or add an account manually."
	primary={{ label: 'Add without bank feed', href: '/app/accounting/bank-accounts/new/manual' }}
/>

<p class="text-sm mb-4 flex items-center justify-between gap-3 flex-wrap">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
	<a href="/app/bank-feeds" class="text-brand-600 hover:underline">Manage connected banks</a>
</p>

{#if pending}
	<div class="card p-6 max-w-3xl mx-auto">
		<h2 class="content-section-title">Finish connecting {pending.InstitutionName || provider}</h2>
		<p class="muted text-sm mt-2">
			Your bank's consent screen has been opened in a new tab. Approve access there and come back
			here — the connection is also completed for us when the bank reports it, so this page can be
			closed at any point.
		</p>
		{#if error}
			<div class="mt-3 text-sm text-red-700">{error}</div>
		{/if}
		<div class="mt-5 flex items-center gap-3 flex-wrap">
			{#if pending.AuthURL}
				<a
					class="btn-primary"
					href={pending.AuthURL}
					target="_blank"
					rel="noopener noreferrer">Open consent link</a
				>
			{/if}
			<button class="btn-secondary" onclick={finish} disabled={busy}>I've finished</button>
			<button class="btn-ghost" onclick={() => (pending = null)} disabled={busy}>
				Choose a different bank
			</button>
		</div>
	</div>
{:else if loadingProviders}
	<div class="card p-6 max-w-3xl mx-auto muted">Loading…</div>
{:else if providers.length > 0}
	<div class="card p-6 max-w-3xl mx-auto">
		<h2 class="content-section-title text-center">Select your account</h2>
		<p class="text-center muted text-sm mt-1">
			Search for your bank, then approve access at its own consent screen.
		</p>

		{#if providers.length > 1}
			<div class="mt-5 flex items-center justify-center gap-2 flex-wrap">
				{#each providers as p (p.Slug)}
					<button
						type="button"
						class="btn-secondary !py-1.5 !px-3 !text-xs capitalize {provider === p.Slug
							? '!border-brand-500 !text-brand-600'
							: ''}"
						onclick={() => selectProvider(p.Slug)}
					>
						{p.Slug}
					</button>
				{/each}
			</div>
		{/if}

		<div class="mt-5">
			{#if error}
				<div class="mb-3 text-sm text-red-700">{error}</div>
			{/if}
			<BankInstitutionPicker
				{provider}
				bind:country
				picksInstitution={providerPicksInstitution}
				onselect={startConnection}
			/>
		</div>
	</div>
{:else}
	<div class="card p-6 max-w-3xl mx-auto">
		<h2 class="content-section-title text-center">Select your account</h2>
		<p class="text-center muted text-sm mt-1">
			Search for banks, credit cards and payment providers.
		</p>
		<p class="text-center muted text-xs mt-1">
			No bank feed provider is configured on this server, so nothing here connects to a bank —
			pick one to add the account by hand.
		</p>

		<div class="mt-5 flex items-center gap-2">
			<div class="flex-1 relative">
				<input
					class="input pl-9"
					type="search"
					placeholder="Search"
					aria-label="Search banks"
					bind:value={brandQuery}
					autocomplete="off"
				/>
				<svg
					class="absolute top-2.5 left-2.5 h-4 w-4 text-ink-400"
					viewBox="0 0 20 20"
					fill="currentColor"
					aria-hidden="true"
				>
					<path
						fill-rule="evenodd"
						d="M9 3.5a5.5 5.5 0 1 0 3.316 9.85l3.667 3.667a.75.75 0 0 0 1.06-1.06l-3.667-3.667A5.5 5.5 0 0 0 9 3.5Zm-4 5.5a4 4 0 1 1 8 0 4 4 0 0 1-8 0Z"
						clip-rule="evenodd"
					/>
				</svg>
			</div>
		</div>

		<div class="mt-4 text-sm flex items-center gap-2">
			<span class="muted">Country:</span>
			<select class="select w-auto" bind:value={brandCountry} aria-label="Country">
				{#each BANK_COUNTRIES as c (c.code)}
					<option value={c.code}>{c.name}</option>
				{/each}
			</select>
		</div>

		{#if popular.length > 0}
			<h3 class="font-semibold mt-6 mb-3">
				Popular in {BANK_COUNTRIES.find((c) => c.code === brandCountry)?.name}
			</h3>
			<div class="grid gap-3 sm:grid-cols-2">
				{#each popular as b (b.id)}
					<button
						type="button"
						class="flex items-center gap-3 border border-ink-200 rounded-md p-3 hover:bg-ink-50 text-left transition"
						onclick={() => selectBrand(b)}
					>
						<span
							class="h-10 w-14 flex items-center justify-center rounded text-xs font-bold"
							style="background:{b.color}; color:{b.color.toLowerCase() === '#ffffff' ||
							b.color.toLowerCase() === '#f7f2ea' ||
							b.color.toLowerCase() === '#f3f4f6'
								? '#1f2937'
								: '#ffffff'}"
						>
							{b.initials}
						</span>
						<span class="text-sm text-ink-800">{b.name}</span>
					</button>
				{/each}
			</div>
		{/if}

		{#if others.length > 0}
			<h3 class="font-semibold mt-6 mb-3">All results</h3>
			<div class="grid gap-2 sm:grid-cols-2">
				{#each others as b (b.id)}
					<button
						type="button"
						class="flex items-center gap-3 border border-ink-200 rounded-md p-3 hover:bg-ink-50 text-left transition"
						onclick={() => selectBrand(b)}
					>
						<span
							class="h-9 w-12 flex items-center justify-center rounded text-xs font-bold"
							style="background:{b.color}; color:#fff"
						>
							{b.initials}
						</span>
						<span class="text-sm text-ink-800">{b.name}</span>
					</button>
				{/each}
			</div>
		{/if}

		{#if popular.length === 0 && others.length === 0}
			<p class="text-center muted mt-6">
				No banks found. Try a different search or
				<a href="/app/accounting/bank-accounts/new/manual" class="text-brand-600 hover:underline"
					>add without a feed</a
				>.
			</p>
		{/if}
	</div>
{/if}
