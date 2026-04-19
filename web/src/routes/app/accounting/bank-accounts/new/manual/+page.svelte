<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { accountApi, orgApi } from '$lib/api';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import BankAccountForm, {
		type BankAccountFormValues
	} from '$lib/components/BankAccountForm.svelte';
	import type { Organisation } from '$lib/types';

	const initialName = $page.url.searchParams.get('name') ?? '';

	let initial = $state<BankAccountFormValues>({
		accountType: 'BANK',
		name: initialName,
		code: '',
		bankAccountNumber: '',
		currencyCode: 'USD',
		description: '',
		status: 'ACTIVE'
	});

	let saving = $state(false);
	let err = $state('');

	onMount(async () => {
		try {
			const org: Organisation | null = await orgApi.current();
			if (org?.BaseCurrency) initial = { ...initial, currencyCode: org.BaseCurrency };
		} catch {
			// keep USD default on failure — the form stays editable
		}
	});

	async function submit(v: BankAccountFormValues) {
		err = '';
		if (!v.name) {
			err = 'Account name is required.';
			return;
		}
		saving = true;
		try {
			const res = await accountApi.create({
				Name: v.name,
				Type: 'BANK',
				BankAccountType: v.accountType,
				CurrencyCode: v.currencyCode,
				Status: 'ACTIVE',
				Description: v.description || undefined,
				Code: v.code || undefined,
				BankAccountNumber: v.bankAccountNumber || undefined
			});
			const newId = res?.Accounts?.[0]?.AccountID ?? '';
			await goto(
				newId ? `/app/accounting/bank-accounts/${newId}` : '/app/accounting/bank-accounts'
			);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to create bank account';
		} finally {
			saving = false;
		}
	}
</script>

<ModuleHeader
	title="Add bank account"
	subtitle="Add a bank or credit-card account without a live feed."
/>

<p class="text-sm mb-4">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
	/ <a href="/app/accounting/bank-accounts/new" class="text-brand-600 hover:underline">Add</a>
</p>

<div class="card p-6 max-w-2xl">
	<BankAccountForm
		mode="create"
		{initial}
		{saving}
		error={err}
		cancelHref="/app/accounting/bank-accounts"
		onSubmit={submit}
	/>
</div>
