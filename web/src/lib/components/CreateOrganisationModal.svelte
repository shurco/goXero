<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import {
		FY_DAYS,
		MONTH_NAMES,
		ONBOARDING_ACCOUNTING_TOOLS,
		ONBOARDING_COUNTRIES,
		ONBOARDING_COUNTRY_DEFAULTS,
		ONBOARDING_CURRENCIES,
		ONBOARDING_INDUSTRIES,
		ONBOARDING_TIMEZONES
	} from '$lib/organisation-onboarding';

	let { open = $bindable(false) }: { open: boolean } = $props();

	let businessName = $state('');
	let industry = $state('');
	let countryCode = $state('US');
	let baseCurrency = $state('USD');
	let timezone = $state('America/New_York');
	let fyDay = $state(31);
	let fyMonth = $state(12);
	/** Default "no" so the form can be submitted without a flaky radio binding; user can switch to "yes". */
	let employeesChoice = $state<'yes' | 'no'>('no');
	let priorAccountingTool = $state('');

	let creating = $state(false);
	let errorMsg = $state('');

	const formValid = $derived(businessName.trim().length > 0 && countryCode.length > 0);

	function applyCountryDefaults() {
		const d = ONBOARDING_COUNTRY_DEFAULTS[countryCode];
		if (d) {
			baseCurrency = d.currency;
			timezone = d.timezone;
		}
	}

	function resetForm() {
		businessName = '';
		industry = '';
		countryCode = 'US';
		fyDay = 31;
		fyMonth = 12;
		employeesChoice = 'no';
		priorAccountingTool = '';
		errorMsg = '';
		applyCountryDefaults();
	}

	$effect(() => {
		if (open) resetForm();
	});

	$effect(() => {
		countryCode;
		applyCountryDefaults();
	});

	function close() {
		open = false;
	}

	$effect(() => {
		if (!open) return;
		function onEsc(e: KeyboardEvent) {
			if (e.key === 'Escape') close();
		}
		document.addEventListener('keydown', onEsc);
		return () => document.removeEventListener('keydown', onEsc);
	});

	async function createOrganisation(e: Event) {
		e.preventDefault();
		if (!formValid || creating) return;
		creating = true;
		errorMsg = '';
		try {
			const payload = {
				name: businessName.trim(),
				countryCode,
				baseCurrency,
				timezone,
				lineOfBusiness: industry.trim() || undefined,
				financialYearEndDay: Number(fyDay),
				financialYearEndMonth: Number(fyMonth),
				hasEmployees: employeesChoice === 'yes',
				priorAccountingTool: priorAccountingTool || undefined
			};
			await orgApi.create(payload);
			const mine = await orgApi.mine();
			const created = mine.organisations?.find((o) => o.Name === payload.name);
			if (created) {
				session.updateTenants(
					mine.organisations.map((o) => ({
						organisationId: o.OrganisationID,
						name: o.Name,
						baseCurrency: o.BaseCurrency
					}))
				);
				session.setTenant(created.OrganisationID);
				open = false;
				location.href = '/app';
				return;
			}
			await invalidateAll();
			open = false;
		} catch (e) {
			errorMsg = e instanceof Error ? e.message : 'Could not create organisation';
		} finally {
			creating = false;
		}
	}
</script>

