<script lang="ts">
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { session } from '$lib/stores/session';
	import { authApi } from '$lib/api';
	import NavDropdown from './NavDropdown.svelte';
	import CreateOrganisationModal from './CreateOrganisationModal.svelte';
	import type { DropdownGroup } from './NavDropdown.svelte';
	import type { TenantSummary } from '$lib/types';
	import { getReportRowByKey, reportFavourites } from '$lib/reports-favourites.svelte';
	import type { FlatReportRow } from '$lib/reports-catalog';

	let tenantMenu = $state(false);
	let userMenu = $state(false);
	let quickAdd = $state(false);
	let showCreateOrg = $state(false);

	const salesGroups: DropdownGroup[] = [
		{
			items: [
				{ label: 'Sales overview', href: '/app/sales' },
				{ label: 'Invoices', href: '/app/invoices' },
				{ label: 'Online payments', href: '/app/payments' },
				{ label: 'Quotes', href: '/app/sales/quotes' },
				{ label: 'Products and services', href: '/app/items' },
				{ label: 'Customers', href: '/app/contacts?type=customer' }
			]
		}
	];

	const purchasesGroups: DropdownGroup[] = [
		{
			items: [
				{ label: 'Purchases overview', href: '/app/purchases' },
				{ label: 'Bills', href: '/app/purchases/bills' },
				{ label: 'Online bill payments', href: '/app/payments?type=ACCPAY' },
				{ label: 'Purchase orders', href: '/app/purchases/orders' },
				{ label: 'Cheques', href: '/app/purchases/cheques' },
				{ label: 'Suppliers', href: '/app/contacts?type=supplier' }
			]
		}
	];

	const reportingFavouriteItems = $derived.by(() =>
		reportFavourites.keys
			.map((k) => getReportRowByKey(k))
			.filter((r): r is FlatReportRow => !!r && r.href != null)
			.map((r) => ({ label: r.label, href: r.href! }))
	);

	const reportingGroups = $derived.by((): DropdownGroup[] => [
		{ items: [{ label: 'All reports', href: '/app/reports' }] },
		...(reportingFavouriteItems.length > 0
			? [
					{
						title: 'Favourite reports',
						titleClass: 'nav-dropdown-group-title-favourite',
						titleIcon: 'star' as const,
						items: reportingFavouriteItems
					}
				]
			: []),
		{
			items: [
				{ label: 'Short-term cash flow', href: '/app/reports/short-term-cash-flow' },
				{ label: 'Business snapshot', href: '/app/reports/business-snapshot' }
			]
		}
	]);

	const accountingGroups: DropdownGroup[] = [
		{
			title: 'Banking',
			items: [
				{ label: 'Bank accounts', href: '/app/accounting/bank-accounts' },
				{ label: 'Bank rules', href: '/app/accounting/bank-rules' }
			]
		},
		{
			title: 'Accounting tools',
			items: [
				{ label: 'Chart of accounts', href: '/app/accounts' },
				{ label: 'Fixed assets', href: '/app/accounting/fixed-assets' },
				{ label: 'Manual journals', href: '/app/accounting/manual-journals' },
				{ label: 'Find and recode', href: '/app/accounting/find-and-recode' },
				{ label: 'Assurance dashboard', href: '/app/accounting/assurance' },
				{ label: 'History and notes', href: '/app/accounting/history-and-notes' }
			]
		}
	];

	const taxGroups: DropdownGroup[] = [
		{ items: [{ label: 'Sales tax', href: '/app/tax/sales-tax' }, { label: '1099', href: '/app/tax/1099' }] }
	];

	const contactsGroups: DropdownGroup[] = [
		{
			items: [
				{ label: 'All contacts', href: '/app/contacts' },
				{ label: 'Customers', href: '/app/contacts?type=customer' },
				{ label: 'Suppliers', href: '/app/contacts?type=supplier' }
			]
		}
	];

	// Active-state matchers for each top-level item. Must return TRUE when the
	// given URL belongs under that section, FALSE otherwise. Matchers are
	// mutually exclusive (first match wins conceptually).

	const onHome = $derived($page.url.pathname === '/app');

	function purchasesActive(u: URL) {
		if (u.pathname.startsWith('/app/purchases')) return true;
		if (u.pathname === '/app/invoices' && u.searchParams.get('type') === 'ACCPAY') return true;
		if (u.pathname.startsWith('/app/invoices/') && u.searchParams.get('type') === 'ACCPAY') return true;
		if (u.pathname === '/app/payments' && u.searchParams.get('type') === 'ACCPAY') return true;
		if (u.pathname === '/app/contacts' && u.searchParams.get('type') === 'supplier') return true;
		return false;
	}
	function salesActive(u: URL) {
		if (purchasesActive(u)) return false;
		if (u.pathname.startsWith('/app/sales')) return true;
		if (u.pathname.startsWith('/app/invoices')) return true;
		if (u.pathname.startsWith('/app/items')) return true;
		if (u.pathname.startsWith('/app/payments')) return true;
		return false;
	}
	function reportingActive(u: URL) {
		return u.pathname.startsWith('/app/reports');
	}
	function accountingActive(u: URL) {
		if (u.pathname.startsWith('/app/accounting')) return true;
		if (u.pathname.startsWith('/app/accounts')) return true;
		if (u.pathname.startsWith('/app/bank-feeds')) return true;
		return false;
	}
	function taxActive(u: URL) {
		return u.pathname.startsWith('/app/tax');
	}
	function contactsActive(u: URL) {
		if (!u.pathname.startsWith('/app/contacts')) return false;
		const t = u.searchParams.get('type');
		// Contacts filtered by supplier → shown under Purchases instead.
		if (t === 'supplier') return false;
		return true;
	}

	const currentTenant = $derived(
		$session.tenants.find((t) => t.organisationId === $session.tenantId)
	);

	const userDisplayName = $derived(
		[$session.firstName, $session.lastName].filter(Boolean).join(' ').trim() ||
			$session.email?.split('@')[0] ||
			'User'
	);

	const orgSettingsActive = $derived($page.url.pathname.startsWith('/app/settings'));
	const orgFilesActive = $derived($page.url.pathname.startsWith('/app/files'));

	function orgInitials(name: string | undefined) {
		if (!name?.trim()) return '?';
		const parts = name.trim().split(/\s+/).filter(Boolean);
		if (parts.length >= 2)
			return (parts[0][0] + parts[parts.length - 1]![0]).toUpperCase();
		return name.slice(0, 3).toUpperCase();
	}

	function orgAvatarColor(id: string) {
		let h = 0;
		for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
		return `hsl(${h % 360} 52% 46%)`;
	}

	function pickTenant(t: TenantSummary) {
		session.setTenant(t.organisationId);
		tenantMenu = false;
		location.reload();
	}

	async function logout() {
		// Revoke the refresh token server-side so a stolen copy can't be
		// reused. Failure is non-fatal — we still clear the local session.
		const refreshToken = $session.refreshToken;
		try {
			await authApi.logout({ refreshToken });
		} catch {
			// ignore — local logout still proceeds
		}
		session.logout();
		goto('/login');
	}

	// Close menus on navigation or outside click.
	let userMenuRoot: HTMLElement | undefined = $state();
	let tenantMenuRoot: HTMLElement | undefined = $state();
	let quickAddRoot: HTMLElement | undefined = $state();

	function onDocClick(e: MouseEvent) {
		const t = e.target as Node;
		if (userMenuRoot && !userMenuRoot.contains(t)) userMenu = false;
		if (tenantMenuRoot && !tenantMenuRoot.contains(t)) tenantMenu = false;
		if (quickAddRoot && !quickAddRoot.contains(t)) quickAdd = false;
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			userMenu = false;
			tenantMenu = false;
			quickAdd = false;
		}
	}
	onMount(() => {
		document.addEventListener('mousedown', onDocClick);
		document.addEventListener('keydown', onKey);
		return () => {
			document.removeEventListener('mousedown', onDocClick);
			document.removeEventListener('keydown', onKey);
		};
	});

	let prevPath = $state($page.url.pathname);
	$effect(() => {
		if ($page.url.pathname !== prevPath) {
			userMenu = false;
			tenantMenu = false;
			quickAdd = false;
			prevPath = $page.url.pathname;
		}
	});
