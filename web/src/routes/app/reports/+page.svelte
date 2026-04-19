<script lang="ts">
	import { browser } from '$app/environment';
	import { onMount } from 'svelte';
	import { REPORT_CATEGORIES, reportRowKey } from '$lib/reports-catalog';
	import {
		getReportRowByKey,
		isFavouriteReportKey,
		reportFavourites,
		toggleFavouriteReportKey
	} from '$lib/reports-favourites.svelte';

	type TabId = 'home' | 'custom' | 'drafts' | 'published' | 'archived';

	let tab = $state<TabId>('home');
	let showDescriptions = $state(false);
	let openMenuKey = $state<string | null>(null);

	const favouriteRows = $derived.by(() =>
		reportFavourites.keys
			.map((k) => getReportRowByKey(k))
			.filter((row): row is NonNullable<typeof row> => !!row)
	);

	function closeMenuOnOutsideClick(e: MouseEvent) {
		const t = e.target;
		if (!(t instanceof Node)) return;
		if ((t as HTMLElement).closest?.('.report-row-menu-root')) return;
		openMenuKey = null;
	}

	onMount(() => {
		document.addEventListener('click', closeMenuOnOutsideClick);
		return () => document.removeEventListener('click', closeMenuOnOutsideClick);
	});

	async function copyReportLink(href: string | null) {
		if (!href || !browser) return;
		const url = `${window.location.origin}${href}`;
		try {
			await navigator.clipboard.writeText(url);
		} catch {
			/* ignore */
		}
		openMenuKey = null;
	}
</script>

