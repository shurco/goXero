<script lang="ts">
	import { goto } from '$app/navigation';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import { BANK_BRANDS, BANK_COUNTRIES, type BankBrand } from '$lib/bank-brand';

	let query = $state('');
	let country = $state('US');

	const filtered = $derived(() => {
		const q = query.trim().toLowerCase();
		return BANK_BRANDS.filter((b) => {
			if (country && b.country !== country) return false;
			if (!q) return true;
			return b.name.toLowerCase().includes(q);
		});
	});

	const popular = $derived(filtered().filter((b) => b.popular));
	const others = $derived(filtered().filter((b) => !b.popular));

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

<p class="text-sm mb-4">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
</p>

<div class="card p-6 max-w-3xl mx-auto">
	<h2 class="content-section-title text-center">Select your account</h2>
	<p class="text-center muted text-sm mt-1">
		Search for banks, credit cards and payment providers.
	</p>

	<div class="mt-5 flex items-center gap-2">
		<div class="flex-1 relative">
			<input
				class="input pl-9"
				type="search"
				placeholder="Search"
				bind:value={query}
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
		<select class="select w-auto" bind:value={country}>
			{#each BANK_COUNTRIES as c (c.code)}
				<option value={c.code}>{c.name}</option>
			{/each}
		</select>
	</div>

	{#if popular.length > 0}
		<h3 class="font-semibold mt-6 mb-3">Popular in {BANK_COUNTRIES.find((c) => c.code === country)?.name}</h3>
		<div class="grid gap-3 sm:grid-cols-2">
			{#each popular as b (b.id)}
				<button
					type="button"
					class="flex items-center gap-3 border border-ink-200 rounded-md p-3 hover:bg-ink-50 text-left transition"
					onclick={() => selectBrand(b)}
				>
					<span
						class="h-10 w-14 flex items-center justify-center rounded text-xs font-bold"
						style="background:{b.color}; color:{b.color.toLowerCase() === '#ffffff' || b.color.toLowerCase() === '#f7f2ea' || b.color.toLowerCase() === '#f3f4f6' ? '#1f2937' : '#ffffff'}"
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
			<a href="/app/accounting/bank-accounts/new/manual" class="text-brand-600 hover:underline">add without a feed</a>.
		</p>
	{/if}
</div>
