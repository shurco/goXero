<script lang="ts">
	import { page } from '$app/stores';
	import { onMount } from 'svelte';

	export interface DropdownItem {
		label: string;
		href?: string;
		external?: boolean;
		iconAfter?: 'external';
	}
	export interface DropdownGroup {
		title?: string;
		/** Extra classes for the title row (e.g. emphasis colour). */
		titleClass?: string;
		/** Small icon after the title (e.g. favourite star). */
		titleIcon?: 'star';
		items: DropdownItem[];
	}

	interface Props {
		label: string;
		/** Top-level link target (click → navigate). Usually the section overview page. */
		href: string;
		groups: DropdownGroup[];
		settingsHref?: string;
		settingsLabel?: string;
		/** Called against the current URL to decide if the root nav item is active. */
		isActive: (url: URL) => boolean;
	}

	let { label, href, groups, settingsHref, settingsLabel, isActive }: Props = $props();

	let open = $state(false);
	let root: HTMLElement | undefined = $state();
	let closeTimer: ReturnType<typeof setTimeout> | null = null;

	function show() {
		if (closeTimer) { clearTimeout(closeTimer); closeTimer = null; }
		open = true;
	}
	function scheduleHide() {
		if (closeTimer) clearTimeout(closeTimer);
		closeTimer = setTimeout(() => { open = false; closeTimer = null; }, 150);
	}
	function hideNow() {
		if (closeTimer) { clearTimeout(closeTimer); closeTimer = null; }
		open = false;
	}

	// Close on navigation.
	let prevPath = $state($page.url.pathname + $page.url.search);
	$effect(() => {
		const now = $page.url.pathname + $page.url.search;
		if (now !== prevPath) {
			hideNow();
			prevPath = now;
		}
	});

	function onDocClick(e: MouseEvent) {
		if (!open || !root) return;
		if (!root.contains(e.target as Node)) hideNow();
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') hideNow();
	}

	onMount(() => {
		document.addEventListener('mousedown', onDocClick);
		document.addEventListener('keydown', onKey);
		return () => {
			document.removeEventListener('mousedown', onDocClick);
			document.removeEventListener('keydown', onKey);
			if (closeTimer) clearTimeout(closeTimer);
		};
	});

	const active = $derived(isActive($page.url));

	function isItemActive(it: DropdownItem) {
		if (!it.href) return false;
		const itemURL = new URL(it.href, location.origin);
		if ($page.url.pathname !== itemURL.pathname) return false;
		const itemType = itemURL.searchParams.get('type');
		const pageType = $page.url.searchParams.get('type');
		return (itemType ?? '') === (pageType ?? '');
	}
</script>

<div
	class="relative"
	bind:this={root}
	onmouseenter={show}
	onmouseleave={scheduleHide}
	role="none"
>
	<a
		href={href}
		class="topbar-nav-item {active ? 'topbar-nav-item-active' : ''}"
		aria-haspopup="menu"
		aria-expanded={open}
		onfocus={show}
		onblur={scheduleHide}
	>
		{label}
	</a>

	{#if open}
		<div class="nav-dropdown" role="menu" tabindex="-1" onmouseenter={show} onmouseleave={scheduleHide}>
			{#each groups as group, gi}
				{#if group.title}
					<div
						class="nav-dropdown-group-title {group.titleClass ?? ''} {group.titleIcon ? 'flex items-center justify-between gap-2 pr-3' : ''}"
					>
						<span>{group.title}</span>
						{#if group.titleIcon === 'star'}
							<svg
								class="h-3.5 w-3.5 shrink-0 fill-current text-brand-600"
								viewBox="0 0 24 24"
								aria-hidden="true"
							>
								<path d="M12 2l2.4 7.4H22l-6 4.6 2.3 7L12 17.8 5.7 21l2.3-7-6-4.6h7.6L12 2z" />
							</svg>
						{/if}
					</div>
				{/if}
				{#each group.items as item}
					{#if item.href}
						<a
							href={item.href}
							target={item.external ? '_blank' : undefined}
							rel={item.external ? 'noopener noreferrer' : undefined}
							class="nav-dropdown-item {isItemActive(item) ? 'nav-dropdown-item-active' : ''}"
							role="menuitem"
							onclick={hideNow}
						>
							<span>{item.label}</span>
							{#if item.iconAfter === 'external' || item.external}
								<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current text-ink-400">
									<path d="M14 3h7v7h-2V6.4l-9.3 9.3-1.4-1.4L17.6 5H14V3zM5 5h6v2H7v10h10v-4h2v6H5V5z" />
								</svg>
							{/if}
						</a>
					{:else}
						<span class="nav-dropdown-item opacity-60">{item.label}</span>
					{/if}
				{/each}
				{#if gi < groups.length - 1}<div class="nav-dropdown-separator"></div>{/if}
			{/each}

			{#if settingsHref}
				<div class="nav-dropdown-separator"></div>
				<a href={settingsHref} class="nav-dropdown-item" role="menuitem" onclick={hideNow}>
					<span>{settingsLabel ?? `${label} settings`}</span>
					<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current text-ink-500">
						<path d="M19.4 13a7.9 7.9 0 0 0 0-2l2-1.6-2-3.5-2.4.8a8 8 0 0 0-1.7-1L14.8 3h-4l-.5 2.6a8 8 0 0 0-1.7 1L6.2 5.9l-2 3.5L6.2 11a7.9 7.9 0 0 0 0 2l-2 1.6 2 3.5 2.4-.8c.5.4 1.1.7 1.7 1l.5 2.7h4l.5-2.7c.6-.3 1.2-.6 1.7-1l2.4.8 2-3.5-2-1.6zM12 15.5A3.5 3.5 0 1 1 12 8.5a3.5 3.5 0 0 1 0 7z" />
					</svg>
				</a>
			{/if}
		</div>
	{/if}
</div>
