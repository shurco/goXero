<script lang="ts">
	import { accountApi, taxRateApi } from '$lib/api';
	import { ACCOUNT_TYPE_OPTIONS } from '$lib/chart-of-accounts';
	import type { Account, TaxRate } from '$lib/types';
	import AccountReportsExplainer from '$lib/components/AccountReportsExplainer.svelte';

	interface Props {
		open: boolean;
		mode: 'add' | 'edit';
		account: Account | null;
		onClose: () => void;
		onSaved: () => void;
	}
	let { open, mode, account, onClose, onSaved }: Props = $props();

	let saving = $state(false);
	let errorMsg = $state<string | null>(null);
	let taxRates = $state<TaxRate[]>([]);

	let code = $state('');
	let name = $state('');
	let type = $state('EXPENSE');
	let description = $state('');
	let bankAccountNumber = $state('');
	let taxType = $state('');
	let status = $state<'ACTIVE' | 'ARCHIVED'>('ACTIVE');
	let enablePayments = $state(false);
	let showInExpenseClaims = $state(false);
	let showOnDashboard = $state(false);

	function classForAccountType(t: string): string {
		const asset = new Set([
			'BANK',
			'CURRENT',
			'FIXED',
			'INVENTORY',
			'NONCURRENT',
			'PREPAYMENT'
		]);
		const liab = new Set([
			'CURRLIAB',
			'LIABILITY',
			'TERMLIAB',
			'PAYGLIABILITY',
			'SUPERANNUATIONLIABILITY'
		]);
		if (asset.has(t)) return 'ASSET';
		if (liab.has(t)) return 'LIABILITY';
		if (t === 'EQUITY') return 'EQUITY';
		if (t === 'REVENUE' || t === 'SALES') return 'REVENUE';
		return 'EXPENSE';
	}

	function uniqueTaxOptions(rates: TaxRate[]): TaxRate[] {
		const seen = new Set<string>();
		const out: TaxRate[] = [];
		for (const r of rates) {
			if (seen.has(r.TaxType)) continue;
			seen.add(r.TaxType);
			out.push(r);
		}
		return out;
	}

	function defaultTaxType(rates: TaxRate[]): string {
		const u = uniqueTaxOptions(rates);
		const exempt = u.find((r) => /exempt|none/i.test(r.Name) || r.TaxType === 'NONE');
		return exempt?.TaxType ?? u[0]?.TaxType ?? 'NONE';
	}

	function persistWatchlist(codeStr: string) {
		if (!showOnDashboard || mode !== 'add' || !codeStr.trim()) return;
		try {
			const raw = localStorage.getItem('coa_watchlist_codes');
			const arr: string[] = raw ? JSON.parse(raw) : [];
			if (!arr.includes(codeStr.trim())) {
				arr.push(codeStr.trim());
				localStorage.setItem('coa_watchlist_codes', JSON.stringify(arr));
			}
		} catch {
			/* ignore */
		}
	}

	$effect(() => {
		if (!open) return;
		errorMsg = null;
		void taxRateApi
			.list()
			.then((r) => {
				taxRates = Array.isArray(r) ? r : [];
			})
			.catch(() => {
				taxRates = [];
			});

		if (mode === 'edit' && account) {
			code = account.Code ?? '';
			name = account.Name ?? '';
			type = account.Type ?? 'EXPENSE';
			description = account.Description ?? '';
			bankAccountNumber = account.BankAccountNumber ?? '';
			taxType = account.TaxType ?? '';
			status = (account.Status === 'ARCHIVED' ? 'ARCHIVED' : 'ACTIVE') as 'ACTIVE' | 'ARCHIVED';
			enablePayments = !!account.EnablePaymentsToAccount;
			showInExpenseClaims = !!account.ShowInExpenseClaims;
			showOnDashboard = false;
		} else {
			code = '';
			name = '';
			type = 'EXPENSE';
			description = '';
			bankAccountNumber = '';
			taxType = '';
			status = 'ACTIVE';
			enablePayments = false;
			showInExpenseClaims = false;
			showOnDashboard = false;
		}
	});

	$effect(() => {
		if (!open || mode !== 'add') return;
		if (taxRates.length === 0) return;
		if (taxType) return;
		taxType = defaultTaxType(taxRates);
	});

	async function submit(e: Event) {
		e.preventDefault();
		errorMsg = null;
		if (!code.trim() || !name.trim() || !type) {
			errorMsg = 'Account type, code and name are required.';
			return;
		}
		saving = true;
		try {
			const payload: Partial<Account> = {
				Code: code.trim(),
				Name: name.trim(),
				Type: type,
				Description: description.trim() || undefined,
				BankAccountNumber: type === 'BANK' ? bankAccountNumber.trim() || undefined : undefined,
				BankAccountType: type === 'BANK' ? 'BANK' : undefined,
				TaxType: taxType.trim() || undefined,
				Status: status,
				Class: classForAccountType(type),
				EnablePaymentsToAccount: enablePayments,
				ShowInExpenseClaims: showInExpenseClaims
			};
			if (mode === 'add') {
				await accountApi.create(payload);
				persistWatchlist(code.trim());
			} else if (account) {
				await accountApi.update(account.AccountID, { ...account, ...payload });
			}
			onSaved();
			onClose();
		} catch (e) {
			errorMsg = (e as Error).message || 'Save failed';
		} finally {
			saving = false;
		}
	}

	const taxSelectOptions = $derived(uniqueTaxOptions(taxRates));
	const isBank = $derived(type === 'BANK');
</script>

