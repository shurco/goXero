<script lang="ts">
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	const groups = [
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
			title: 'Cash flow',
			items: [
				{ label: 'Bank Summary', href: '/app/reports/bank-summary' },
				{ label: 'Cash Summary', href: '/app/reports/cash-summary' },
				{ label: 'Short-term cash flow', href: '/app/reports/short-term-cash-flow' }
			]
		},
		{
			title: 'Receivables & payables',
			items: [
				{ label: 'Aged Receivables', href: '/app/reports/aged-receivables' },
				{ label: 'Aged Payables', href: '/app/reports/aged-payables' }
			]
		},
		{
			title: 'Compliance',
			items: [
				{ label: 'Budget Summary', href: '/app/reports/budget-summary' },
				{ label: 'Executive Summary', href: '/app/reports/executive-summary' },
				{ label: 'Journal Report', href: '/app/reports/journal-report' }
			]
		}
	];
</script>

<ModuleHeader title="All reports" subtitle="Run any of the built-in financial reports." />

<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
	{#each groups as group}
		<div class="card p-6">
			<h2 class="text-sm font-semibold uppercase tracking-wide text-ink-500 mb-3">{group.title}</h2>
			<ul class="space-y-1">
				{#each group.items as it}
					<li>
						<a href={it.href} class="flex items-center justify-between py-2 px-2 rounded-md hover:bg-brand-50 text-brand-600 hover:text-brand-700 text-sm">
							<span>{it.label}</span>
							<svg viewBox="0 0 24 24" class="h-4 w-4 fill-current text-ink-400"><path d="M9 18l6-6-6-6z" /></svg>
						</a>
					</li>
				{/each}
			</ul>
		</div>
	{/each}
</div>
