<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { accountApi } from '$lib/api';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import BankAccountForm, {
		type BankAccountFormValues
	} from '$lib/components/BankAccountForm.svelte';
	import type { Account } from '$lib/types';

	const id = $derived($page.params.id ?? '');

	let account = $state<Account | null>(null);
	let initial = $state<BankAccountFormValues | null>(null);
	let loading = $state(true);
	let saving = $state(false);
	let err = $state('');
	let msg = $state('');

	async function reload() {
		if (!id) return;
		loading = true;
		err = '';
		try {
			const a = await accountApi.get(id);
			account = a ?? null;
			initial = {
				accountType: (a?.BankAccountType as 'BANK' | 'CREDITCARD') ?? 'BANK',
				name: a?.Name ?? '',
				code: a?.Code ?? '',
				bankAccountNumber: a?.BankAccountNumber ?? '',
				currencyCode: a?.CurrencyCode ?? 'USD',
				description: a?.Description ?? '',
				status: (a?.Status as 'ACTIVE' | 'ARCHIVED') ?? 'ACTIVE'
			};
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load bank account';
		} finally {
			loading = false;
		}
	}

	onMount(reload);

	async function submit(v: BankAccountFormValues) {
		err = '';
		msg = '';
		if (!v.name) {
			err = 'Name is required.';
			return;
		}
		saving = true;
		try {
			await accountApi.update(id, {
				Name: v.name,
				Code: v.code || undefined,
				BankAccountNumber: v.bankAccountNumber || undefined,
				CurrencyCode: v.currencyCode,
				Description: v.description || undefined,
				Status: v.status,
				BankAccountType: v.accountType
			});
			msg = 'Saved.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to save';
		} finally {
			saving = false;
		}
	}

	async function archive() {
		if (!confirm('Archive this bank account? You can restore it later.')) return;
		try {
			await accountApi.update(id, { Status: 'ARCHIVED' });
			await goto('/app/accounting/bank-accounts');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to archive';
		}
	}

	async function remove() {
		if (
			!confirm(
				'Permanently delete this bank account? This cannot be undone and only works when the account has no transactions.'
			)
		)
			return;
		try {
			await accountApi.delete(id);
			await goto('/app/accounting/bank-accounts');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to delete';
		}
	}
</script>

<p class="text-sm mb-2">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
	/
	<a href="/app/accounting/bank-accounts/{id}" class="text-brand-600 hover:underline">
		{account?.Name ?? '…'}
	</a>
</p>

<ModuleHeader title="Manage bank account" subtitle="Rename, archive or delete this account." />

<div class="card p-6 max-w-2xl">
	{#if loading || !initial}
		<p class="muted">Loading…</p>
	{:else}
		<BankAccountForm
			mode="edit"
			{initial}
			{saving}
			error={err}
			message={msg}
			cancelHref={`/app/accounting/bank-accounts/${id}`}
			onSubmit={submit}
			onArchive={archive}
			onDelete={remove}
		/>
	{/if}
</div>