{#if open}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 z-[60] flex items-center justify-center bg-ink-900/40 p-4"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) close();
		}}
	>
		<div
			class="bg-white rounded-xl shadow-pop w-full max-w-lg max-h-[90vh] flex flex-col border border-ink-100 outline-none overflow-hidden"
			role="dialog"
			aria-modal="true"
			aria-labelledby="create-org-title"
			tabindex="-1"
		>
			<div class="px-6 py-4 border-b border-ink-100 flex items-center justify-between shrink-0">
				<h2 id="create-org-title" class="text-lg font-semibold text-ink-900">Add your business</h2>
				<button type="button" class="btn-ghost" onclick={close} aria-label="Close">✕</button>
			</div>

			<form class="overflow-y-auto flex-1 px-6 py-5 space-y-5" onsubmit={createOrganisation}>
				{#if errorMsg}
					<p class="text-sm text-red-700" role="alert">{errorMsg}</p>
				{/if}

				<label class="block" for="ob-name">
					<span class="label">Business name</span>
					<input
						id="ob-name"
						class="input w-full"
						bind:value={businessName}
						placeholder=""
						autocomplete="organization"
						required
					/>
				</label>

				<div id="industry-block">
					<label class="block" for="ob-industry">
						<span class="label">Industry</span>
					</label>
					<div class="relative">
						<svg
							viewBox="0 0 24 24"
							class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400"
							fill="none"
							stroke="currentColor"
							stroke-width="2"
							aria-hidden="true"
						>
							<circle cx="11" cy="11" r="7" />
							<path d="M21 21l-4-4" stroke-linecap="round" />
						</svg>
						<input
							id="ob-industry"
							class="input w-full pl-9"
							bind:value={industry}
							placeholder="e.g. construction, retail, services"
							list="ob-industry-datalist"
							autocomplete="off"
						/>
						<datalist id="ob-industry-datalist">
							{#each ONBOARDING_INDUSTRIES as ind}
								<option value={ind}></option>
							{/each}
						</datalist>
					</div>
					<p class="text-xs text-ink-500 mt-1.5">
						If you can’t find your industry,
						<button
							type="button"
							class="text-brand-600 hover:underline font-medium"
							onclick={() => document.getElementById('ob-industry')?.focus()}
						>
							select it from this list
						</button>
						(datalist suggestions as you type).
					</p>
				</div>

				<label class="block" for="ob-country">
					<span class="label">Country</span>
					<select id="ob-country" class="input w-full" bind:value={countryCode}>
						{#each ONBOARDING_COUNTRIES as c}
							<option value={c.code}>{c.name}</option>
						{/each}
					</select>
				</label>

				<div class="flex flex-col sm:flex-row sm:flex-wrap gap-4">
					<label class="block flex-1 min-w-[140px]" for="ob-tz">
						<span class="label">Time zone</span>
						<select id="ob-tz" class="input w-full text-sm" bind:value={timezone}>
							{#each ONBOARDING_TIMEZONES as tz}
								<option value={tz.value}>{tz.label}</option>
							{/each}
						</select>
					</label>
					<label class="block flex-1 min-w-[140px]" for="ob-currency">
						<span class="label">Currency</span>
						<select id="ob-currency" class="input w-full text-sm" bind:value={baseCurrency}>
							{#each ONBOARDING_CURRENCIES as cur}
								<option value={cur.code}>{cur.label}</option>
							{/each}
						</select>
					</label>
				</div>

				<div>
					<span class="label">Last day of your financial year</span>
					<div class="grid grid-cols-2 gap-3 mt-1.5">
						<label class="sr-only" for="ob-fy-day">Day</label>
						<select id="ob-fy-day" class="input w-full" bind:value={fyDay}>
							{#each FY_DAYS as d}
								<option value={d}>{d}</option>
							{/each}
						</select>
						<label class="sr-only" for="ob-fy-month">Month</label>
						<select id="ob-fy-month" class="input w-full" bind:value={fyMonth}>
							{#each MONTH_NAMES as m}
								<option value={m.value}>{m.label}</option>
							{/each}
						</select>
					</div>
				</div>

				<fieldset>
					<legend class="label mb-2">Do you have employees?</legend>
					<div class="rounded-lg border border-ink-200 divide-y divide-ink-100 overflow-hidden">
						<label
							class="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-ink-50/80 has-[:focus-visible]:bg-ink-50"
							for="ob-emp-yes"
						>
							<input
								id="ob-emp-yes"
								type="radio"
								class="accent-brand-500"
								name="create-org-employees"
								checked={employeesChoice === 'yes'}
								onchange={() => {
									employeesChoice = 'yes';
								}}
							/>
							<span class="text-sm text-ink-900">Yes</span>
						</label>
						<label
							class="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-ink-50/80 has-[:focus-visible]:bg-ink-50"
							for="ob-emp-no"
						>
							<input
								id="ob-emp-no"
								type="radio"
								class="accent-brand-500"
								name="create-org-employees"
								checked={employeesChoice === 'no'}
								onchange={() => {
									employeesChoice = 'no';
								}}
							/>
							<span class="text-sm text-ink-900">No, it’s just me</span>
						</label>
					</div>
				</fieldset>

				<label class="block" for="ob-tool">
					<span class="label">What accounting tool do you currently use?</span>
					<select id="ob-tool" class="input w-full" bind:value={priorAccountingTool}>
						{#each ONBOARDING_ACCOUNTING_TOOLS as t}
							<option value={t.value}>{t.label}</option>
						{/each}
					</select>
				</label>

				<div class="flex flex-col-reverse sm:flex-row gap-3 pt-2 border-t border-ink-100">
					<button type="button" class="btn-secondary flex-1" onclick={close}>Cancel</button>
					<button type="submit" class="btn-primary flex-1" disabled={!formValid || creating}>
						{creating ? 'Creating…' : 'Create organisation'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}
