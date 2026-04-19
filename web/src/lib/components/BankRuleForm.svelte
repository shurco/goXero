<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { accountApi, bankRuleApi, contactApi, taxRateApi } from '$lib/api';
	import type { Account, BankRule, BankRuleAllocationLine, BankRuleCondition, BankRuleType, Contact, TaxRate } from '$lib/types';

	interface Props {
		ruleId: string | null;
		initialRuleType?: BankRuleType;
	}
	let { ruleId, initialRuleType = 'SPEND' }: Props = $props();

	const FIELD_OPTIONS = [
		{ value: 'ANY_TEXT', label: 'Any text field' },
		{ value: 'PAYEE', label: 'Payee' },
		{ value: 'REFERENCE', label: 'Reference' },
		{ value: 'AMOUNT', label: 'Amount' }
	];
	const OP_OPTIONS = [
		{ value: 'contains', label: 'contains' },
		{ value: 'equals', label: 'equals' },
		{ value: 'starts_with', label: 'starts with' }
	];
	const REF_OPTIONS = [
		{ value: 'REFERENCE', label: 'Reference' },
		{ value: 'NARRATION', label: 'Description / narration' }
	];

	function emptyRule(t: BankRuleType): BankRule {
		return {
			RuleType: t,
			Name: '',
			Definition: {
				MatchMode: 'ANY',
				Conditions: [{ Field: 'ANY_TEXT', Operator: 'contains', Value: '' }],
				ContactMode: 'EXISTING_OR_NEW',
				FixedLines: [],
				PercentLines: [{ Description: '', Percent: 100 }],
				ReferenceField: 'REFERENCE',
				RunOn: 'ALL_BANK_ACCOUNTS',
				TransferTargetMode: 'RECONCILE_CHOOSE'
			}
		};
	}

	let rule = $state<BankRule>(emptyRule('SPEND'));
	let loading = $state(true);
	let saving = $state(false);

	$effect(() => {
		if (ruleId) return;
		rule = emptyRule(initialRuleType);
	});
	let err = $state('');
	let accounts = $state<Account[]>([]);
	let taxRates = $state<TaxRate[]>([]);
	let contacts = $state<Contact[]>([]);
	let contactQuery = $state('');

	const bankAccounts = $derived(accounts.filter((a) => a.Type === 'BANK'));

	const pctSum = $derived(
		(rule.Definition.PercentLines ?? []).reduce((s, l) => s + (Number(l.Percent) || 0), 0)
	);
	const pctOk = $derived(
		rule.RuleType === 'TRANSFER' || Math.abs(pctSum - 100) < 0.02
	);

	const filteredContacts = $derived.by(() => {
		const q = contactQuery.trim().toLowerCase();
		if (!q) return contacts.slice(0, 30);
		return contacts.filter((c) => c.Name.toLowerCase().includes(q)).slice(0, 40);
	});

	function setType(t: BankRuleType) {
		rule = emptyRule(t);
		contactQuery = '';
	}

	function addCondition() {
		const c = rule.Definition.Conditions ?? [];
		rule.Definition.Conditions = [...c, { Field: 'ANY_TEXT', Operator: 'contains', Value: '' }];
	}
	function removeCondition(i: number) {
		const c = [...(rule.Definition.Conditions ?? [])];
		c.splice(i, 1);
		rule.Definition.Conditions = c.length ? c : [{ Field: 'ANY_TEXT', Operator: 'contains', Value: '' }];
	}

	function addFixed() {
		const lines = [...(rule.Definition.FixedLines ?? [])];
		lines.push({ Description: '', Amount: 0 });
		rule.Definition.FixedLines = lines;
	}
	function removeFixed(i: number) {
		const lines = [...(rule.Definition.FixedLines ?? [])];
		lines.splice(i, 1);
		rule.Definition.FixedLines = lines;
	}

	function addPercent() {
		const lines = [...(rule.Definition.PercentLines ?? [])];
		lines.push({ Description: '', Percent: 0 });
		rule.Definition.PercentLines = lines;
	}
	function removePercent(i: number) {
		const lines = [...(rule.Definition.PercentLines ?? [])];
		lines.splice(i, 1);
		rule.Definition.PercentLines = lines.length ? lines : [{ Percent: 100 }];
	}

	function pickContact(c: Contact) {
		rule.Definition.ContactID = c.ContactID;
		rule.Definition.ContactName = c.Name;
		contactQuery = c.Name;
	}

	async function loadBase() {
		const [accs, tr, ct] = await Promise.all([
			accountApi.list({ status: 'ACTIVE' }),
			taxRateApi.list(),
			contactApi.list({ page: '1', pageSize: '200' })
		]);
		accounts = accs;
		taxRates = tr;
		contacts = ct.Contacts ?? [];
	}

	onMount(async () => {
		try {
			await loadBase();
			if (ruleId) {
				const r = await bankRuleApi.get(ruleId);
				rule = r;
				contactQuery = r.Definition.ContactName ?? '';
			}
		} catch (e) {
			err = e instanceof Error ? e.message : 'Load failed';
		} finally {
			loading = false;
		}
	});

	async function save() {
		err = '';
		if (!rule.Name.trim()) {
			err = 'Name the rule.';
			return;
		}
		const conds = rule.Definition.Conditions ?? [];
		if (conds.some((c) => !(c.Value ?? '').trim())) {
			err = 'Fill in a value for each condition.';
			return;
		}
		if (!pctOk) {
			err = 'Percent lines must total 100%.';
			return;
		}
		saving = true;
		try {
			if (ruleId) {
				await bankRuleApi.update(ruleId, { ...rule, BankRuleID: ruleId, IsActive: true });
			} else {
				await bankRuleApi.create({ ...rule, IsActive: true });
			}
			await goto('/app/accounting/bank-rules');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	async function removeRule() {
		if (!ruleId || !confirm('Delete this bank rule?')) return;
		try {
			await bankRuleApi.delete(ruleId);
			await goto('/app/accounting/bank-rules');
		} catch (e) {
			err = e instanceof Error ? e.message : 'Delete failed';
		}
	}
</script>

{#if loading}
	<div class="card p-8 muted text-center">Loading…</div>
{:else}
	<div class="max-w-3xl space-y-6">
		{#if err}
			<p class="text-sm text-red-700" role="alert">{err}</p>
		{/if}

		{#if !ruleId}
			<div class="flex flex-wrap gap-0 border-b border-ink-200">
				{#each ['SPEND', 'RECEIVE', 'TRANSFER'] as t}
					<button
						type="button"
						class="px-4 py-2.5 text-sm font-medium border-b-2 -mb-px transition-colors {rule.RuleType === t
							? 'border-brand-500 text-brand-700'
							: 'border-transparent text-ink-600 hover:text-ink-900'}"
						onclick={() => setType(t as BankRuleType)}
					>
						{t === 'SPEND'
							? 'Spend money rule'
							: t === 'RECEIVE'
								? 'Receive money rule'
								: 'Transfer money rule'}
					</button>
				{/each}
			</div>
		{/if}

		<div class="card p-6 space-y-8">
			<!-- 1 -->
			<section>
				<h3 class="text-sm font-semibold text-ink-900 mb-3">1. Apply a bank rule</h3>
				<label class="block max-w-xs mb-4" for="match-mode">
					<span class="label">Matching</span>
					<select id="match-mode" class="input w-full" bind:value={rule.Definition.MatchMode}>
						<option value="ANY">Any conditions match</option>
						<option value="ALL">All conditions match</option>
					</select>
				</label>
				<div class="overflow-x-auto border border-ink-100 rounded-lg">
					<table class="w-full text-sm">
						<thead>
							<tr class="bg-ink-50 text-left text-ink-600">
								<th class="px-3 py-2 font-medium">Field</th>
								<th class="px-3 py-2 font-medium">Condition</th>
								<th class="px-3 py-2 font-medium">Value</th>
								<th class="w-10"></th>
							</tr>
						</thead>
						<tbody>
							{#each rule.Definition.Conditions ?? [] as cond, i}
								<tr class="border-t border-ink-100">
									<td class="px-2 py-2">
										<select class="input py-1.5 text-sm w-full" bind:value={cond.Field}>
											{#each FIELD_OPTIONS as o}
												<option value={o.value}>{o.label}</option>
											{/each}
										</select>
									</td>
									<td class="px-2 py-2">
										<select class="input py-1.5 text-sm w-full" bind:value={cond.Operator}>
											{#each OP_OPTIONS as o}
												<option value={o.value}>{o.label}</option>
											{/each}
										</select>
									</td>
									<td class="px-2 py-2">
										<input class="input py-1.5 text-sm w-full" bind:value={cond.Value} />
									</td>
									<td class="px-1">
										<button
											type="button"
											class="text-ink-400 hover:text-red-600 text-lg leading-none"
											onclick={() => removeCondition(i)}
											aria-label="Remove condition">×</button
										>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<button type="button" class="mt-2 text-sm text-brand-600 hover:underline" onclick={addCondition}>
					Add condition
				</button>
			</section>

			{#if rule.RuleType === 'TRANSFER'}
				<!-- 2 transfer -->
				<section>
					<h3 class="text-sm font-semibold text-ink-900 mb-2">2. Create a transfer</h3>
					<p class="text-sm text-ink-600 mb-4">
						A transfer will be created between the selected account and the account being reconciled.
					</p>
					<label class="block mb-4" for="xfer-mode">
						<span class="label">Bank account</span>
						<select id="xfer-mode" class="input w-full max-w-md" bind:value={rule.Definition.TransferTargetMode}>
							<option value="RECONCILE_CHOOSE">Choose during bank reconciliation</option>
							<option value="FIXED">Fixed bank account</option>
						</select>
					</label>
					{#if rule.Definition.TransferTargetMode === 'FIXED'}
						<label class="block mb-4" for="xfer-acc">
							<span class="label">Target account</span>
							<select id="xfer-acc" class="input w-full max-w-md" bind:value={rule.Definition.TransferBankAccountID}>
								<option value="">Select account…</option>
								{#each bankAccounts as a}
									<option value={a.AccountID}>{a.Code} — {a.Name}</option>
								{/each}
							</select>
						</label>
					{/if}
					<label class="block max-w-md" for="xfer-track">
						<span class="label">Region (optional)</span>
						<input
							id="xfer-track"
							class="input w-full"
							bind:value={rule.Definition.TransferTrackingRegion}
							placeholder="Tracking / region"
						/>
					</label>
				</section>
			{:else}
				<!-- 2 contact -->
				<section>
					<h3 class="text-sm font-semibold text-ink-900 mb-3">2. Set the contact</h3>
					<div class="flex flex-col sm:flex-row gap-3 sm:items-start">
						<label class="block shrink-0" for="cmode">
							<span class="label">Contact</span>
							<select id="cmode" class="input w-full sm:w-64" bind:value={rule.Definition.ContactMode}>
								<option value="EXISTING_OR_NEW">To an existing or new contact</option>
								<option value="NO_CONTACT">No contact</option>
							</select>
						</label>
						<div class="flex-1 min-w-0">
							<span class="label">Search contact</span>
							<div class="relative">
								<input
									class="input w-full pl-9"
									bind:value={contactQuery}
									oninput={() => {
										rule.Definition.ContactID = undefined;
										rule.Definition.ContactName = contactQuery;
									}}
									placeholder="Type to search…"
									autocomplete="off"
								/>
								<svg
									class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-ink-400"
									viewBox="0 0 24 24"
									fill="none"
									stroke="currentColor"
									aria-hidden="true"
								>
									<path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2M12 11a4 4 0 100-8 4 4 0 000 8z" stroke-width="1.5" />
								</svg>
								{#if contactQuery && filteredContacts.length}
									<ul
										class="absolute z-10 mt-1 max-h-48 w-full overflow-auto rounded-md border border-ink-100 bg-white py-1 shadow-pop text-sm"
									>
										{#each filteredContacts as c}
											<li>
												<button
													type="button"
													class="w-full px-3 py-2 text-left hover:bg-ink-50"
													onclick={() => pickContact(c)}
												>
													{c.Name}
												</button>
											</li>
										{/each}
									</ul>
								{/if}
							</div>
							{#if rule.Definition.ContactID}
								<p class="text-xs text-ink-500 mt-1">Linked: {rule.Definition.ContactName}</p>
							{/if}
						</div>
					</div>
				</section>

				<!-- 3 allocate -->
				<section>
					<h3 class="text-sm font-semibold text-ink-900 mb-3">3. Allocate line items</h3>
					<p class="text-xs text-ink-500 mb-2">Automatically allocate fixed value line items</p>
					<div class="overflow-x-auto border border-ink-100 rounded-lg mb-4">
						<table class="w-full text-sm min-w-[640px]">
							<thead>
								<tr class="bg-ink-50 text-ink-600 text-left">
									<th class="px-2 py-2 font-medium">Description</th>
									<th class="px-2 py-2 font-medium">Account</th>
									<th class="px-2 py-2 font-medium">Tax rate</th>
									<th class="px-2 py-2 font-medium">Region</th>
									<th class="px-2 py-2 font-medium w-24">Amount</th>
									<th class="w-8"></th>
								</tr>
							</thead>
							<tbody>
								{#each rule.Definition.FixedLines ?? [] as line, i}
									<tr class="border-t border-ink-100">
										<td class="px-1 py-1"
											><input class="input py-1 text-sm w-full" bind:value={line.Description} /></td
										>
										<td class="px-1 py-1">
											<select class="input py-1 text-sm w-full min-w-[140px]" bind:value={line.AccountID}>
												<option value="">—</option>
												{#each accounts as a}
													<option value={a.AccountID}>{a.Code} — {a.Name}</option>
												{/each}
											</select>
										</td>
										<td class="px-1 py-1">
											<select class="input py-1 text-sm w-full min-w-[120px]" bind:value={line.TaxRateID}>
												<option value="">—</option>
												{#each taxRates as tr}
													<option value={tr.TaxRateID}>{tr.Name}</option>
												{/each}
											</select>
										</td>
										<td class="px-1 py-1"
											><input class="input py-1 text-sm w-20" bind:value={line.Region} /></td
										>
										<td class="px-1 py-1">
											<input
												type="number"
												step="0.01"
												class="input py-1 text-sm w-full"
												bind:value={line.Amount}
											/>
										</td>
										<td
											><button type="button" class="text-ink-400 hover:text-red-600" onclick={() => removeFixed(i)}
												>×</button
											></td
										>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
					<button type="button" class="text-sm text-brand-600 hover:underline mb-6" onclick={addFixed}>
						Add a new line
					</button>

					<p class="text-xs text-ink-500 mb-2">With any remainder, allocate ratios using percent line items</p>
					<div class="overflow-x-auto border border-ink-100 rounded-lg">
						<table class="w-full text-sm min-w-[640px]">
							<thead>
								<tr class="bg-ink-50 text-ink-600 text-left">
									<th class="px-2 py-2 font-medium">Description</th>
									<th class="px-2 py-2 font-medium">Account</th>
									<th class="px-2 py-2 font-medium">Tax rate</th>
									<th class="px-2 py-2 font-medium">Region</th>
									<th class="px-2 py-2 font-medium w-24">Percent</th>
									<th class="w-8"></th>
								</tr>
							</thead>
							<tbody>
								{#each rule.Definition.PercentLines ?? [] as line, i}
									<tr class="border-t border-ink-100">
										<td class="px-1 py-1"
											><input class="input py-1 text-sm w-full" bind:value={line.Description} /></td
										>
										<td class="px-1 py-1">
											<select class="input py-1 text-sm w-full" bind:value={line.AccountID}>
												<option value="">—</option>
												{#each accounts as a}
													<option value={a.AccountID}>{a.Code} — {a.Name}</option>
												{/each}
											</select>
										</td>
										<td class="px-1 py-1">
											<select class="input py-1 text-sm w-full" bind:value={line.TaxRateID}>
												<option value="">—</option>
												{#each taxRates as tr}
													<option value={tr.TaxRateID}>{tr.Name}</option>
												{/each}
											</select>
										</td>
										<td class="px-1 py-1"
											><input class="input py-1 text-sm w-20" bind:value={line.Region} /></td
										>
										<td class="px-1 py-1">
											<input
												type="number"
												step="0.01"
												class="input py-1 text-sm w-full"
												bind:value={line.Percent}
											/>
										</td>
										<td
											><button type="button" class="text-ink-400 hover:text-red-600" onclick={() => removePercent(i)}
												>×</button
											></td
										>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
					<div class="flex justify-between items-center mt-2">
						<button type="button" class="text-sm text-brand-600 hover:underline" onclick={addPercent}>
							Add a new line
						</button>
						<span class="text-sm font-medium {pctOk ? 'text-ink-700' : 'text-red-600'}">
							Total {pctSum.toFixed(2)}%
						</span>
					</div>
				</section>
			{/if}

			<!-- reference -->
			<section>
				<h3 class="text-sm font-semibold text-ink-900 mb-3">
					{rule.RuleType === 'TRANSFER' ? '3' : '4'}. Set the reference
				</h3>
				<label class="block max-w-md" for="ref">
					<span class="label">Reference source</span>
					<select id="ref" class="input w-full" bind:value={rule.Definition.ReferenceField}>
						{#each REF_OPTIONS as o}
							<option value={o.value}>{o.label}</option>
						{/each}
					</select>
				</label>
			</section>

			<!-- details -->
			<section>
				<h3 class="text-sm font-semibold text-ink-900 mb-3">
					{rule.RuleType === 'TRANSFER' ? '4' : '5'}. Add rule details
				</h3>
				<label class="block mb-4 max-w-md" for="runon">
					<span class="label">Run this rule on</span>
					<select id="runon" class="input w-full" bind:value={rule.Definition.RunOn}>
						<option value="ALL_BANK_ACCOUNTS">All bank accounts</option>
						<option value="SPECIFIC_ACCOUNT">Specific bank account</option>
					</select>
				</label>
				{#if rule.Definition.RunOn === 'SPECIFIC_ACCOUNT'}
					<label class="block mb-4 max-w-md" for="scope">
						<span class="label">Bank account</span>
						<select id="scope" class="input w-full" bind:value={rule.Definition.ScopeBankAccountID}>
							<option value="">Select…</option>
							{#each bankAccounts as a}
								<option value={a.AccountID}>{a.Code} — {a.Name}</option>
							{/each}
						</select>
					</label>
				{/if}
				<label class="block max-w-md" for="rname">
					<span class="label">Name the rule</span>
					<input id="rname" class="input w-full" bind:value={rule.Name} placeholder="e.g. 7-Eleven" />
				</label>
			</section>

			<details class="rounded-lg border border-ink-100 p-4">
				<summary class="text-sm font-medium text-ink-800 cursor-pointer">History and notes</summary>
				<p class="text-sm text-ink-500 mt-3">Notes and audit history will appear here in a future update.</p>
			</details>
		</div>

		<div class="flex flex-wrap items-center justify-between gap-3">
			<div>
				{#if ruleId}
					<button type="button" class="text-sm font-medium text-red-600 hover:underline" onclick={removeRule}>
						Delete
					</button>
				{/if}
			</div>
			<div class="flex gap-2">
				<button type="button" class="btn-secondary" onclick={() => goto('/app/accounting/bank-rules')}>Cancel</button>
				<button type="button" class="btn-primary" disabled={saving || !pctOk} onclick={() => void save()}>
					{saving ? 'Saving…' : 'Save'}
				</button>
			</div>
		</div>
	</div>
{/if}
