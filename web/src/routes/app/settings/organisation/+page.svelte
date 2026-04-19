<script lang="ts">
	import { onMount } from 'svelte';
	import { orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import type { Organisation, OrganisationProfile } from '$lib/types';

	let loading = $state(true);
	let saving = $state(false);
	let org = $state<Organisation | null>(null);
	let showExtra = $state(false);
	let successMsg = $state('');
	let errorMsg = $state('');

	interface OrgForm {
		displayName: string;
		legalName: string;
		industry: string;
		organisationType: string;
		registrationNumber: string;
		description: string;
		postalStreet: string;
		postalCity: string;
		postalState: string;
		postalZip: string;
		postalCountry: string;
		postalAttention: string;
		sameAsPostal: boolean;
		physStreet: string;
		physCity: string;
		physState: string;
		physZip: string;
		physCountry: string;
		physAttention: string;
		telephoneCountry: string;
		telephone: string;
		email: string;
		website: string;
		mobileCountry: string;
		mobile: string;
		faxCountry: string;
		fax: string;
		facebook: string;
		twitter: string;
		linkedin: string;
	}

	const ORG_TYPE_OPTIONS = [
		'Company',
		'Sole Trader',
		'Partnership',
		'Trust',
		'Non-profit'
	] as const;

	const LABEL_TO_API: Record<string, string> = {
		Company: 'COMPANY',
		'Sole Trader': 'SOLE_TRADER',
		Partnership: 'PARTNERSHIP',
		Trust: 'TRUST',
		'Non-profit': 'NON_PROFIT'
	};

	const API_TO_LABEL: Record<string, string> = {
		COMPANY: 'Company',
		SOLE_TRADER: 'Sole Trader',
		PARTNERSHIP: 'Partnership',
		TRUST: 'Trust',
		NON_PROFIT: 'Non-profit'
	};

	function blank(): OrgForm {
		return {
			displayName: '',
			legalName: '',
			industry: '',
			organisationType: 'Company',
			registrationNumber: '',
			description: '',
			postalStreet: '',
			postalCity: '',
			postalState: '',
			postalZip: '',
			postalCountry: '',
			postalAttention: '',
			sameAsPostal: false,
			physStreet: '',
			physCity: '',
			physState: '',
			physZip: '',
			physCountry: '',
			physAttention: '',
			telephoneCountry: '+1',
			telephone: '',
			email: '',
			website: '',
			mobileCountry: '+1',
			mobile: '',
			faxCountry: '+1',
			fax: '',
			facebook: '',
			twitter: '',
			linkedin: ''
		};
	}

	let form = $state<OrgForm>(blank());

	function applyOrgToForm(o: Organisation) {
		const p = o.Profile ?? {};
		const postal = p.Postal ?? {};
		const physical = p.Physical ?? {};
		const tel = p.Telephone ?? {};
		const mob = p.Mobile ?? {};
		const fax = p.Fax ?? {};
		const soc = p.Social ?? {};

		const typeLabel = API_TO_LABEL[(o.OrganisationType || 'COMPANY').toUpperCase()] || 'Company';

		form = {
			...blank(),
			displayName: o.Name,
			legalName: o.LegalName || '',
			organisationType: ORG_TYPE_OPTIONS.includes(typeLabel as (typeof ORG_TYPE_OPTIONS)[number])
				? (typeLabel as OrgForm['organisationType'])
				: 'Company',
			registrationNumber: o.RegistrationNumber || '',
			industry: o.LineOfBusiness || '',
			description: o.Description || '',
			postalStreet: postal.AddressLine1 || '',
			postalCity: postal.City || '',
			postalState: postal.Region || '',
			postalZip: postal.PostalCode || '',
			postalCountry: postal.Country || o.CountryCode || '',
			postalAttention: postal.Attention || '',
			sameAsPostal: p.SameAsPostal ?? false,
			physStreet: physical.AddressLine1 || '',
			physCity: physical.City || '',
			physState: physical.Region || '',
			physZip: physical.PostalCode || '',
			physCountry: physical.Country || '',
			physAttention: physical.Attention || '',
			telephoneCountry: tel.PhoneCountryCode || '+1',
			telephone: tel.PhoneNumber || '',
			email: p.Email || '',
			website: p.Website || '',
			mobileCountry: mob.PhoneCountryCode || '+1',
			mobile: mob.PhoneNumber || '',
			faxCountry: fax.PhoneCountryCode || '+1',
			fax: fax.PhoneNumber || '',
			facebook: soc.Facebook || '',
			twitter: soc.Twitter || '',
			linkedin: soc.LinkedIn || ''
		};
		showExtra = p.ShowExtraOnInvoices ?? false;

		if (form.sameAsPostal) {
			form.physStreet = form.postalStreet;
			form.physCity = form.postalCity;
			form.physState = form.postalState;
			form.physZip = form.postalZip;
			form.physCountry = form.postalCountry;
			form.physAttention = form.postalAttention;
		}
	}

	function buildProfile(): OrganisationProfile {
		const postal = {
			AddressLine1: form.postalStreet.trim(),
			City: form.postalCity.trim(),
			Region: form.postalState.trim(),
			PostalCode: form.postalZip.trim(),
			Country: form.postalCountry.trim(),
			Attention: form.postalAttention.trim()
		};
		const physical = form.sameAsPostal
			? { ...postal }
			: {
					AddressLine1: form.physStreet.trim(),
					City: form.physCity.trim(),
					Region: form.physState.trim(),
					PostalCode: form.physZip.trim(),
					Country: form.physCountry.trim(),
					Attention: form.physAttention.trim()
				};

		return {
			ShowExtraOnInvoices: showExtra,
			SameAsPostal: form.sameAsPostal,
			Postal: postal,
			Physical: physical,
			Telephone: {
				PhoneCountryCode: form.telephoneCountry.trim(),
				PhoneNumber: form.telephone.trim()
			},
			Mobile: {
				PhoneCountryCode: form.mobileCountry.trim(),
				PhoneNumber: form.mobile.trim()
			},
			Fax: {
				PhoneCountryCode: form.faxCountry.trim(),
				PhoneNumber: form.fax.trim()
			},
			Email: form.email.trim(),
			Website: form.website.trim(),
			Social: {
				Facebook: form.facebook.trim(),
				Twitter: form.twitter.trim(),
				LinkedIn: form.linkedin.trim()
			},
			EmailTemplates: org?.Profile?.EmailTemplates ?? [],
			ReplyAddresses: org?.Profile?.ReplyAddresses ?? []
		};
	}

	async function reload() {
		loading = true;
		errorMsg = '';
		try {
			org = (await orgApi.current()) ?? null;
			if (org) applyOrgToForm(org);
		} catch (e) {
			errorMsg = e instanceof Error ? e.message : 'Failed to load organisation';
		} finally {
			loading = false;
		}
	}

	onMount(reload);
	$effect(() => {
		if ($session.tenantId) void reload();
	});

	$effect(() => {
		if (form.sameAsPostal) {
			form.physStreet = form.postalStreet;
			form.physCity = form.postalCity;
			form.physState = form.postalState;
			form.physZip = form.postalZip;
			form.physCountry = form.postalCountry;
			form.physAttention = form.postalAttention;
		}
	});

	async function save() {
		if (!org) return;
		saving = true;
		errorMsg = '';
		successMsg = '';
		try {
			const apiType = LABEL_TO_API[form.organisationType] ?? 'COMPANY';
			const updated = await orgApi.update({
				Name: form.displayName.trim(),
				LegalName: form.legalName.trim(),
				OrganisationType: apiType,
				CountryCode: form.postalCountry.trim() || org.CountryCode || '',
				LineOfBusiness: form.industry.trim(),
				RegistrationNumber: form.registrationNumber.trim(),
				Description: form.description.trim(),
				Timezone: org.Timezone || 'UTC',
				TaxNumber: org.TaxNumber || '',
				Profile: buildProfile()
			});
			org = updated;
			if (updated) applyOrgToForm(updated);
			successMsg = 'Organisation details saved.';
			window.setTimeout(() => {
				successMsg = '';
			}, 5000);
		} catch (e) {
			errorMsg =
				e instanceof Error ? e.message : 'Save failed. Check your connection and try again.';
		} finally {
			saving = false;
		}
	}

	function cancel() {
		void reload();
		successMsg = '';
		errorMsg = '';
	}
</script>

<div class="w-full space-y-6">
	<p class="text-sm mb-1">
		<a href="/app/settings" class="text-brand-600 hover:underline">Settings</a>
	</p>

	<ModuleHeader
		title="Organisation details"
		subtitle="Update how your business appears on documents, invoices and in goXero."
	/>

	{#if successMsg}
		<p class="text-sm text-emerald-800" role="status">{successMsg}</p>
	{/if}
	{#if errorMsg}
		<p class="text-sm text-red-700" role="alert">{errorMsg}</p>
	{/if}

	{#if loading}
		<div class="card p-8 flex flex-col items-center justify-center gap-3 text-ink-500">
			<span
				class="inline-block h-8 w-8 animate-spin rounded-full border-2 border-brand-500 border-t-transparent"
				aria-hidden="true"
			></span>
			<span class="text-sm">Loading organisation…</span>
		</div>
	{:else}
		<form
			id="org-settings-form"
			class="space-y-6"
			onsubmit={(e) => {
				e.preventDefault();
				void save();
			}}
		>
			<!-- Same pattern as /app/sales: separate cards, no single outer frame -->
			<div class="card p-5">
				<h2 class="content-section-title mb-4">Online invoices</h2>
				<div class="flex flex-col sm:flex-row sm:items-start gap-4">
					<label class="relative inline-flex h-8 w-14 shrink-0 cursor-pointer items-center">
						<input type="checkbox" bind:checked={showExtra} class="peer sr-only" />
						<span
							class="h-8 w-14 rounded-full bg-ink-200 transition peer-focus-visible:ring-2 peer-focus-visible:ring-brand-400 peer-checked:bg-brand-500"
						></span>
						<span
							class="pointer-events-none absolute left-1 top-1 h-6 w-6 rounded-full bg-white shadow transition peer-checked:translate-x-6"
						></span>
					</label>
					<p class="text-sm text-ink-700 leading-relaxed">
						<span class="font-medium text-ink-900">Show extra organisation details</span>
						on customer-facing online invoices. Save to apply.
					</p>
				</div>
			</div>

			<div class="grid grid-cols-1 gap-6 lg:grid-cols-2 lg:items-start">
				<div class="card p-5">
					<h2 class="content-section-title mb-4">Basic information</h2>
					<div class="form-grid-2">
						<div class="sm:col-span-2">
							<label class="label flex items-center gap-1" for="org-logo">
								Logo
								<span
									class="inline-flex h-4 w-4 items-center justify-center rounded-full bg-ink-200 text-[10px] text-ink-600"
									title="Logo appears on invoices and quotes">?</span
								>
							</label>
							<div class="flex flex-col sm:flex-row sm:items-center gap-4">
								<div
									class="h-20 w-20 shrink-0 rounded-lg border-2 border-dashed border-ink-200 bg-ink-50 flex items-center justify-center text-ink-400"
								>
									<svg width="32" height="32" viewBox="0 0 24 24" fill="none" aria-hidden="true">
										<rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.25" />
										<circle cx="8.5" cy="10" r="1.5" fill="currentColor" />
										<path
											d="M21 17l-5-5-5 5-3-3-5 5"
											stroke="currentColor"
											stroke-width="1.25"
											stroke-linecap="round"
										/>
									</svg>
								</div>
								<div class="min-w-0">
									<button id="org-logo" class="btn-secondary" type="button" disabled title="Coming soon">
										Upload logo
									</button>
									<p class="muted text-xs mt-1.5">PNG or JPG, max 1&nbsp;MB.</p>
								</div>
							</div>
						</div>

						<div>
							<label class="label" for="org-display">Display name <span class="muted">(required)</span></label>
							<input id="org-display" class="input" bind:value={form.displayName} required autocomplete="organization" />
						</div>
						<div>
							<label class="label" for="org-legal">Legal / Trading name <span class="muted">(required)</span></label>
							<input id="org-legal" class="input" bind:value={form.legalName} required />
						</div>

						<div>
							<label class="label" for="org-industry">Industry</label>
							<input
								id="org-industry"
								class="input"
								bind:value={form.industry}
								placeholder="e.g. Construction, retail, services"
							/>
						</div>
						<div>
							<label class="label" for="org-type">Organisation type <span class="muted">(required)</span></label>
							<select id="org-type" class="select" bind:value={form.organisationType}>
								{#each ORG_TYPE_OPTIONS as opt}
									<option value={opt}>{opt}</option>
								{/each}
							</select>
						</div>

						<div class="sm:col-span-2">
							<label class="label" for="org-reg">Business registration number</label>
							<input
								id="org-reg"
								class="input"
								bind:value={form.registrationNumber}
								placeholder="Official number to appear on your documents"
							/>
						</div>

						<div class="sm:col-span-2">
							<label class="label" for="org-desc">Organisation description</label>
							<textarea
								id="org-desc"
								class="textarea"
								bind:value={form.description}
								placeholder="Short summary of what your business does"
							></textarea>
						</div>
					</div>
				</div>

				<div class="card p-5">
					<h2 class="content-section-title mb-4">Contact information</h2>
					<div class="space-y-5">
						<div>
							<label class="label" for="org-postal-street">Postal address</label>
							<input
								id="org-postal-street"
								class="input"
								bind:value={form.postalStreet}
								placeholder="Street or PO Box"
							/>
							<input class="input mt-2" bind:value={form.postalCity} placeholder="Town / City" />
						</div>

						<div class="form-grid-2">
							<div>
								<label class="label" for="org-postal-state">State / Region</label>
								<input id="org-postal-state" class="input" bind:value={form.postalState} />
							</div>
							<div>
								<label class="label" for="org-postal-zip">Postal / ZIP</label>
								<input id="org-postal-zip" class="input" bind:value={form.postalZip} />
							</div>
						</div>

						<div>
							<label class="label" for="org-postal-country">Country</label>
							<input
								id="org-postal-country"
								class="input uppercase"
								bind:value={form.postalCountry}
								placeholder="US, GB, NZ…"
								maxlength="2"
							/>
						</div>

						<div>
							<label class="label" for="org-postal-attn">Attention</label>
							<input id="org-postal-attn" class="input" bind:value={form.postalAttention} />
						</div>

						<div class="rounded-lg bg-ink-50/80 p-4">
							<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-3">
								<span class="text-sm font-semibold text-ink-900">Physical address</span>
								<label class="flex items-center gap-2 text-sm text-ink-700 cursor-pointer">
									<input
										type="checkbox"
										bind:checked={form.sameAsPostal}
										class="rounded border-ink-300 text-brand-600 focus:ring-brand-500"
									/>
									Same as postal
								</label>
							</div>
							<input
								id="org-phys-street"
								class="input"
								bind:value={form.physStreet}
								placeholder="Street address"
								disabled={form.sameAsPostal}
							/>
							<input
								class="input mt-2"
								bind:value={form.physCity}
								placeholder="Town / City"
								disabled={form.sameAsPostal}
							/>
							<div class="form-grid-2 mt-3">
								<div>
									<label class="label" for="org-phys-state">State / Region</label>
									<input id="org-phys-state" class="input" bind:value={form.physState} disabled={form.sameAsPostal} />
								</div>
								<div>
									<label class="label" for="org-phys-zip">Postal / ZIP</label>
									<input id="org-phys-zip" class="input" bind:value={form.physZip} disabled={form.sameAsPostal} />
								</div>
								<div class="sm:col-span-2">
									<label class="label" for="org-phys-country">Country</label>
									<input
										id="org-phys-country"
										class="input uppercase"
										bind:value={form.physCountry}
										disabled={form.sameAsPostal}
									/>
								</div>
								<div class="sm:col-span-2">
									<label class="label" for="org-phys-attn">Attention</label>
									<input id="org-phys-attn" class="input" bind:value={form.physAttention} disabled={form.sameAsPostal} />
								</div>
							</div>
						</div>

						<div class="form-grid-2">
							<div class="sm:col-span-2">
								<label class="label" for="org-phone">Telephone</label>
								<div class="flex flex-col gap-2 sm:flex-row">
									<input
										class="input w-full sm:w-24 shrink-0"
										bind:value={form.telephoneCountry}
										aria-label="Country code"
									/>
									<input
										id="org-phone"
										class="input flex-1 min-w-0"
										bind:value={form.telephone}
										placeholder="Phone number"
										type="tel"
									/>
								</div>
							</div>
							<div>
								<label class="label" for="org-email">Email</label>
								<input id="org-email" type="email" class="input" bind:value={form.email} autocomplete="email" />
							</div>
							<div>
								<label class="label" for="org-website">Website</label>
								<input id="org-website" class="input" bind:value={form.website} placeholder="https://" type="url" />
							</div>
							<div class="sm:col-span-2">
								<label class="label" for="org-mobile">Mobile</label>
								<div class="flex flex-col gap-2 sm:flex-row">
									<input class="input w-full sm:w-24" bind:value={form.mobileCountry} aria-label="Mobile country code" />
									<input id="org-mobile" class="input flex-1 min-w-0" bind:value={form.mobile} type="tel" />
								</div>
							</div>
							<div class="sm:col-span-2">
								<label class="label" for="org-fax">Fax</label>
								<div class="flex flex-col gap-2 sm:flex-row">
									<input class="input w-full sm:w-24" bind:value={form.faxCountry} aria-label="Fax country code" />
									<input id="org-fax" class="input flex-1 min-w-0" bind:value={form.fax} type="tel" />
								</div>
							</div>
						</div>

						<div class="border-t border-ink-100 pt-4">
							<h3 class="text-base font-semibold text-ink-900 mb-3">Social</h3>
							<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
								<div>
									<label class="label" for="org-fb">Facebook</label>
									<div
										class="flex min-w-0 rounded-md border border-ink-200 overflow-hidden focus-within:ring-2 focus-within:ring-brand-100 focus-within:border-brand-500"
									>
										<span class="shrink-0 bg-ink-50 px-2.5 py-2 text-xs text-ink-500 border-r border-ink-200 truncate"
											>facebook.com/</span
										>
										<input id="org-fb" class="input !border-0 !rounded-none !shadow-none min-w-0 flex-1" bind:value={form.facebook} />
									</div>
								</div>
								<div>
									<label class="label" for="org-tw">X (Twitter)</label>
									<div
										class="flex min-w-0 rounded-md border border-ink-200 overflow-hidden focus-within:ring-2 focus-within:ring-brand-100 focus-within:border-brand-500"
									>
										<span class="shrink-0 bg-ink-50 px-2.5 py-2 text-xs text-ink-500 border-r border-ink-200 truncate"
											>twitter.com/</span
										>
										<input id="org-tw" class="input !border-0 !rounded-none !shadow-none min-w-0 flex-1" bind:value={form.twitter} />
									</div>
								</div>
								<div class="sm:col-span-2">
									<label class="label" for="org-li">LinkedIn</label>
									<div
										class="flex min-w-0 rounded-md border border-ink-200 overflow-hidden focus-within:ring-2 focus-within:ring-brand-100 focus-within:border-brand-500"
									>
										<span class="shrink-0 bg-ink-50 px-2.5 py-2 text-xs text-ink-500 border-r border-ink-200 truncate"
											>linkedin.com/</span
										>
										<input id="org-li" class="input !border-0 !rounded-none !shadow-none min-w-0 flex-1" bind:value={form.linkedin} />
									</div>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>

			<div class="card p-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
				<p class="text-xs muted order-2 sm:order-1">
					{#if org?.UpdatedDateUTC}
						Last saved {new Date(org.UpdatedDateUTC).toLocaleString()}
					{:else}
						Save all changes
					{/if}
				</p>
				<div class="flex flex-wrap gap-2 order-1 sm:order-2 sm:justify-end">
					<button type="button" class="btn-secondary" onclick={cancel} disabled={saving}>Cancel</button>
					<button type="submit" class="btn-primary min-w-[7rem]" disabled={saving}>
						{saving ? 'Saving…' : 'Save'}
					</button>
				</div>
			</div>
		</form>
	{/if}
</div>
