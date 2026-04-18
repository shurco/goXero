<script lang="ts">
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	const tiles = [
		{
			title: 'Banking',
			items: [
				{ label: 'Bank accounts', href: '/app/accounting/bank-accounts', desc: 'Reconcile and view movements' },
				{ label: 'Bank feeds', href: '/app/bank-feeds', desc: 'Connect via Open Banking' },
				{ label: 'Bank rules', href: '/app/accounting/bank-rules', desc: 'Automate common transactions' }
			]
		},
		{
			title: 'Accounting tools',
			items: [
				{ label: 'Chart of accounts', href: '/app/accounts', desc: 'General ledger structure' },
				{ label: 'Manual journals', href: '/app/accounting/manual-journals', desc: 'Direct GL entries' },
				{ label: 'Fixed assets', href: '/app/accounting/fixed-assets', desc: 'Depreciation register' }
			]
		}
	];
</script>

<ModuleHeader title="Accounting" subtitle="Banking, ledger and reporting tools." />

<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
	{#each tiles as group}
		<div class="card p-6">
			<h2 class="text-lg font-semibold text-ink-900 mb-4">{group.title}</h2>
			<div class="space-y-3">
				{#each group.items as it}
					<a href={it.href} class="block rounded-lg border border-ink-100 hover:border-brand-400 hover:bg-brand-50/30 p-4 transition">
						<div class="font-medium text-ink-900">{it.label}</div>
						<div class="text-xs muted mt-0.5">{it.desc}</div>
					</a>
				{/each}
			</div>
		</div>
	{/each}
</div>
