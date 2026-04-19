<script lang="ts">
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type { BankTile } from '$lib/dashboard-types';

	interface Props {
		tile: BankTile;
		variant: 'first' | 'second' | 'rest';
		currency: string;
		menuOpen: string | null;
		toggleMenu: (id: string) => void;
	}
	let { tile, variant, currency, menuOpen, toggleMenu }: Props = $props();

	const mid = $derived(`bank-${tile.account.AccountID}`);
</script>

{#if variant === 'rest'}
	<div class="font-semibold text-ink-900">{tile.account.Name}</div>
	<div class="grid grid-cols-2 gap-3 text-sm">
		<div>
			<div class="text-xl font-semibold tabular-nums">
				{formatCurrency(tile.statementBalance, tile.account.CurrencyCode || currency)}
			</div>
			<div class="text-xs text-ink-500">Statement</div>
		</div>
		<div>
			<div class="text-xl font-semibold tabular-nums">
				{formatCurrency(tile.xeroBalance, tile.account.CurrencyCode || currency)}
			</div>
			<div class="text-xs text-brand-700">In goXero</div>
		</div>
	</div>
{:else if variant === 'first'}
	<div class="flex items-start justify-between gap-3">
		<div>
			<div class="font-semibold text-ink-900">{tile.account.Name}</div>
			<div class="mt-0.5 text-xs text-ink-500">
				{tile.account.BankAccountNumber || tile.account.Code}
			</div>
		</div>
		<div class="relative" data-dash-menu>
			<button
				class="rounded p-1 text-ink-400 hover:bg-ink-100 hover:text-ink-600"
				aria-label="More"
				aria-expanded={menuOpen === mid}
				onclick={() => toggleMenu(mid)}
			>
				<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24" aria-hidden="true">
					<circle cx="12" cy="6" r="1.5" />
					<circle cx="12" cy="12" r="1.5" />
					<circle cx="12" cy="18" r="1.5" />
				</svg>
			</button>
			{#if menuOpen === mid}
				<div
					class="absolute right-0 z-30 mt-1 min-w-[220px] rounded-lg border border-ink-100 bg-white py-2 text-ink-800 shadow-pop"
					role="menu"
				>
					<a class="nav-dropdown-item" href="/app/bank-transactions?accountId={tile.account.AccountID}"
						>Account transactions</a
					>
					<a class="nav-dropdown-item" href="/app/bank-feeds">Manage bank feeds</a>
					<a
						class="nav-dropdown-item"
						href="/app/accounting/bank-accounts/{tile.account.AccountID}?tab=statements"
						>Import bank statement</a
					>
					<a class="nav-dropdown-item" href="/app/accounting/bank-accounts/{tile.account.AccountID}/edit"
						>Edit account details</a
					>
					<a class="nav-dropdown-item" href="/app/reports/bank-summary">Bank summary report</a>
				</div>
			{/if}
		</div>
	</div>
	<div class="grid grid-cols-2 gap-4 pt-1">
		<div>
			<div class="text-2xl font-semibold tabular-nums text-ink-900">
				{formatCurrency(tile.statementBalance, tile.account.CurrencyCode || currency)}
			</div>
			<div class="text-xs text-ink-500">
				Statement balance{#if tile.lastStatementDate}
					<span class="text-ink-400"> ({formatDate(tile.lastStatementDate)})</span>
				{/if}
			</div>
		</div>
		<div>
			<div class="text-2xl font-semibold tabular-nums text-ink-900">
				{formatCurrency(tile.xeroBalance, tile.account.CurrencyCode || currency)}
			</div>
			<div class="text-xs text-brand-700">Balance in goXero</div>
		</div>
	</div>
	<div class="flex items-center justify-between border-t border-ink-100 pt-2 text-sm">
		<span class="text-ink-600">Balance difference</span>
		<span class="font-semibold tabular-nums text-ink-900">
			{formatCurrency(
				Math.abs(tile.statementBalance - tile.xeroBalance),
				tile.account.CurrencyCode || currency
			)}
		</span>
	</div>
	<div class="mt-auto flex flex-wrap items-center justify-between gap-2 border-t border-ink-100 pt-3">
		{#if tile.unreconciled > 0}
			<a
				href="/app/accounting/bank-accounts/{tile.account.AccountID}?tab=reconcile"
				class="btn-primary !rounded-full !px-4 !py-2 !text-xs font-semibold"
			>
				Reconcile {tile.unreconciled} items
			</a>
		{:else}
			<span class="inline-flex items-center gap-1 text-xs text-emerald-700">
				<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current"
					><path
						d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1 14.5-4.5-4.5 1.4-1.4 3.1 3 6.6-6.6 1.4 1.4-8 8z"
					/></svg
				>
				Up to date
			</span>
		{/if}
		<a
			href="/app/accounting/bank-accounts/{tile.account.AccountID}?tab=statements"
			class="btn-secondary !rounded-full !px-4 !py-2 !text-xs font-semibold text-brand-800"
		>
			Import bank statement
		</a>
	</div>
{:else}
	<div class="flex items-start justify-between gap-3">
		<div>
			<div class="font-semibold text-ink-900">{tile.account.Name}</div>
			<div class="mt-0.5 text-xs text-ink-500">
				{tile.account.BankAccountNumber || tile.account.Code}
			</div>
		</div>
		<div class="relative" data-dash-menu>
			<button
				class="rounded p-1 text-ink-400 hover:bg-ink-100"
				aria-label="More"
				aria-expanded={menuOpen === mid}
				onclick={() => toggleMenu(mid)}
			>
				<svg class="h-5 w-5 fill-current" viewBox="0 0 24 24"
					><circle cx="12" cy="6" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="12" cy="18" r="1.5" /></svg
				>
			</button>
			{#if menuOpen === mid}
				<div
					class="absolute right-0 z-30 mt-1 min-w-[220px] rounded-lg border border-ink-100 bg-white py-2 shadow-pop"
				>
					<a class="nav-dropdown-item" href="/app/bank-transactions?accountId={tile.account.AccountID}"
						>Account transactions</a
					>
					<a class="nav-dropdown-item" href="/app/bank-feeds">Manage bank feeds</a>
					<a
						class="nav-dropdown-item"
						href="/app/accounting/bank-accounts/{tile.account.AccountID}?tab=statements"
						>Import bank statement</a
					>
					<a class="nav-dropdown-item" href="/app/accounting/bank-accounts/{tile.account.AccountID}/edit"
						>Edit account details</a
					>
					<a class="nav-dropdown-item" href="/app/reports/bank-summary">Bank summary report</a>
				</div>
			{/if}
		</div>
	</div>
	<div class="grid grid-cols-2 gap-4">
		<div>
			<div class="text-2xl font-semibold tabular-nums">
				{formatCurrency(tile.statementBalance, tile.account.CurrencyCode || currency)}
			</div>
			<div class="text-xs text-ink-500">Statement balance</div>
		</div>
		<div>
			<div class="text-2xl font-semibold tabular-nums">
				{formatCurrency(tile.xeroBalance, tile.account.CurrencyCode || currency)}
			</div>
			<div class="text-xs text-brand-700">Balance in goXero</div>
		</div>
	</div>
	<div class="border-t border-ink-100 pt-3">
		<a
			href="/app/accounting/bank-accounts/{tile.account.AccountID}?tab=statements"
			class="btn-secondary btn-secondary-sm w-full justify-center sm:w-auto"
		>
			Import bank statement
		</a>
	</div>
{/if}
