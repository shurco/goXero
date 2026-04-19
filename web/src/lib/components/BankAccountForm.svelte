<script lang="ts">
	import { BANK_CURRENCIES } from '$lib/bank-brand';

	type Mode = 'create' | 'edit';

	export interface BankAccountFormValues {
		accountType: 'BANK' | 'CREDITCARD';
		name: string;
		code: string;
		bankAccountNumber: string;
		currencyCode: string;
		description: string;
		status: 'ACTIVE' | 'ARCHIVED';
	}

	interface Props {
		mode: Mode;
		initial: BankAccountFormValues;
		saving?: boolean;
		error?: string;
		message?: string;
		cancelHref: string;
		onSubmit: (values: BankAccountFormValues) => void | Promise<void>;
		onArchive?: () => void | Promise<void>;
		onDelete?: () => void | Promise<void>;
	}

	let {
		mode,
		initial,
		saving = false,
		error = '',
		message = '',
		cancelHref,
		onSubmit,
		onArchive,
		onDelete
	}: Props = $props();

	// The parent passes `initial` once the form is ready to mount; we snapshot
	// the values into local reactive state so the user can edit freely.
	// svelte-ignore state_referenced_locally
	let accountType = $state(initial.accountType);
	// svelte-ignore state_referenced_locally
	let name = $state(initial.name);
	// svelte-ignore state_referenced_locally
	let code = $state(initial.code);
	// svelte-ignore state_referenced_locally
	let bankAccountNumber = $state(initial.bankAccountNumber);
	// svelte-ignore state_referenced_locally
	let currencyCode = $state(initial.currencyCode);
	// svelte-ignore state_referenced_locally
	let description = $state(initial.description);
	// svelte-ignore state_referenced_locally
	let status = $state(initial.status);

	function handleSubmit(e: SubmitEvent) {
		e.preventDefault();
		void onSubmit({
			accountType,
			name: name.trim(),
			code: code.trim(),
			bankAccountNumber: bankAccountNumber.trim(),
			currencyCode,
			description: description.trim(),
			status
		});
	}
</script>

{#if error}
	<p class="text-sm text-red-700 mb-3" role="alert">{error}</p>
{/if}
{#if message}
	<p class="text-sm text-emerald-700 mb-3">{message}</p>
{/if}

<form class="space-y-4" onsubmit={handleSubmit}>
	<fieldset class="space-y-2">
		<legend class="label">Account type</legend>
		<div class="flex gap-2">
			<label
				class="inline-flex items-center gap-2 border border-ink-200 rounded-md px-3 py-2 cursor-pointer flex-1"
				class:border-brand-500={accountType === 'BANK'}
				class:bg-brand-50={accountType === 'BANK'}
			>
				<input type="radio" bind:group={accountType} value="BANK" />
				<span>Everyday bank account</span>
			</label>
			<label
				class="inline-flex items-center gap-2 border border-ink-200 rounded-md px-3 py-2 cursor-pointer flex-1"
				class:border-brand-500={accountType === 'CREDITCARD'}
				class:bg-brand-50={accountType === 'CREDITCARD'}
			>
				<input type="radio" bind:group={accountType} value="CREDITCARD" />
				<span>Credit card</span>
			</label>
		</div>
	</fieldset>

	<label class="block">
		<span class="label">Account name *</span>
		<input
			class="input"
			type="text"
			bind:value={name}
			placeholder="Business Bank Account"
			required
		/>
		{#if mode === 'create'}
			<span class="text-xs muted mt-1 block">Shown to everyone in this organisation.</span>
		{/if}
	</label>

	<div class="grid sm:grid-cols-2 gap-4">
		<label class="block">
			<span class="label">Account number</span>
			<input
				class="input tabular-nums"
				type="text"
				bind:value={bankAccountNumber}
				placeholder="090-8007-006543"
				autocomplete="off"
			/>
		</label>
		<label class="block">
			<span class="label">Account code</span>
			<input class="input" type="text" bind:value={code} placeholder="090" />
		</label>
	</div>

	<label class="block">
		<span class="label">Currency</span>
		<select class="select" bind:value={currencyCode}>
			{#each BANK_CURRENCIES as c (c)}
				<option value={c}>{c}</option>
			{/each}
		</select>
	</label>

	<label class="block">
		<span class="label">Description</span>
		<textarea
			class="textarea"
			bind:value={description}
			rows="2"
			placeholder="Optional description for reporting."
		></textarea>
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

	<div class="flex flex-wrap gap-2 pt-2">
		<button class="btn-primary" type="submit" disabled={saving}>
			{saving ? 'Saving…' : 'Save'}
		</button>
		<a class="btn-secondary" href={cancelHref}>Cancel</a>
		{#if mode === 'edit'}
			<span class="flex-1"></span>
			{#if onArchive}
				<button type="button" class="btn-secondary" onclick={() => onArchive?.()}>
					Archive
				</button>
			{/if}
			{#if onDelete}
				<button type="button" class="btn-danger" onclick={() => onDelete?.()}>Delete</button>
			{/if}
		{/if}
	</div>
</form>
