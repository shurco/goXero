<script lang="ts">
	import { page } from '$app/stores';
	import BankRuleForm from '$lib/components/BankRuleForm.svelte';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import type { BankRule, BankRuleType } from '$lib/types';

	const initialType = $derived(
		((): BankRuleType => {
			const t = $page.url.searchParams.get('type')?.toUpperCase();
			if (t === 'RECEIVE' || t === 'TRANSFER' || t === 'SPEND') return t;
			return 'SPEND';
		})()
	);

	/**
	 * A rule carried in from somewhere else — today, from a statement line on the
	 * reconcile screen via "Create bank rule". The payee becomes the condition,
	 * the reference becomes the reference field, and the account the line was
	 * being coded to becomes where the rule sends its matches. Everything else
	 * is left at the defaults, because those are the parts the user should be
	 * deciding rather than inheriting.
	 */
	const initialRule = $derived.by((): BankRule | null => {
		const q = $page.url.searchParams;
		const payee = (q.get('payee') ?? '').trim();
		const reference = (q.get('reference') ?? '').trim();
		const description = (q.get('description') ?? '').trim();
		const accountId = (q.get('accountId') ?? '').trim();
		const scope = (q.get('bankAccountId') ?? '').trim();
		if (!payee && !reference) return null;
		const type = initialType;
		return {
			RuleType: type,
			Name: payee || reference,
			Definition: {
				MatchMode: 'ANY',
				Conditions: [
					payee
						? { Field: 'PAYEE', Operator: 'contains', Value: payee }
						: { Field: 'REFERENCE', Operator: 'contains', Value: reference }
				],
				ContactMode: 'EXISTING_OR_NEW',
				FixedLines: [],
				PercentLines:
					type === 'TRANSFER'
						? []
						: [{ Description: description || payee, AccountID: accountId || undefined, Percent: 100 }],
				ReferenceField: reference ? 'REFERENCE' : 'NARRATION',
				// A rule written from one account's statement line is almost always
				// about that account, so it is scoped to it unless the user widens it.
				RunOn: scope ? 'SPECIFIC_ACCOUNT' : 'ALL_BANK_ACCOUNTS',
				ScopeBankAccountID: scope || undefined,
				TransferTargetMode: 'RECONCILE_CHOOSE'
			}
		};
	});
</script>

<div class="w-full space-y-6">
	<p class="text-sm mb-1">
		<a href="/app/accounting" class="text-brand-600 hover:underline">Accounting</a>
		/
		<a href="/app/accounting/bank-rules" class="text-brand-600 hover:underline">Bank rules</a>
	</p>

	<ModuleHeader title="Create rule" subtitle="Define when the rule runs and how transactions are coded." />

	<BankRuleForm ruleId={null} initialRuleType={initialType} {initialRule} />
</div>
