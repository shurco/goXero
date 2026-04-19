<script lang="ts">
	import type { BankTile } from '$lib/dashboard-types';
	import { STATIC_WIDGET_IDS, bankWidgetId } from '$lib/dashboard-layout';

	interface Props {
		open: boolean;
		tiles: BankTile[];
		hidden: Record<string, boolean>;
		onClose: () => void;
		onToggle: (widgetId: string, visible: boolean) => void;
	}
	let { open, tiles, hidden, onClose, onToggle }: Props = $props();

	const WIDGET_LABELS: Record<string, string> = {
		'bills-pay': 'Bills to pay',
		'net-profit': 'Profit and loss',
		tasks: 'Tasks',
		'recent-payments': 'Recent invoice payments',
		'expenses-review': 'Expenses to review',
		'invoices-owed': 'Invoices owed to you',
		'cash-in-out': 'Cash in and out',
		'coa-watchlist': 'Chart of accounts watchlist'
	};

	function isVisible(id: string): boolean {
		return hidden[id] !== true;
	}

	function toggleWidget(id: string) {
		onToggle(id, !isVisible(id));
	}

	function toggleBank(tile: BankTile) {
		const id = bankWidgetId(tile.account.AccountID);
		onToggle(id, !isVisible(id));
	}
</script>

{#if open}
	<div
		class="fixed inset-0 z-[100] flex items-center justify-center bg-ink-900/40 p-4"
		role="presentation"
		onclick={(e) => e.target === e.currentTarget && onClose()}
	>
		<div
			class="flex max-h-[90vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-ink-200 bg-white shadow-pop"
			role="dialog"
			aria-modal="true"
			aria-labelledby="dash-widgets-title"
			tabindex="-1"
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => e.stopPropagation()}
		>
			<div class="flex items-center justify-between border-b border-ink-100 px-5 py-4">
				<h2 id="dash-widgets-title" class="text-lg font-semibold text-ink-900">Show and hide widgets</h2>
				<button
					type="button"
					class="rounded p-1.5 text-ink-500 hover:bg-ink-100 hover:text-ink-800"
					aria-label="Close"
					onclick={onClose}
				>
					<svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
						><path d="M18 6L6 18M6 6l12 12" stroke-linecap="round" /></svg
					>
				</button>
			</div>

			<div class="flex-1 overflow-y-auto px-5 py-4">
				<p class="mb-2 text-xs font-medium uppercase tracking-wide text-ink-500">Bank accounts</p>
				<div class="overflow-hidden rounded-lg border border-ink-200 bg-white">
					{#if tiles.length === 0}
						<p class="px-4 py-6 text-sm text-ink-500">No bank accounts yet.</p>
					{:else}
						{#each tiles as tile (tile.account.AccountID)}
							<div
								class="flex items-center justify-between gap-3 border-t border-ink-100 px-4 py-3 first:border-t-0"
							>
								<div class="min-w-0">
									<div class="font-semibold text-ink-900">{tile.account.Name}</div>
									<div class="text-xs text-ink-500">
										{tile.account.BankAccountNumber || tile.account.Code || '—'}
									</div>
								</div>
								<button
									type="button"
									class="relative h-7 w-12 shrink-0 rounded-full border border-ink-200 transition"
									class:bg-brand-500={isVisible(bankWidgetId(tile.account.AccountID))}
									class:border-brand-500={isVisible(bankWidgetId(tile.account.AccountID))}
									role="switch"
									aria-label="Show or hide {tile.account.Name} on dashboard"
									aria-checked={isVisible(bankWidgetId(tile.account.AccountID))}
									onclick={() => toggleBank(tile)}
								>
									<span
										class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition"
										class:translate-x-5={isVisible(bankWidgetId(tile.account.AccountID))}
									></span>
								</button>
							</div>
						{/each}
					{/if}
				</div>

				<p class="mb-2 mt-6 text-xs font-medium uppercase tracking-wide text-ink-500">Widgets</p>
				<div class="overflow-hidden rounded-lg border border-ink-200 bg-white">
					{#each STATIC_WIDGET_IDS as wid (wid)}
						<div class="flex items-center justify-between gap-3 border-t border-ink-100 px-4 py-3 first:border-t-0">
							<span class="font-semibold text-ink-900">{WIDGET_LABELS[wid] ?? wid}</span>
							<button
								type="button"
								class="relative h-7 w-12 shrink-0 rounded-full border border-ink-200 transition"
								class:bg-brand-500={isVisible(wid)}
								class:border-brand-500={isVisible(wid)}
								role="switch"
								aria-label="Show or hide {WIDGET_LABELS[wid] ?? wid}"
								aria-checked={isVisible(wid)}
								onclick={() => toggleWidget(wid)}
							>
								<span
									class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition"
									class:translate-x-5={isVisible(wid)}
								></span>
							</button>
						</div>
					{/each}
				</div>
			</div>

			<div class="border-t border-ink-100 px-5 py-4">
				<button type="button" class="btn-primary ml-auto block" onclick={onClose}>Done</button>
			</div>
		</div>
	</div>
{/if}
