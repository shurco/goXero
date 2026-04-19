<script lang="ts">
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	const tiles = [
		{
			title: 'Banking',
			items: [
				{ label: 'Bank accounts', href: '/app/accounting/bank-accounts', desc: 'Reconcile and view movements' },
				{ label: 'Bank rules', href: '/app/accounting/bank-rules', desc: 'Automate common transactions' }
			]
		},
		{
			title: 'Accounting tools',
			items: [
				{ label: 'Chart of accounts', href: '/app/accounts', desc: 'General ledger structure' },
				{ label: 'Fixed assets', href: '/app/accounting/fixed-assets', desc: 'Depreciation register' },
				{ label: 'Manual journals', href: '/app/accounting/manual-journals', desc: 'Direct GL entries' },
				{ label: 'Find and recode', href: '/app/accounting/find-and-recode', desc: 'Bulk search and recode' },
				{ label: 'Assurance dashboard', href: '/app/accounting/assurance', desc: 'Data quality overview' },
				{ label: 'History and notes', href: '/app/accounting/history-and-notes', desc: 'Change history and notes' }
			]
		}
	];
</script>

<ModuleHeader title="Accounting" subtitle="Banking, ledger and reporting tools." />

<p class="muted mb-6 text-sm">
	<a href="/app/bank-feeds" class="text-brand-700 hover:underline">Bank feeds</a>
	— connect bank accounts via Open Banking.
</p>

<div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
	{#each tiles as group}
		<div class="card p-6">
			<h2 class="content-section-title mb-4">{group.title}</h2>
			<div class="space-y-3">
				{#each group.items as it}
					<a
						href={it.href}
						class="block rounded-lg border border-ink-100 p-4 transition hover:border-brand-400 hover:bg-brand-50/30"
					>
						<div class="font-medium text-ink-900">{it.label}</div>
						<div class="muted mt-0.5 text-xs">{it.desc}</div>
					</a>
				{/each}
			</div>
		</div>
	{/each}
</div>