</script>

<header class="topbar">
	<div class="mx-auto flex h-14 items-center gap-1 px-4 lg:px-6">
		<!-- Logo + org picker -->
		<div class="relative flex items-center gap-2 pr-3" bind:this={tenantMenuRoot}>
			<a href="/app" class="flex h-9 w-9 items-center justify-center" aria-label="goXero home">
				<svg viewBox="0 0 32 32" class="h-7 w-7">
					<circle cx="16" cy="16" r="14" fill="#ffffff" />
					<path d="M10 10l12 12M22 10l-12 12" stroke="#2c6cb0" stroke-width="3" stroke-linecap="round" />
				</svg>
			</a>

			<button
				type="button"
				class="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 text-sm font-semibold text-white hover:bg-white/10"
				aria-haspopup="menu"
				aria-expanded={tenantMenu}
				onclick={() => (tenantMenu = !tenantMenu)}
			>
				<span class="max-w-[220px] truncate text-left">
					{currentTenant?.name ?? 'Choose org'}
					{#if currentTenant?.baseCurrency}
						<span class="font-normal opacity-90"> ({currentTenant.baseCurrency})</span>
					{/if}
				</span>
				<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current"><path d="M7 10l5 5 5-5z" /></svg>
			</button>
			{#if tenantMenu}
				<div
					class="absolute left-11 top-12 w-[min(100vw-2rem,300px)] rounded-xl bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-50"
					role="menu"
				>
					{#if currentTenant}
						<div class="px-4 pt-3 pb-2 flex gap-3 items-start">
							<div
								class="h-10 w-10 shrink-0 rounded flex items-center justify-center text-xs font-bold text-white shadow-sm"
								style="background-color: {orgAvatarColor(currentTenant.organisationId)}"
								aria-hidden="true"
							>
								{orgInitials(currentTenant.name)}
							</div>
							<div class="min-w-0 flex-1">
								<p class="font-semibold text-ink-900 text-sm leading-snug break-words">
									{currentTenant.name}
								</p>
								{#if currentTenant.baseCurrency}
									<p class="text-xs text-ink-500 mt-0.5">{currentTenant.baseCurrency}</p>
								{/if}
							</div>
						</div>
						<div class="px-2 pb-1 space-y-0.5">
							<a
								href="/app/files"
								class="topbar-org-menu-row {orgFilesActive ? 'topbar-org-menu-row-active' : ''}"
								onclick={() => (tenantMenu = false)}
								role="menuitem"
							>
								<svg viewBox="0 0 24 24" class="h-5 w-5 shrink-0 text-ink-500" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
									<path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								<span>Files</span>
							</a>
							<a
								href="/app/settings"
								class="topbar-org-menu-row {orgSettingsActive ? 'topbar-org-menu-row-active' : ''}"
								onclick={() => (tenantMenu = false)}
								role="menuitem"
							>
								<svg viewBox="0 0 24 24" class="h-5 w-5 shrink-0 text-ink-500" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
									<path d="M12 15.5a3.5 3.5 0 100-7 3.5 3.5 0 000 7z" stroke-linecap="round" />
									<path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.6a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001.51 1 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								<span>Settings</span>
							</a>
						</div>
					{:else}
						<p class="px-4 pt-3 pb-2 text-sm text-ink-600">Select an organisation to open Files and Settings.</p>
					{/if}

					<div class="nav-dropdown-separator"></div>
					<div class="px-4 pt-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-ink-500">
						Organisation
					</div>
					{#each $session.tenants as t}
						<button
							type="button"
							class="w-full text-left px-4 py-2 text-sm hover:bg-ink-50 flex items-center gap-3"
							onclick={() => pickTenant(t)}
							role="menuitem"
						>
							<div
								class="h-8 w-8 shrink-0 rounded flex items-center justify-center text-[10px] font-bold text-white"
								style="background-color: {orgAvatarColor(t.organisationId)}"
								aria-hidden="true"
							>
								{orgInitials(t.name)}
							</div>
							<span class="truncate flex-1">{t.name}</span>
							{#if t.organisationId === $session.tenantId}
								<span class="text-brand-600 text-xs shrink-0" aria-label="Current">✓</span>
							{/if}
						</button>
					{/each}
					{#if $session.tenants.length === 0}
						<div class="px-4 py-2 text-sm text-ink-500">No organisations yet.</div>
					{/if}
					<button
						type="button"
						class="w-full text-left px-4 py-2.5 text-sm text-ink-800 hover:bg-ink-50 flex items-center gap-3"
						onclick={() => {
							tenantMenu = false;
							showCreateOrg = true;
						}}
						role="menuitem"
					>
						<span class="inline-flex h-8 w-8 items-center justify-center rounded border border-ink-200 bg-ink-50 text-ink-600 font-semibold" aria-hidden="true">+</span>
						<span class="font-medium">Add new organisation</span>
					</button>
				</div>
			{/if}
		</div>

		<!-- Primary nav -->
		<nav class="flex items-center gap-[5px]">
			<a
				href="/app"
				class="topbar-nav-item {onHome ? 'topbar-nav-item-active' : ''}"
			>
				Home
			</a>
			<NavDropdown
				label="Sales"
				href="/app/sales"
				groups={salesGroups}
				isActive={salesActive}
			/>
			<NavDropdown
				label="Purchases"
				href="/app/purchases"
				groups={purchasesGroups}
				isActive={purchasesActive}
			/>
			<NavDropdown
				label="Reporting"
				href="/app/reports"
				groups={reportingGroups}
				isActive={reportingActive}
			/>
			<NavDropdown
				label="Accounting"
				href="/app/accounting"
				groups={accountingGroups}
				isActive={accountingActive}
			/>
			<NavDropdown
				label="Tax"
				href="/app/tax"
				groups={taxGroups}
				isActive={taxActive}
			/>
			<NavDropdown
				label="Contacts"
				href="/app/contacts"
				groups={contactsGroups}
				isActive={contactsActive}
			/>
		</nav>

		<div class="ml-auto flex items-center gap-1">
			<!-- Quick add -->
			<div class="relative" bind:this={quickAddRoot}>
				<button
					type="button"
					class="topbar-icon-btn"
					aria-label="Create new"
					aria-haspopup="menu"
					aria-expanded={quickAdd}
					onclick={() => (quickAdd = !quickAdd)}
				>
					<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M19 11h-6V5h-2v6H5v2h6v6h2v-6h6z" /></svg>
				</button>
				{#if quickAdd}
					<div
						class="absolute right-0 mt-1 w-[min(100vw-2rem,280px)] rounded-xl bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-50"
						role="menu"
						aria-label="Create"
					>
						<button
							type="button"
							class="nav-dropdown-item w-full text-left border-0 bg-transparent font-inherit"
							role="menuitem"
							onclick={() => {
								quickAdd = false;
								showCreateOrg = true;
							}}
						>
							New organisation
						</button>

						<div class="nav-dropdown-separator"></div>

						<div class="nav-dropdown-group-title !pt-1">Create new</div>

						<a
							class="nav-dropdown-item"
							href="/app/invoices/new"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Invoice
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/purchases/bills/new"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Bill
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/contacts/new"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Contact
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/sales/quotes/new"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Quote
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/purchases/orders/new"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Purchase order
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/accounting/manual-journals"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Manual journal
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/bank-transactions"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Spend money
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/bank-transactions"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Receive money
						</a>
						<a
							class="nav-dropdown-item"
							href="/app/bank-transactions"
							role="menuitem"
							onclick={() => (quickAdd = false)}
						>
							Transfer money
						</a>
					</div>
				{/if}
			</div>
			<button type="button" class="topbar-icon-btn" aria-label="Search">
				<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M15.5 14h-.8l-.3-.3a6.5 6.5 0 1 0-.7.7l.3.3v.8l5 5 1.5-1.5-5-5zm-6 0a4.5 4.5 0 1 1 0-9 4.5 4.5 0 0 1 0 9z" /></svg>
			</button>

			<div class="relative" bind:this={userMenuRoot}>
				<button
					type="button"
					class="ml-1 flex h-9 w-9 items-center justify-center rounded-full bg-white/15 text-sm font-semibold hover:bg-white/25"
					onclick={() => (userMenu = !userMenu)}
					aria-haspopup="menu"
					aria-expanded={userMenu}
				>
					{($session.firstName || $session.email || '?')[0].toUpperCase()}
				</button>
				{#if userMenu}
					<div
						class="absolute right-0 mt-1 w-[min(100vw-2rem,280px)] rounded-xl bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-50"
						role="menu"
					>
						<div class="px-4 pt-3 pb-2 text-xs font-bold uppercase tracking-wide text-brand-900">
							{userDisplayName}
						</div>
						<div class="px-2 pb-1 space-y-0.5">
							<a
								href="/app/profile"
								class="topbar-user-menu-row"
								onclick={() => (userMenu = false)}
								role="menuitem"
							>
								<svg viewBox="0 0 24 24" class="h-5 w-5 shrink-0 text-ink-500" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
									<path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2M12 11a4 4 0 100-8 4 4 0 000 8z" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								<span>Profile</span>
							</a>
							<a
								href="/app/account"
								class="topbar-user-menu-row"
								onclick={() => (userMenu = false)}
								role="menuitem"
							>
								<svg viewBox="0 0 24 24" class="h-5 w-5 shrink-0 text-ink-500" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
									<path d="M12 15.5a3.5 3.5 0 100-7 3.5 3.5 0 000 7z" stroke-linecap="round" />
									<path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.6a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001.51 1 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z" stroke-linecap="round" stroke-linejoin="round" />
								</svg>
								<span>Account</span>
							</a>
						</div>
						<div class="nav-dropdown-separator"></div>
						<button
							type="button"
							class="topbar-user-menu-row w-full text-left"
							onclick={logout}
							role="menuitem"
						>
							<svg viewBox="0 0 24 24" class="h-5 w-5 shrink-0 text-ink-500" fill="none" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
								<path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4M16 17l5-5-5-5M21 12H9" stroke-linecap="round" stroke-linejoin="round" />
							</svg>
							<span>Log out</span>
						</button>
					</div>
				{/if}
			</div>
		</div>
	</div>
</header>

<CreateOrganisationModal bind:open={showCreateOrg} />
