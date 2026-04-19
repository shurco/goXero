<script lang="ts">
	import { page } from '$app/stores';
	import BankRuleForm from '$lib/components/BankRuleForm.svelte';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	const id = $derived($page.params.id ?? '');
</script>

<div class="w-full space-y-6">
	<p class="text-sm mb-1">
		<a href="/app/accounting" class="text-brand-600 hover:underline">Accounting</a>
		/
		<a href="/app/accounting/bank-rules" class="text-brand-600 hover:underline">Bank rules</a>
	</p>

	<ModuleHeader title="Edit rule" subtitle="Update conditions, allocation, and where this rule applies." />

	{#if id}
		{#key id}
			<BankRuleForm ruleId={id} />
		{/key}
	{/if}
</div>
