<script lang="ts">
	import { page } from '$app/stores';
	import BankRuleForm from '$lib/components/BankRuleForm.svelte';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import type { BankRuleType } from '$lib/types';

	const initialType = $derived(
		((): BankRuleType => {
			const t = $page.url.searchParams.get('type')?.toUpperCase();
			if (t === 'RECEIVE' || t === 'TRANSFER' || t === 'SPEND') return t;
			return 'SPEND';
		})()
	);
</script>

<div class="w-full space-y-6">
	<p class="text-sm mb-1">
		<a href="/app/accounting" class="text-brand-600 hover:underline">Accounting</a>
		/
		<a href="/app/accounting/bank-rules" class="text-brand-600 hover:underline">Bank rules</a>
	</p>

	<ModuleHeader title="Create rule" subtitle="Define when the rule runs and how transactions are coded." />

	<BankRuleForm ruleId={null} initialRuleType={initialType} />
</div>
