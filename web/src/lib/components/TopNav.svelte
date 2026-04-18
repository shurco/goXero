<script lang="ts">
	import { page } from '$app/stores';
	import { goto, invalidateAll } from '$app/navigation';
	import { onMount } from 'svelte';
	import { session } from '$lib/stores/session';
	import { authApi, orgApi } from '$lib/api';
	import NavDropdown from './NavDropdown.svelte';
	import type { DropdownGroup } from './NavDropdown.svelte';
	import type { TenantSummary } from '$lib/types';

	let tenantMenu = $state(false);
	let userMenu = $state(false);
	let quickAdd = $state(false);
	let showCreateOrg = $state(false);

	let newOrg = $state({ name: '', countryCode: '', baseCurrency: 'USD' });
	let creatingOrg = $state(false);
	let createError = $state('');

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

	const reportingGroups: DropdownGroup[] = [
		{ items: [{ label: 'All reports', href: '/app/reports' }] },
		{
			title: 'Analytics',
			items: [
				{ label: 'Dashboards', href: '/app/reports/dashboards', iconAfter: 'external' },
				{ label: 'Cash flow manager', href: '/app/reports/cash-flow' },
				{ label: 'Visualisations', href: '/app/reports/visualisations', iconAfter: 'external' },
				{ label: 'Business health scorecard', href: '/app/reports/health', iconAfter: 'external' }
			]
		},
		{
			title: 'Favourite reports',
			items: [
				{ label: 'Account Transactions', href: '/app/reports/account-transactions' },
				{ label: 'Balance Sheet', href: '/app/reports/balance-sheet' },
				{ label: 'General Ledger Detail', href: '/app/reports/general-ledger-detail' },
				{ label: 'General Ledger Summary', href: '/app/reports/general-ledger' },
				{ label: 'Income Statement (Profit and Loss)', href: '/app/reports/profit-and-loss' },
				{ label: 'Sales Tax Report', href: '/app/reports/sales-tax' }
			]
		},
		{
			items: [
				{ label: 'Short-term cash flow', href: '/app/reports/short-term-cash-flow' },
				{ label: 'Business snapshot', href: '/app/reports/business-snapshot' }
			]
		}
	];

	const accountingGroups: DropdownGroup[] = [
		{
			title: 'Banking',
			items: [
				{ label: 'Bank accounts', href: '/app/accounting/bank-accounts' },
				{ label: 'Bank feeds', href: '/app/bank-feeds' },
				{ label: 'Bank rules', href: '/app/accounting/bank-rules' }
			]
		},
		{
			title: 'Accounting tools',
			items: [
				{ label: 'Chart of accounts', href: '/app/accounts' },
				{ label: 'Manual journals', href: '/app/accounting/manual-journals' },
				{ label: 'Fixed assets', href: '/app/accounting/fixed-assets' }
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

	async function createOrg() {
		creatingOrg = true;
		createError = '';
		try {
			await orgApi.create(newOrg);
			// reload tenants and switch to the new one
			const mine = await orgApi.mine();
			const created = mine.organisations?.find((o) => o.Name === newOrg.name);
			if (created) {
				session.updateTenants(
					mine.organisations.map((o) => ({
						organisationId: o.OrganisationID,
						name: o.Name,
						baseCurrency: o.BaseCurrency
					}))
				);
				session.setTenant(created.OrganisationID);
				location.href = '/app';
				return;
			}
			await invalidateAll();
			showCreateOrg = false;
		} catch (e) {
			createError = (e as Error).message;
		} finally {
			creatingOrg = false;
		}
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
				<span class="max-w-[200px] truncate">{currentTenant?.name ?? 'Choose org'}</span>
				<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current"><path d="M7 10l5 5 5-5z" /></svg>
			</button>
			{#if tenantMenu}
				<div class="absolute left-11 top-12 min-w-[260px] rounded-lg bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-50">
					<div class="px-4 pt-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-ink-500">My organisations</div>
					{#each $session.tenants as t}
						<button
							type="button"
							class="w-full text-left px-4 py-2 text-sm hover:bg-ink-50 flex items-center justify-between"
							onclick={() => pickTenant(t)}
						>
							<span class="truncate">{t.name}</span>
							{#if t.organisationId === $session.tenantId}
								<span class="text-brand-600">✓</span>
							{/if}
						</button>
					{/each}
					{#if $session.tenants.length === 0}
						<div class="px-4 py-2 text-sm text-ink-500">No organisations yet.</div>
					{/if}
					<div class="nav-dropdown-separator"></div>
					<button
						type="button"
						class="w-full text-left px-4 py-2 text-sm text-brand-600 hover:bg-brand-50 font-medium"
						onclick={() => { tenantMenu = false; showCreateOrg = true; }}
					>
						+ Add a new organisation
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
				settingsHref="/app/sales/settings"
				settingsLabel="Sales settings"
				isActive={salesActive}
			/>
			<NavDropdown
				label="Purchases"
				href="/app/purchases"
				groups={purchasesGroups}
				settingsHref="/app/purchases/settings"
				settingsLabel="Purchases settings"
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
				settingsHref="/app/accounting/settings"
				settingsLabel="Accounting settings"
				isActive={accountingActive}
			/>
			<NavDropdown
				label="Tax"
				href="/app/tax"
				groups={taxGroups}
				settingsHref="/app/tax/settings"
				settingsLabel="Tax settings"
				isActive={taxActive}
			/>
			<NavDropdown
				label="Contacts"
				href="/app/contacts"
				groups={contactsGroups}
				settingsHref="/app/contacts/settings"
				settingsLabel="Contacts settings"
				isActive={contactsActive}
			/>
		</nav>

		<div class="ml-auto flex items-center gap-1">
			<!-- Quick add -->
			<div class="relative" bind:this={quickAddRoot}>
				<button
					type="button"
					class="topbar-icon-btn"
					aria-label="Create"
					aria-expanded={quickAdd}
					onclick={() => (quickAdd = !quickAdd)}
				>
					<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M19 11h-6V5h-2v6H5v2h6v6h2v-6h6z" /></svg>
				</button>
				{#if quickAdd}
					<div class="absolute right-0 mt-1 min-w-[220px] rounded-lg bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-50">
						<a class="nav-dropdown-item" href="/app/invoices/new" onclick={() => (quickAdd = false)}>New invoice</a>
						<a class="nav-dropdown-item" href="/app/purchases/bills/new" onclick={() => (quickAdd = false)}>New bill</a>
						<a class="nav-dropdown-item" href="/app/contacts/new" onclick={() => (quickAdd = false)}>New contact</a>
						<a class="nav-dropdown-item" href="/app/sales/quotes/new" onclick={() => (quickAdd = false)}>New quote</a>
					</div>
				{/if}
			</div>
			<button type="button" class="topbar-icon-btn" aria-label="Search">
				<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M15.5 14h-.8l-.3-.3a6.5 6.5 0 1 0-.7.7l.3.3v.8l5 5 1.5-1.5-5-5zm-6 0a4.5 4.5 0 1 1 0-9 4.5 4.5 0 0 1 0 9z" /></svg>
			</button>
			<button type="button" class="topbar-icon-btn" aria-label="Help">
				<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 17h-2v-2h2v2zm2.1-7.8-.9.9c-.7.8-1.2 1.4-1.2 2.9h-2v-.5c0-1.1.5-2.1 1.2-2.9l1.2-1.3c.4-.4.6-.9.6-1.4a2 2 0 1 0-4 0H7a4 4 0 0 1 8 0c0 .8-.3 1.5-.9 2z" /></svg>
			</button>
			<button type="button" class="topbar-icon-btn" aria-label="Notifications">
				<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current"><path d="M12 22c1.1 0 2-.9 2-2h-4a2 2 0 0 0 2 2zm6-6V11a6 6 0 1 0-12 0v5l-2 2v1h16v-1l-2-2z" /></svg>
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
					<div class="absolute right-0 mt-1 min-w-[240px] rounded-lg bg-white text-ink-800 shadow-pop border border-ink-100 py-2 z-50">
						<div class="px-4 py-2 text-xs text-ink-500">
							Signed in as
							<div class="font-medium text-ink-900">{$session.email}</div>
						</div>
						<div class="nav-dropdown-separator"></div>
						<button
							type="button"
							class="w-full text-left nav-dropdown-item"
							onclick={() => { userMenu = false; showCreateOrg = true; }}
						>Add a new organisation</button>
						<a class="nav-dropdown-item" href="/app/settings" onclick={() => (userMenu = false)}>Settings</a>
						<button class="nav-dropdown-item w-full text-left" onclick={logout}>Sign out</button>
					</div>
				{/if}
			</div>
		</div>
	</div>
</header>

{#if showCreateOrg}
	<div class="fixed inset-0 bg-ink-900/40 flex items-center justify-center z-[60] p-4" role="dialog" aria-modal="true">
		<div class="bg-white rounded-xl shadow-pop w-full max-w-md">
			<div class="p-5 border-b border-ink-100 flex items-center justify-between">
				<h3 class="font-semibold text-lg">Add a new organisation</h3>
				<button type="button" class="btn-ghost" onclick={() => (showCreateOrg = false)} aria-label="Close">✕</button>
			</div>
			<div class="p-5 space-y-4">
				<label class="block">
					<span class="label">Organisation name *</span>
					<input class="input" bind:value={newOrg.name} placeholder="Acme, Inc." />
				</label>
				<div class="grid grid-cols-2 gap-3">
					<label class="block">
						<span class="label">Country</span>
						<input class="input" bind:value={newOrg.countryCode} maxlength="2" placeholder="US" />
					</label>
					<label class="block">
						<span class="label">Base currency</span>
						<input class="input" bind:value={newOrg.baseCurrency} maxlength="3" placeholder="USD" />
					</label>
				</div>
				{#if createError}
					<div class="text-sm text-red-700">{createError}</div>
				{/if}
			</div>
			<div class="p-5 border-t border-ink-100 flex justify-end gap-2">
				<button type="button" class="btn-secondary" onclick={() => (showCreateOrg = false)}>Cancel</button>
				<button
					type="button"
					class="btn-primary"
					onclick={createOrg}
					disabled={!newOrg.name.trim() || creatingOrg}
				>
					{creatingOrg ? 'Creating…' : 'Create organisation'}
				</button>
			</div>
		</div>
	</div>
{/if}
