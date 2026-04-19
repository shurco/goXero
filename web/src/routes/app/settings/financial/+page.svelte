<script module lang="ts">
	const currencyNames: Record<string, string> = {
		USD: 'United States Dollar',
		EUR: 'Euro',
		GBP: 'British Pound',
		AUD: 'Australian Dollar',
		NZD: 'New Zealand Dollar',
		CAD: 'Canadian Dollar',
		JPY: 'Japanese Yen',
		RUB: 'Russian Rouble'
	};
	export function currencyName(code?: string) {
		return currencyNames[(code || '').toUpperCase()] ?? '';
	}
</script>

<script lang="ts">
	import { onMount } from 'svelte';
	import { orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import SettingsHeader from '$lib/components/SettingsHeader.svelte';
	import SettingsSection from '$lib/components/SettingsSection.svelte';
	import SettingsFooterActions from '$lib/components/SettingsFooterActions.svelte';
	import { formatDate } from '$lib/utils/format';
	import type { Organisation } from '$lib/types';

	let org = $state<Organisation | null>(null);
	let loading = $state(true);

	// Local form state — seeded from the org record and left as "draft" until
	// a PUT /organisation endpoint ships. The Save button is surfaced so the
	// UI matches Xero and stays keyboard-navigable in the interim.
	let form = $state({
		taxBasis: 'Accrual',
		taxNumber: '',
		taxIdDisplayName: 'Tax reg',
		taxPeriod: '3 Monthly',
		salesDefault: 'Based on last sale',
		purchasesDefault: 'Based on last purchase',
		lockDateAll: '',
		lockDateStaff: '',
		timezone: '(UTC-05:00) Eastern Time (US & Canada)',
		financialYearEnd: '31 December'
	});

	const months = [
		'January',
		'February',
		'March',
		'April',
		'May',
		'June',
		'July',
		'August',
		'September',
		'October',
		'November',
		'December'
	];

	async function reload() {
		loading = true;
		try {
			org = (await orgApi.current()) ?? null;
			if (org) {
				form.taxNumber = org.TaxNumber ?? '';
				if (org.Timezone) form.timezone = org.Timezone;
				if (org.FinancialYearEndDay && org.FinancialYearEndMonth) {
					form.financialYearEnd = `${org.FinancialYearEndDay} ${months[org.FinancialYearEndMonth - 1] ?? ''}`;
				}
			}
		} finally {
			loading = false;
		}
	}

	onMount(reload);
	$effect(() => {
		if ($session.tenantId) void reload();
	});

	let saving = $state(false);
	async function save() {
		saving = true;
		try {
			// PUT /api/v1/organisation is not yet implemented server-side.
			await new Promise((r) => setTimeout(r, 300));
			alert(
				'Changes captured locally. Persisting financial settings needs a PUT /api/v1/organisation endpoint.'
			);
		} finally {
			saving = false;
		}
	}

	function cancel() {
		void reload();
	}
</script>

<SettingsHeader title="Financial settings" />

{#if loading}
	<div class="card p-6 text-center muted">Loading…</div>
{:else}
	<div class="card max-w-3xl">
		<SettingsSection title="Currency">
			<p class="text-sm text-ink-800">
				<span class="font-semibold">{org?.BaseCurrency || 'USD'}</span>
				{currencyName(org?.BaseCurrency)} is the base currency for this organisation.
			</p>
		</SettingsSection>

		<SettingsSection title="Financial Year End">
			<div class="flex items-center gap-3 text-sm">
				<span class="read-value">{form.financialYearEnd}</span>
				<button
					class="text-brand-600 hover:underline"
					type="button"
					onclick={() => alert('Editing FYE requires PUT /api/v1/organisation.')}
				>
					Change
				</button>
			</div>
		</SettingsSection>

		<SettingsSection title="Sales Tax">
			<div class="form-grid-4">
				<div>
					<label class="label" for="fs-tax-basis">Tax Basis</label>
					<select id="fs-tax-basis" class="select" bind:value={form.taxBasis}>
						<option value="Accrual">Accrual Basis</option>
						<option value="Cash">Cash Basis</option>
					</select>
				</div>
				<div>
					<label class="label" for="fs-tax-number">Tax ID Number</label>
					<input id="fs-tax-number" class="input" bind:value={form.taxNumber} placeholder="101-2-303" />
				</div>
				<div>
					<label class="label" for="fs-tax-display">Tax ID Display Name</label>
					<input id="fs-tax-display" class="input" bind:value={form.taxIdDisplayName} />
				</div>
				<div>
					<label class="label" for="fs-tax-period">Tax Period</label>
					<select id="fs-tax-period" class="select" bind:value={form.taxPeriod}>
						<option>Monthly</option>
						<option>2 Monthly</option>
						<option>3 Monthly</option>
						<option>6 Monthly</option>
						<option>Annually</option>
					</select>
				</div>
			</div>
		</SettingsSection>

		<SettingsSection title="Tax Defaults">
			<div class="form-grid-2">
				<div>
					<label class="label" for="fs-sales-default">For Sales</label>
					<select id="fs-sales-default" class="select" bind:value={form.salesDefault}>
						<option>Based on last sale</option>
						<option>Tax Exclusive</option>
						<option>Tax Inclusive</option>
						<option>No Tax</option>
					</select>
					<p class="muted text-xs mt-1">
						Includes invoices, quotes, credit notes and receive money items
					</p>
				</div>
				<div>
					<label class="label" for="fs-purch-default">For Purchases</label>
					<select id="fs-purch-default" class="select" bind:value={form.purchasesDefault}>
						<option>Based on last purchase</option>
						<option>Tax Exclusive</option>
						<option>Tax Inclusive</option>
						<option>No Tax</option>
					</select>
					<p class="muted text-xs mt-1">
						Includes bills, purchase orders, credit notes and spend money items
					</p>
				</div>
			</div>
		</SettingsSection>

		<SettingsSection
			title="Lock Dates"
			hint="Lock dates stop data from being changed for a specific period."
			description="Lock dates stop data from being changed for a specific period. You can change these at any time."
		>
			<div class="form-grid-2">
				<div>
					<label class="label" for="fs-lock-all">
						Stop all users (except advisers) making changes on and before this date. Auto-reconcile
						still runs during this period.
					</label>
					<input id="fs-lock-all" class="input" type="date" bind:value={form.lockDateAll} />
				</div>
				<div>
					<label class="label" for="fs-lock-staff">
						Stop all users making changes on and before
					</label>
					<input id="fs-lock-staff" class="input" type="date" bind:value={form.lockDateStaff} />
				</div>
			</div>
		</SettingsSection>

		<SettingsSection title="Time zone">
			<select class="select sm:w-96" aria-label="Time zone" bind:value={form.timezone}>
				<option>(UTC-08:00) Pacific Time (US &amp; Canada)</option>
				<option>(UTC-07:00) Mountain Time (US &amp; Canada)</option>
				<option>(UTC-06:00) Central Time (US &amp; Canada)</option>
				<option>(UTC-05:00) Eastern Time (US &amp; Canada)</option>
				<option>(UTC+00:00) UTC</option>
				<option>(UTC+01:00) Central European Time</option>
				<option>(UTC+03:00) Moscow</option>
				<option>(UTC+08:00) Perth</option>
				<option>(UTC+10:00) Sydney</option>
			</select>
		</SettingsSection>

		<SettingsFooterActions {saving} onSave={save} onCancel={cancel} />
	</div>

	<!-- History & Notes -->
	<div class="mt-8 max-w-3xl">
		<h3 class="text-sm font-semibold text-ink-700 mb-2">History &amp; Notes</h3>
		<button class="btn-secondary" type="button" disabled>Add Note</button>
		<p class="muted text-xs mt-2">
			Last updated {org?.UpdatedDateUTC ? formatDate(org.UpdatedDateUTC) : '—'}.
		</p>
	</div>
{/if}

