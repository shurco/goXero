<script lang="ts">
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	interface Row {
		label: string;
		desc: string;
		href?: string;
		wip?: boolean;
	}
	interface Section {
		title: string;
		rows: Row[];
	}

	const sections: Section[] = [
		{
			title: 'General',
			rows: [
				{
					label: 'Organisation details',
					desc: "Update your organisation's name, logo, or contact information",
					href: '/app/settings/organisation'
				},
				{
					label: 'Users',
					desc: 'Invite new users, manage permissions, or delete current users',
					href: '/app/settings/users'
				},
				{ label: 'Subscription and billing', desc: 'Update your subscription, billing information, or pricing plan', wip: true },
				{ label: 'Connected apps', desc: 'Add and manage the apps connected to your organisation', href: '/app/settings/connected-apps' },
				{ label: 'Email settings', desc: 'Set a reply-to email address and name, or edit email templates', href: '/app/settings/email' }
			]
		},
		{
			title: 'Sales',
			rows: [
				{
					label: 'Invoice settings',
					desc: 'Default settings, invoice reminders, and branding themes',
					href: '/app/settings/organisation'
				},
				{ label: 'Online payments', desc: 'Payment options for customers to pay you online', wip: true }
			]
		},
		{
			title: 'Purchases',
			rows: [
				{ label: 'Check styles', desc: 'Create custom styles for checks', wip: true },
				{ label: 'Online bill payments', desc: 'Options for making payments directly from goXero', wip: true },
				{
					label: 'Peer invoicing',
					desc: 'Exchange invoices and bills with customers and suppliers on the same platform',
					wip: true
				}
			]
		},
		{
			title: 'Accounting',
			rows: [
				{ label: 'Financial settings', desc: 'Financial year end, tax settings, lock dates, and time zone', href: '/app/settings/financial' },
				{ label: 'Chart of accounts', desc: 'Accounts used to categorise your transactions', href: '/app/accounts' },
				{ label: 'Tracking categories', desc: 'See how different areas of your business are performing', wip: true },
				{ label: 'Conversion balances', desc: 'Opening balances of your accounts in goXero', href: '/app/settings/conversion-balances' }
			]
		},
		{
			title: 'Tax',
			rows: [
				{ label: 'Tax rates', desc: 'Add and manage tax rates', href: '/app/settings/financial' },
				{ label: 'Auto sales tax settings', desc: 'Manage Avalara integration settings', wip: true },
				{ label: 'Registered states', desc: 'States where your organisation is registered to collect sales tax', wip: true }
			]
		},
		{
			title: 'Contacts',
			rows: [{ label: 'Custom contact links', desc: 'Connect your contacts with external systems such as CRM', wip: true }]
		},
		{
			title: 'Projects',
			rows: [{ label: 'Staff permissions', desc: 'Invite staff to Projects and manage their permissions', wip: true }]
		}
	];
</script>

<div class="w-full space-y-8">
	<ModuleHeader
		title="Settings"
		subtitle="Settings for the organisation selected in the header — they do not apply to your other organisations."
	/>

	{#each sections as sec}
		<section class="space-y-4">
			<h2 class="content-section-title">{sec.title}</h2>
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
				{#each sec.rows as row}
					{#if row.href}
						<a
							href={row.href}
							class="card block p-5 transition hover:shadow-pop focus:outline-none focus:ring-2 focus:ring-brand-400 focus:ring-offset-2"
						>
							<div class="flex flex-wrap items-baseline gap-2">
								<span class="font-medium text-ink-900">{row.label}</span>
								{#if row.wip}
									<span
										class="inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-amber-800"
									>
										Under development
									</span>
								{/if}
							</div>
							<p class="muted mt-1.5 text-sm leading-relaxed">{row.desc}</p>
						</a>
					{:else}
						<div class="card cursor-not-allowed p-5 opacity-90">
							<div class="flex flex-wrap items-baseline gap-2">
								<span class="font-medium text-ink-800">{row.label}</span>
								{#if row.wip}
									<span
										class="inline-flex items-center rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-amber-800"
									>
										Under development
									</span>
								{/if}
							</div>
							<p class="muted mt-1.5 text-sm leading-relaxed">{row.desc}</p>
						</div>
					{/if}
				{/each}
			</div>
		</section>
	{/each}
</div>
