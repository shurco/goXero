<script lang="ts">
	interface Row {
		label: string;
		desc: string;
		href?: string;
		/** When true, show an "Under development" badge next to the row title. */
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
				{ label: 'Organisation details', desc: "Update your organisation's name, logo, or contact information", href: '/app/settings/organisation' },
				{ label: 'Users', desc: 'Invite new users, manage permissions, or delete current users', href: '/app/settings/users' },
				{ label: 'Subscription and billing', desc: 'Update your subscription, billing information, or pricing plan', wip: true },
				{ label: 'Connected apps', desc: 'Add and manage the apps connected to your organisation', wip: true },
				{ label: 'Email settings', desc: 'Set a reply-to email address and name, or edit email templates', wip: true }
			]
		},
		{
			title: 'Sales',
			rows: [
				{ label: 'Invoice settings', desc: 'Manage default settings, invoice reminders, and branding themes (templates for invoices, quotes, purchase orders etc.)', href: '/app/sales/settings', wip: true },
				{ label: 'Online payments', desc: 'Add and manage payment options for customers to pay you online', wip: true }
			]
		},
		{
			title: 'Purchases',
			rows: [
				{ label: 'Check styles', desc: 'Create custom styles for checks', wip: true },
				{ label: 'Online bill payments', desc: 'Add and manage options for making payments directly from goXero', wip: true },
				{ label: 'Xero to Xero', desc: 'Connect with customers and suppliers who use Xero to exchange invoices and bills directly', wip: true }
			]
		},
		{
			title: 'Accounting',
			rows: [
				{ label: 'Financial settings', desc: 'Update financial year end, tax settings, lock dates, and time zone', href: '/app/settings/financial' },
				{ label: 'Chart of accounts', desc: 'Manage accounts used to categorise your transactions', href: '/app/accounts' },
				{ label: 'Tracking categories', desc: 'Set up tracking categories to see how different areas of your business are performing', wip: true },
				{ label: 'Conversion balances', desc: 'Update the opening balances of your accounts in goXero', wip: true }
			]
		},
		{
			title: 'Tax',
			rows: [
				{ label: 'Tax rates', desc: 'Add and manage tax rates', href: '/app/settings/tax-rates' },
				{ label: 'Auto sales tax settings', desc: 'Manage Avalara integration settings', wip: true },
				{ label: 'Registered states', desc: 'Add and manage states where your organisation is registered to collect sales tax', wip: true }
			]
		},
		{
			title: 'Contacts',
			rows: [
				{ label: 'Custom contact links', desc: 'Connect your contacts with external systems such as CRM', wip: true }
			]
		},
		{
			title: 'Projects',
			rows: [{ label: 'Staff permissions', desc: 'Invite staff to Projects and manage their permissions', wip: true }]
		}
	];
</script>

<div class="flex">
	<!-- Left rail label (mirrors Xero's "Settings" sticky label) -->
	<aside class="hidden md:block w-56 shrink-0 pt-1">
		<h1 class="text-base font-semibold text-ink-900 px-2">Settings</h1>
	</aside>

	<div class="flex-1 space-y-8">
		{#each sections as sec}
			<section>
				<h2 class="text-sm font-semibold text-ink-900 mb-3">{sec.title}</h2>
				<div class="card divide-y divide-ink-100">
					{#each sec.rows as row}
						<div class="flex items-start justify-between gap-4 p-4">
							<div>
								{#if row.href}
									<a href={row.href} class="text-brand-600 hover:underline font-medium">{row.label}</a>
								{:else}
									<span class="font-medium text-ink-700">{row.label}</span>
								{/if}
								{#if row.wip}
									<span class="ml-2 inline-flex items-center rounded-full bg-amber-100 text-amber-800 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider">Under development</span>
								{/if}
								<div class="text-sm muted mt-0.5">{row.desc}</div>
							</div>
						</div>
					{/each}
				</div>
			</section>
		{/each}
	</div>
</div>