{#if open}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-ink-900/40 p-3 sm:p-4"
		role="presentation"
		onclick={(e) => e.target === e.currentTarget && onClose()}
	>
		<div
			class="card flex max-h-[92vh] w-full max-w-4xl flex-col overflow-hidden shadow-pop"
			role="dialog"
			aria-modal="true"
			aria-labelledby="acc-form-title"
			tabindex="-1"
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => e.stopPropagation()}
		>
			<div class="flex items-start justify-between gap-3 border-b border-ink-100 px-4 py-3 sm:px-5">
				<h2 id="acc-form-title" class="text-base font-semibold tracking-tight text-ink-900">
					{mode === 'add' ? 'Add New Account' : 'Edit account'}
				</h2>
				<button
					type="button"
					class="rounded-md p-1.5 text-ink-500 transition hover:bg-ink-100 hover:text-ink-800"
					aria-label="Close"
					onclick={onClose}
				>
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
			</div>

			{#if errorMsg}
				<p class="border-b border-red-100 bg-red-50 px-4 py-2 text-sm text-red-800 sm:px-5">{errorMsg}</p>
			{/if}

			<div class="flex min-h-0 flex-1 flex-col lg:flex-row">
				<div class="min-w-0 flex-1 overflow-y-auto px-4 py-4 sm:px-5">
					<form class="space-y-3" onsubmit={submit} id="account-form">
						<label class="block">
							<span class="label">Account Type</span>
							<select class="select" bind:value={type} required>
								{#each ACCOUNT_TYPE_OPTIONS as o (o.value)}
									<option value={o.value}>{o.label}</option>
								{/each}
							</select>
						</label>

						<label class="block">
							<span class="label">Code</span>
							<input class="input font-mono" bind:value={code} required maxlength={10} />
						</label>

						<label class="block">
							<span class="label">Name</span>
							<input class="input" bind:value={name} required maxlength={150} />
						</label>

						<label class="block">
							<span class="label">Description <span class="font-normal text-ink-500">(optional)</span></span>
							<textarea class="textarea min-h-[64px]" bind:value={description} maxlength={2000}></textarea>
						</label>

						{#if isBank}
							<label class="block">
								<span class="label">Bank account number</span>
								<input class="input font-mono" bind:value={bankAccountNumber} placeholder="Optional" />
							</label>
						{/if}

						<label class="block">
							<span class="label">Tax</span>
							<select class="select" bind:value={taxType}>
								{#if taxSelectOptions.length === 0}
									<option value="NONE">Tax Exempt (0%)</option>
									<option value="OUTPUT">Tax on Sales</option>
									<option value="INPUT">Tax on Purchases</option>
								{:else}
									{#each taxSelectOptions as tr (tr.TaxRateID)}
										<option value={tr.TaxType}>
											{tr.Name}
											({typeof tr.DisplayTaxRate === 'number'
												? tr.DisplayTaxRate
												: tr.DisplayTaxRate ?? tr.EffectiveRate}%)
										</option>
									{/each}
								{/if}
							</select>
						</label>

						{#if mode === 'edit'}
							<label class="block">
								<span class="label">Status</span>
								<select class="select" bind:value={status}>
									<option value="ACTIVE">Active</option>
									<option value="ARCHIVED">Archived</option>
								</select>
							</label>
						{/if}

						<div class="space-y-2 border-t border-ink-100 pt-3">
							<label class="flex cursor-pointer items-start gap-2 text-sm text-ink-800">
								<input
									type="checkbox"
									class="mt-0.5 h-4 w-4 shrink-0 rounded border-ink-300 text-brand-600 focus:ring-brand-500"
									bind:checked={showOnDashboard}
								/>
								<span>Show on Dashboard Watchlist</span>
							</label>
							<label class="flex cursor-pointer items-start gap-2 text-sm text-ink-800">
								<input
									type="checkbox"
									class="mt-0.5 h-4 w-4 shrink-0 rounded border-ink-300 text-brand-600 focus:ring-brand-500"
									bind:checked={showInExpenseClaims}
								/>
								<span>Show in Expense Claims</span>
							</label>
							<label class="flex cursor-pointer items-start gap-2 text-sm text-ink-800">
								<input
									type="checkbox"
									class="mt-0.5 h-4 w-4 shrink-0 rounded border-ink-300 text-brand-600 focus:ring-brand-500"
									bind:checked={enablePayments}
								/>
								<span>Enable payments to this account</span>
							</label>
						</div>
					</form>
				</div>

				<div
					class="flex min-h-[200px] shrink-0 justify-center border-t border-ink-100 lg:min-h-0 lg:w-[min(340px,38vw)] lg:max-w-none lg:border-l lg:border-t-0 lg:border-ink-100"
				>
					<AccountReportsExplainer />
				</div>
			</div>

			<div
				class="flex flex-col gap-2 border-t border-ink-100 bg-ink-50/50 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5"
			>
				<div class="flex flex-wrap items-center gap-2">
					<button type="submit" form="account-form" class="btn-primary min-w-[92px] py-2 text-sm" disabled={saving}>
						{saving ? 'Saving…' : 'Save'}
					</button>
					<button type="button" class="btn-secondary py-2 text-sm" onclick={onClose}>Cancel</button>
				</div>
				<p class="text-[11px] leading-snug text-ink-500 lg:max-w-[240px] lg:text-right">
					Report layout:
					<a href="/app/settings/financial" class="text-brand-600 underline hover:text-brand-700">Settings → Financial</a>
				</p>
			</div>
		</div>
	</div>
{/if}