<div class="reports-hub space-y-8">
	<h1 class="section-title">Reports</h1>

	<nav class="reports-tabs flex flex-wrap gap-1 border-b border-ink-200" aria-label="Report views">
		{#each [{ id: 'home' as const, label: 'Home' }, { id: 'custom' as const, label: 'Custom' }, { id: 'drafts' as const, label: 'Drafts' }, { id: 'published' as const, label: 'Published' }, { id: 'archived' as const, label: 'Archived' }] as t (t.id)}
			<button
				type="button"
				class="reports-tab -mb-px border-b-2 px-4 py-2.5 text-sm font-medium transition {tab === t.id
					? 'border-brand-600 text-brand-700'
					: 'border-transparent text-ink-600 hover:text-ink-900'}"
				onclick={() => (tab = t.id)}
				aria-current={tab === t.id ? 'page' : undefined}
			>
				{t.label}
			</button>
		{/each}
	</nav>

	{#if tab === 'home'}
		<section class="space-y-8" aria-labelledby="favourites-heading">
			<div>
				<h2 id="favourites-heading" class="content-section-title mb-4">Favourites</h2>
				{#if favouriteRows.length === 0}
					<p class="muted text-sm">No favourites yet.</p>
				{:else}
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
						{#each favouriteRows as fav (fav.key)}
							<div
								class="report-fav-card flex items-start gap-3 rounded-lg border border-ink-100 bg-white p-4 shadow-card transition hover:border-brand-200 hover:shadow-pop"
							>
								{#if fav.href}
									<a href={fav.href} class="flex min-w-0 flex-1 items-start gap-3">
										<svg
											class="mt-0.5 h-5 w-5 shrink-0 fill-current text-brand-600"
											viewBox="0 0 24 24"
											aria-hidden="true"
										>
											<path d="M12 2l2.4 7.4H22l-6 4.6 2.3 7L12 17.8 5.7 21l2.3-7-6-4.6h7.6L12 2z" />
										</svg>
										<span class="text-sm font-medium text-brand-700 leading-snug">{fav.label}</span>
									</a>
								{:else}
									<div class="flex min-w-0 flex-1 items-start gap-3">
										<svg
											class="mt-0.5 h-5 w-5 shrink-0 fill-current text-brand-600"
											viewBox="0 0 24 24"
											aria-hidden="true"
										>
											<path d="M12 2l2.4 7.4H22l-6 4.6 2.3 7L12 17.8 5.7 21l2.3-7-6-4.6h7.6L12 2z" />
										</svg>
										<span class="text-sm font-medium text-ink-500 leading-snug">{fav.label}</span>
										<span class="text-[10px] font-semibold uppercase tracking-wide text-amber-700">Soon</span>
									</div>
								{/if}
								<button
									type="button"
									class="shrink-0 rounded p-1 text-ink-400 hover:bg-ink-100 hover:text-amber-700"
									aria-label="Remove {fav.label} from favourites"
									onclick={(e) => {
										e.preventDefault();
										toggleFavouriteReportKey(fav.key);
									}}
								>
									<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
										<path d="M12 2l2.4 7.4H22l-6 4.6 2.3 7L12 17.8 5.7 21l2.3-7-6-4.6h7.6L12 2z" />
									</svg>
								</button>
							</div>
						{/each}
					</div>
				{/if}
			</div>

			<div>
				<div
					class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
				>
					<h2 class="content-section-title min-w-0">All reports</h2>
					<div class="flex shrink-0 items-center justify-end gap-2 sm:justify-end">
						<label
							for="reports-show-desc"
							id="show-desc-label"
							class="cursor-pointer whitespace-nowrap text-sm text-ink-700"
						>
							Show descriptions
						</label>
						<button
							id="reports-show-desc"
							type="button"
							role="switch"
							aria-checked={showDescriptions}
							aria-labelledby="show-desc-label"
							class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer overflow-hidden rounded-full border transition-colors duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-2 {showDescriptions
								? 'border-brand-600 bg-brand-500'
								: 'border-ink-200 bg-ink-200'}"
							onclick={() => (showDescriptions = !showDescriptions)}
						>
							<span
								class="pointer-events-none absolute top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-[left,right] duration-200 ease-out {showDescriptions
									? 'right-0.5 left-auto'
									: 'left-0.5'}"
								aria-hidden="true"
							></span>
						</button>
					</div>
				</div>

				<div class="space-y-3">
					{#each REPORT_CATEGORIES as cat (cat.id)}
						<details class="report-category group rounded-lg border border-ink-100 bg-white shadow-card" open>
							<summary
								class="flex cursor-pointer list-none flex-col gap-1 px-4 py-3 marker:content-none [&::-webkit-details-marker]:hidden"
							>
								<div class="flex items-center gap-2 font-semibold text-ink-900">
									<svg
										class="h-4 w-4 shrink-0 text-ink-500 transition group-open:rotate-180"
										viewBox="0 0 24 24"
										fill="none"
										stroke="currentColor"
										stroke-width="2"
										aria-hidden="true"
									>
										<path d="M6 9l6 6 6-6" stroke-linecap="round" stroke-linejoin="round" />
									</svg>
									{cat.title}
								</div>
								{#if cat.subtitle}
									<p class="pl-6 text-sm font-normal leading-snug text-ink-500">{cat.subtitle}</p>
								{/if}
							</summary>
							<div class="border-t border-ink-100 px-2 py-2">
								<div class="grid grid-cols-1 gap-0 md:grid-cols-2">
									{#each cat.reports as entry (cat.id + entry.label)}
										{@const rowKey = reportRowKey(cat.id, entry.label)}
										<div
											class="report-all-row flex items-start gap-2 border-b border-ink-50 py-2.5 pl-2 pr-1 md:border-b-0 md:odd:border-r md:odd:border-ink-50"
										>
											<button
												type="button"
												class="mt-0.5 shrink-0 text-ink-300 hover:text-brand-500"
												aria-label={isFavouriteReportKey(rowKey) ? `Remove ${entry.label} from favourites` : `Add ${entry.label} to favourites`}
												onclick={(e) => {
													e.preventDefault();
													e.stopPropagation();
													toggleFavouriteReportKey(rowKey);
												}}
											>
												{#if isFavouriteReportKey(rowKey)}
													<svg class="h-4 w-4 fill-current text-brand-600" viewBox="0 0 24 24" aria-hidden="true">
														<path d="M12 2l2.4 7.4H22l-6 4.6 2.3 7L12 17.8 5.7 21l2.3-7-6-4.6h7.6L12 2z" />
													</svg>
												{:else}
													<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
														<path d="M12 2l2.4 7.4H22l-6 4.6 2.3 7L12 17.8 5.7 21l2.3-7-6-4.6h7.6L12 2z" />
													</svg>
												{/if}
											</button>
											<div class="min-w-0 flex-1">
												{#if entry.href}
													<a href={entry.href} class="text-sm font-medium text-brand-700 hover:underline">
														{entry.label}
													</a>
												{:else}
													<span class="text-sm font-medium text-ink-500">{entry.label}</span>
													<span class="ml-2 text-[10px] font-semibold uppercase tracking-wide text-amber-700">Soon</span>
												{/if}
												{#if showDescriptions}
													<p class="mt-0.5 text-xs leading-snug text-ink-500">
														{entry.description}
													</p>
												{/if}
											</div>
											<div class="report-row-menu-root relative shrink-0">
												<button
													type="button"
													class="rounded p-1 text-ink-400 hover:bg-ink-100 hover:text-ink-600"
													aria-label="More actions for {entry.label}"
													aria-expanded={openMenuKey === rowKey}
													aria-haspopup="menu"
													onclick={(e) => {
														e.preventDefault();
														e.stopPropagation();
														openMenuKey = openMenuKey === rowKey ? null : rowKey;
													}}
												>
													<svg class="h-4 w-4" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
														<circle cx="12" cy="6" r="1.5" />
														<circle cx="12" cy="12" r="1.5" />
														<circle cx="12" cy="18" r="1.5" />
													</svg>
												</button>
												{#if openMenuKey === rowKey}
													<ul
														class="absolute right-0 top-full z-30 mt-1 min-w-[11rem] rounded-md border border-ink-200 bg-white py-1 text-left shadow-pop"
														role="menu"
														aria-label="Actions for {entry.label}"
													>
														{#if entry.href}
															<li role="none">
																<a
																	href={entry.href}
																	class="block px-3 py-2 text-sm text-ink-800 hover:bg-ink-50"
																	role="menuitem"
																	onclick={() => (openMenuKey = null)}
																>
																	Open report
																</a>
															</li>
														{/if}
														<li role="none">
															<button
																type="button"
																class="block w-full px-3 py-2 text-left text-sm text-ink-800 hover:bg-ink-50"
																role="menuitem"
																onclick={() => {
																	toggleFavouriteReportKey(rowKey);
																	openMenuKey = null;
																}}
															>
																{isFavouriteReportKey(rowKey) ? 'Remove from favourites' : 'Add to favourites'}
															</button>
														</li>
														{#if entry.href}
															<li role="none">
																<button
																	type="button"
																	class="block w-full px-3 py-2 text-left text-sm text-ink-800 hover:bg-ink-50"
																	role="menuitem"
																	onclick={() => copyReportLink(entry.href)}
																>
																	Copy link
																</button>
															</li>
														{/if}
													</ul>
												{/if}
											</div>
										</div>
									{/each}
								</div>
							</div>
						</details>
					{/each}
				</div>
			</div>
		</section>
	{:else}
		<div class="card p-10 text-center text-ink-600">
			<p class="font-medium text-ink-800">{tab === 'custom' ? 'Custom' : tab === 'drafts' ? 'Drafts' : tab === 'published' ? 'Published' : 'Archived'} reports</p>
			<p class="mt-2 text-sm">This view is not available yet. Use <strong>Home</strong> for standard reports.</p>
		</div>
	{/if}
</div>
