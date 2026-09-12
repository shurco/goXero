<script module lang="ts">
	/**
	 * The country a search starts in. Plaid serves US/CA and falls back to US
	 * when it is handed nothing, so showing that in the box is the scope of the
	 * search made visible rather than implied. Every other provider is asked
	 * with whatever the user typed.
	 */
	export function defaultCountry(provider: string) {
		return provider === 'plaid' ? 'US' : '';
	}
</script>

<script lang="ts">
	import { onDestroy } from 'svelte';
	import { bankFeedApi, type BankFeedInstitution } from '$lib/api';

	interface Props {
		/** Provider slug the search runs against. */
		provider: string;
		/** Country the institutions are searched under — Plaid rejects a mismatch. */
		country?: string;
		/** Whether the provider's own consent flow can ask for the bank itself. */
		picksInstitution?: boolean;
		/**
		 * The chosen institution, or nothing when the user asked the provider to
		 * choose. Both are a valid way to start a consent.
		 */
		onselect: (inst?: BankFeedInstitution) => void | Promise<void>;
	}

	let {
		provider,
		country = $bindable(''),
		picksInstitution = false,
		onselect
	}: Props = $props();

	let query = $state('');
	let institutions = $state<BankFeedInstitution[]>([]);
	let error = $state('');
	let loading = $state(false);

	/** Debounce handle for the typing — the query may go upstream. */
	let timer: ReturnType<typeof setTimeout> | null = null;
	/**
	 * Search generation. Answers can arrive out of order — a slow query for "ba"
	 * landing after a fast one for "bank" — and the list must show the newest
	 * question's answer, not the last one to come back.
	 */
	let generation = 0;
	/** The scope the list on screen was fetched for. */
	let scope = '';

	/**
	 * Ask on a timer rather than straight away: the pending call is what keeps
	 * `query` out of the reactive scope below, so typing debounces instead of
	 * re-querying on every keystroke.
	 */
	function schedule(delay: number, clear: boolean) {
		if (timer) clearTimeout(timer);
		timer = setTimeout(() => {
			timer = null;
			void search(clear);
		}, delay);
	}

	async function search(clear: boolean) {
		if (!provider) return;
		const gen = ++generation;
		const q = query;
		if (clear) institutions = [];
		loading = true;
		try {
			const res = await bankFeedApi.institutions(provider, country, q);
			if (gen !== generation) return;
			institutions = res.Institutions ?? [];
			error = '';
		} catch (e) {
			if (gen !== generation) return;
			error = e instanceof Error ? e.message : 'Could not search banks.';
		} finally {
			if (gen === generation) loading = false;
		}
	}

	/**
	 * A new provider or country is a new question, asked at once and with the
	 * previous answer dropped: a list of US banks under a GB heading is worse
	 * than an empty one, and the old rows are still clickable.
	 */
	$effect(() => {
		const next = JSON.stringify([provider, country]);
		if (next === scope) return;
		scope = next;
		schedule(0, true);
	});

	// The page can be navigated away from mid-search; a timer left running would
	// fire against a component that no longer exists.
	onDestroy(() => {
		if (timer) clearTimeout(timer);
	});

	function onQueryInput(value: string) {
		query = value;
		schedule(250, false);
	}
</script>

<div class="flex flex-wrap items-center gap-2">
	<div class="relative min-w-48 flex-1">
		<input
			class="input pl-9"
			type="search"
			placeholder="Search banks…"
			aria-label="Search banks"
			autocomplete="off"
			value={query}
			oninput={(e) => onQueryInput(e.currentTarget.value)}
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
	<input
		class="input w-28 shrink-0"
		type="text"
		placeholder="Country"
		aria-label="Country code"
		autocomplete="off"
		maxlength="2"
		value={country}
		oninput={(e) => (country = e.currentTarget.value.trim().toUpperCase())}
	/>
</div>

{#if error}
	<div class="mt-3 text-sm text-red-700">{error}</div>
{/if}

<!-- The list scrolls rather than the page, so the search box stays put while
     the results are paged through. -->
<div class="mt-2 max-h-[46vh] overflow-y-auto">
	{#each institutions as inst (inst.ID)}
		<button
			type="button"
			class="w-full flex items-center gap-3 p-3 rounded-md hover:bg-ink-50 text-left"
			onclick={() => onselect(inst)}
		>
			{#if inst.LogoURL}
				<img src={inst.LogoURL} alt="" class="h-8 w-8 rounded object-contain bg-ink-100" />
			{:else}
				<div class="h-8 w-8 rounded bg-ink-100"></div>
			{/if}
			<div class="min-w-0">
				<div class="font-medium truncate">{inst.Name}</div>
				<div class="text-xs muted">{(inst.Countries ?? []).join(', ')}</div>
			</div>
		</button>
	{/each}

	{#if loading && institutions.length === 0}
		<div class="p-6 muted text-center text-sm">Searching…</div>
	{:else if institutions.length === 0}
		<div class="p-6 muted text-center text-sm">
			{#if error}
				Nothing to show.
			{:else if picksInstitution}
				No banks found. Try a different search, or let {provider} ask for it below.
			{:else}
				No banks found. Try a different search.
			{/if}
		</div>
	{/if}
</div>

{#if picksInstitution}
	<div class="mt-3 border-t border-ink-100 pt-3 flex items-center justify-between gap-3 flex-wrap">
		<div class="text-xs muted">
			{provider} opens its own window and can ask for the bank itself.
		</div>
		<button
			type="button"
			class="btn-secondary !py-1.5 !px-3 !text-xs whitespace-nowrap"
			onclick={() => onselect()}
		>
			Let {provider} choose
		</button>
	</div>
{/if}
