<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		accountApi,
		bankTransactionApi,
		orgApi,
		reconcilePeriodApi,
		statementApi,
		statementImportApi,
		taxRateApi
	} from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatCurrency, formatDate } from '$lib/utils/format';
	import type {
		Account,
		BankReconcilePeriod,
		BankStatementLine,
		BankTransaction,
		Organisation,
		StatementBalance,
		StatementImport,
		StatementLineCoding,
		TaxRate
	} from '$lib/types';

	/**
	 * The reconcile screen for one bank account. Every number on it comes from
	 * the statement-line inbox (`/statement-lines`) rather than from the ledger:
	 * a line the bank sent is not a transaction until the user codes it, and
	 * treating the two as the same thing is what makes a reconcile screen lie
	 * about how much is left to do.
	 */
	type TabId = 'reconcile' | 'cash-coding' | 'statements' | 'transactions' | 'period';

	const TABS: { id: TabId; label: string }[] = [
		{ id: 'reconcile', label: 'Reconcile' },
		{ id: 'cash-coding', label: 'Cash coding' },
		{ id: 'statements', label: 'Bank statements' },
		{ id: 'transactions', label: 'Account transactions' },
		{ id: 'period', label: 'Reconcile period' }
	];

	/** Which of the per-line panels is open, and for which line. */
	type PanelKind = 'create' | 'match' | 'transfer';
	interface Panel {
		kind: PanelKind;
		line: BankStatementLine;
	}

	interface Coding {
		AccountCode: string;
		TaxType: string;
		Description: string;
		Reference: string;
	}

	let accountId = $derived($page.params.id ?? '');
	let account = $state<Account | null>(null);
	let org = $state<Organisation | null>(null);
	let balance = $state<StatementBalance | null>(null);
	let inbox = $state<BankStatementLine[]>([]);
	let lines = $state<BankStatementLine[]>([]);
	let transactions = $state<BankTransaction[]>([]);
	let imports = $state<StatementImport[]>([]);
	let periods = $state<BankReconcilePeriod[]>([]);
	let accounts = $state<Account[]>([]);
	let taxRates = $state<TaxRate[]>([]);
	let loading = $state(true);
	let err = $state('');
	let notice = $state('');

	let activeTab = $state<TabId>('reconcile');
	let selected = $state<Record<string, boolean>>({});
	let coding = $state<Record<string, Coding>>({});
	let panel = $state<Panel | null>(null);
	let panelBusy = $state(false);
	let candidates = $state<BankTransaction[]>([]);
	let candidateSearch = $state('');
	let transferTo = $state('');
	let newPeriod = $state({ start: '', end: '', balance: '' });

	const currency = $derived(account?.CurrencyCode ?? org?.BaseCurrency ?? 'USD');
	const statementBalance = $derived(Number(balance?.StatementBalance ?? 0));
	const ledgerBalance = $derived(Number(balance?.LedgerBalance ?? 0));
	const difference = $derived(Number(balance?.Difference ?? 0));

	/** Accounts a line can be coded to — anything with a code, bank accounts included. */
	const codingAccounts = $derived(
		[...accounts].sort((a, b) => a.Code.localeCompare(b.Code, undefined, { numeric: true }))
	);
	/** The other side of a transfer. */
	const otherBankAccounts = $derived(
		accounts.filter((a) => a.Type === 'BANK' && a.AccountID !== accountId)
	);

	/**
	 * Statement lines oldest-first with a running total, which is how the bank
	 * prints them. A balance the bank itself sent wins over the running total —
	 * that number is the point of importing a statement.
	 */
	const statementRows = $derived.by(() => {
		const sorted = [...lines].sort((a, b) =>
			a.PostedAt < b.PostedAt ? -1 : a.PostedAt > b.PostedAt ? 1 : 0
		);
		let running = 0;
		return sorted.map((line) => {
			running += Number(line.Amount ?? 0);
			const fromBank = line.Balance != null;
			return { line, balance: fromBank ? Number(line.Balance) : running, fromBank };
		});
	});

	const selectedIds = $derived(
		Object.entries(selected)
			.filter(([, on]) => on)
			.map(([id]) => id)
	);

	$effect(() => {
		if ($session.tenantId && accountId) void reload();
		// No tenant (or no account yet) means nothing will load — don't sit on
		// the "Loading…" state forever.
		else loading = false;
	});

	$effect(() => {
		const q = ($page.url.searchParams.get('tab') ?? '') as TabId;
		if (TABS.some((t) => t.id === q)) activeTab = q;
	});

	function setTab(t: TabId) {
		activeTab = t;
		panel = null;
		const u = new URL($page.url.toString());
		u.searchParams.set('tab', t);
		void goto(u.pathname + '?' + u.searchParams.toString(), {
			replaceState: true,
			keepFocus: true,
			noScroll: true
		});
	}

	async function reload() {
		if (!accountId) return;
		loading = true;
		err = '';
		try {
			const [a, o, bal, box, all, tx, imp, per, accs, rates] = await Promise.all([
				accountApi.get(accountId),
				orgApi.current().catch(() => null),
				statementApi.balance(accountId).catch(() => null),
				statementApi
					.list({
						bankAccountId: accountId,
						unreconciled: 'true',
						suggestions: 'true',
						pageSize: '500'
					})
					.catch(() => ({ StatementLines: [] })),
				statementApi
					.list({ bankAccountId: accountId, unreconciled: 'false', pageSize: '500' })
					.catch(() => ({ StatementLines: [] })),
				bankTransactionApi
					.list({ bankAccountId: accountId, pageSize: '500' })
					.catch(() => ({ BankTransactions: [] })),
				statementImportApi
					.list({ bankAccountId: accountId, pageSize: '50' })
					.catch(() => ({ StatementImports: [] })),
				reconcilePeriodApi.list(accountId).catch(() => []),
				accountApi.list({ status: 'ACTIVE' }).catch(() => []),
				taxRateApi.list().catch(() => [])
			]);
			account = a ?? null;
			org = o ?? null;
			balance = bal?.Balance ?? null;
			inbox = box?.StatementLines ?? [];
			lines = all?.StatementLines ?? [];
			transactions = tx?.BankTransactions ?? [];
			imports = imp?.StatementImports ?? [];
			periods = per ?? [];
			accounts = accs ?? [];
			taxRates = rates ?? [];
			seedCoding(inbox);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load bank account';
		} finally {
			loading = false;
		}
	}

	/**
	 * Give every inbox line a coding row, keeping whatever the user has already
	 * typed and prefilling from the bank rule that matched it.
	 */
	function seedCoding(rows: BankStatementLine[]) {
		const next = { ...coding };
		for (const l of rows) {
			const existing = next[l.StatementLineID];
			if (existing) continue;
			const suggested = l.Suggestions?.[0];
			const acc = suggested?.AccountID
				? accounts.find((a) => a.AccountID === suggested.AccountID)
				: undefined;
			next[l.StatementLineID] = {
				AccountCode: l.AccountCode ?? acc?.Code ?? '',
				TaxType: l.TaxType ?? suggested?.TaxType ?? '',
				Description: l.Description ?? '',
				Reference: l.Reference ?? ''
			};
		}
		coding = next;
	}

	function setCoding(id: string, field: keyof Coding, value: string) {
		coding = { ...coding, [id]: { ...coding[id], [field]: value } };
	}

	function toggle(id: string) {
		selected = { ...selected, [id]: !selected[id] };
	}

	function toggleAll() {
		if (selectedIds.length > 0) {
			selected = {};
			return;
		}
		selected = Object.fromEntries(inbox.map((l) => [l.StatementLineID, true]));
	}

	function openPanel(kind: PanelKind, line: BankStatementLine) {
		panel = { kind, line };
		panelBusy = false;
		candidates = [];
		candidateSearch = '';
		transferTo = '';
		if (kind === 'match') void loadCandidates(line.StatementLineID);
	}

	async function loadCandidates(lineId: string, search = '') {
		panelBusy = true;
		err = '';
		try {
			const res = await statementApi.matches(lineId, search);
			candidates = res.BankTransactions ?? [];
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not look up matching transactions';
		} finally {
			panelBusy = false;
		}
	}

	async function act(fn: () => Promise<unknown>, message: string) {
		panelBusy = true;
		err = '';
		notice = '';
		try {
			await fn();
			notice = message;
			panel = null;
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'That did not work';
		} finally {
			panelBusy = false;
		}
	}

	function createFromPanel() {
		const p = panel;
		if (!p) return;
		const c = coding[p.line.StatementLineID];
		if (!c?.AccountCode) {
			err = 'Choose an account to code this line to.';
			return;
		}
		void act(
			() => statementApi.create(p.line.StatementLineID, c),
			'Transaction created and reconciled.'
		);
	}

	function matchTo(txId: string) {
		const p = panel;
		if (!p) return;
		void act(
			() => statementApi.match(p.line.StatementLineID, txId),
			'Matched to the existing transaction.'
		);
	}

	function transferFromPanel() {
		const p = panel;
		if (!p || !transferTo) {
			err = 'Choose the account the money moved to or from.';
			return;
		}
		void act(
			() =>
				statementApi.transfer(p.line.StatementLineID, { ToBankAccountID: transferTo }),
			'Transfer created and reconciled.'
		);
	}

	function ignoreLine(id: string) {
		void act(() => statementApi.ignore(id), 'Statement line ignored.');
	}

	function unignoreLine(id: string) {
		void act(() => statementApi.unignore(id), 'Statement line back in the inbox.');
	}

	async function ignoreSelected() {
		if (selectedIds.length === 0) {
			err = 'Select the lines you want to ignore first.';
			return;
		}
		err = '';
		try {
			await statementApi.bulk(selectedIds, 'IGNORE');
			notice = `Ignored ${selectedIds.length} statement line(s).`;
			selected = {};
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not ignore those lines';
		}
	}

	async function markReconciled(tx: BankTransaction) {
		if (!tx.BankTransactionID) return;
		err = '';
		try {
			// Flip the flag on the transaction itself. Creating a copy with the
			// flag set — which is what this screen used to do — books the money
			// twice, because a second transaction is a second set of ledger lines.
			await bankTransactionApi.reconcile(tx.BankTransactionID);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not update transaction';
		}
	}

	async function unreconcile(tx: BankTransaction) {
		if (!tx.BankTransactionID) return;
		err = '';
		try {
			await bankTransactionApi.reconcile(tx.BankTransactionID, false);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not update transaction';
		}
	}

	async function autoReconcile() {
		err = '';
		notice = '';
		panelBusy = true;
		try {
			const res = await statementApi.autoReconcile(accountId);
			notice =
				res.Matched > 0
					? `Matched ${res.Matched} of ${res.Scanned} statement lines. ${res.Remaining} left to reconcile.`
					: `Nothing could be matched with certainty. ${res.Remaining} line(s) still need you.`;
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Auto reconcile failed';
		} finally {
			panelBusy = false;
		}
	}

	/**
	 * Apply the bank rules to the selected lines — or to the whole inbox when
	 * nothing is selected, which is how the button in Xero behaves after a fresh
	 * import. Nothing is saved: this only fills the coding columns in.
	 */
	async function applyRule() {
		err = '';
		notice = '';
		try {
			const res = await statementApi.applyRule({
				BankAccountID: accountId,
				StatementLineIDs: selectedIds.length > 0 ? selectedIds : undefined
			});
			const next = { ...coding };
			let filled = 0;
			for (const l of res.StatementLines ?? []) {
				const s = l.Suggestions?.[0];
				if (!s) continue;
				const acc = s.AccountID ? accounts.find((a) => a.AccountID === s.AccountID) : undefined;
				next[l.StatementLineID] = {
					...(next[l.StatementLineID] ?? { Description: '', Reference: '' }),
					AccountCode: acc?.Code ?? l.AccountCode ?? next[l.StatementLineID]?.AccountCode ?? '',
					TaxType: s.TaxType ?? l.TaxType ?? next[l.StatementLineID]?.TaxType ?? ''
				};
				filled++;
			}
			coding = next;
			notice =
				filled > 0
					? `A rule filled in the coding for ${filled} line(s). Check it, then save.`
					: 'No rule matched those lines.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not apply the bank rules';
		}
	}

	async function saveAndReconcile() {
		err = '';
		notice = '';
		const target =
			selectedIds.length > 0
				? inbox.filter((l) => selectedIds.includes(l.StatementLineID))
				: inbox;
		// Only lines the user actually gave an account to are saved; the rest stay
		// in the inbox for the next pass.
		const rows: StatementLineCoding[] = [];
		for (const l of target) {
			const c = coding[l.StatementLineID];
			if (!c?.AccountCode) continue;
			rows.push({
				StatementLineID: l.StatementLineID,
				AccountCode: c.AccountCode,
				TaxType: c.TaxType || undefined,
				Description: c.Description || undefined,
				Reference: c.Reference || undefined
			});
		}
		if (rows.length === 0) {
			err = 'Code at least one line with an account before saving.';
			return;
		}
		panelBusy = true;
		try {
			const res = await statementApi.cashCode(rows);
			notice = `Coded and reconciled ${res.Coded} statement line(s).`;
			selected = {};
			coding = {};
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not save that coding';
		} finally {
			panelBusy = false;
		}
	}

	async function undoImport(imp: StatementImport) {
		err = '';
		notice = '';
		try {
			const res = await statementImportApi.undo(imp.ImportID);
			notice = `Removed ${res.Deleted} statement line(s) from ${imp.Filename}.`;
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not undo that import';
		}
	}

	async function createPeriod() {
		err = '';
		notice = '';
		if (!newPeriod.start || !newPeriod.end) {
			err = 'Pick a start and an end date for the period.';
			return;
		}
		try {
			await reconcilePeriodApi.create({
				BankAccountID: accountId,
				StartDate: newPeriod.start,
				EndDate: newPeriod.end,
				StatementBalance: newPeriod.balance === '' ? undefined : Number(newPeriod.balance)
			});
			notice = 'Reconcile period locked.';
			newPeriod = { start: '', end: '', balance: '' };
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not create that period';
		}
	}

	async function deletePeriod(p: BankReconcilePeriod) {
		err = '';
		try {
			await reconcilePeriodApi.delete(p.PeriodID);
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not delete that period';
		}
	}

	const stmtRange = $derived(
		balance?.LastStatementEnd
			? `Last statement ends ${formatDate(balance.LastStatementEnd)}`
			: 'No statement imported yet'
	);
	const syncInfo = $derived(
		balance?.LastSyncAt ? `Feed synced ${formatDate(balance.LastSyncAt)}` : 'No bank feed connected'
	);
</script>

<p class="text-sm mb-2">
	<a href="/app/accounting/bank-accounts" class="text-brand-600 hover:underline">Bank accounts</a>
</p>

<header class="card p-5 mb-4 flex flex-wrap items-start justify-between gap-4">
	<div>
		<h1 class="section-title">{account?.Name ?? '—'}</h1>
		<p class="text-sm muted tabular-nums mt-1">
			{account?.BankAccountNumber || account?.Code || '—'}
		</p>
		<p class="text-xs muted mt-1">{stmtRange} · {syncInfo}</p>
	</div>
	<div class="flex flex-wrap gap-6">
		<div>
			<div class="text-xs uppercase muted tracking-wide">Statement balance</div>
			<div class="text-xl tabular-nums font-semibold">
				{formatCurrency(statementBalance, currency)}
			</div>
		</div>
		<div>
			<div class="text-xs uppercase muted tracking-wide">Balance in {currency}</div>
			<div class="text-xl tabular-nums font-semibold">
				{formatCurrency(ledgerBalance, currency)}
			</div>
			{#if Math.abs(difference) > 0.004}
				<div class="text-xs text-amber-700 tabular-nums">
					{formatCurrency(difference, currency)} still to reconcile
				</div>
			{:else}
				<div class="text-xs text-emerald-700">Fully reconciled</div>
			{/if}
		</div>
		<div class="flex flex-wrap items-start gap-2">
			<a class="btn-primary" href="/app/accounting/bank-accounts/{accountId}/import">
				Import statement
			</a>
			<a class="btn-secondary-sm" href="/app/accounting/bank-accounts/{accountId}/edit">
				Manage account
			</a>
		</div>
	</div>
</header>

<nav class="flex flex-wrap gap-6 border-b border-ink-200 mb-5 text-sm">
	{#each TABS as t (t.id)}
		<button
			type="button"
			class="relative py-2 transition {activeTab === t.id
				? 'text-brand-600 font-semibold'
				: 'text-ink-600 hover:text-ink-900'}"
			onclick={() => setTab(t.id)}
		>
			{t.label}
			{#if t.id === 'reconcile' && inbox.length > 0}
				<span class="ml-1 text-xs">({inbox.length})</span>
			{/if}
			{#if activeTab === t.id}
				<span class="absolute left-0 right-0 -bottom-[1px] h-0.5 bg-brand-500"></span>
			{/if}
		</button>
	{/each}
</nav>

{#if err}
	<p class="text-sm text-red-700 mb-3" role="alert">{err}</p>
{/if}
{#if notice}
	<p class="text-sm text-emerald-700 mb-3">{notice}</p>
{/if}

{#if loading}
	<div class="card p-8 muted text-center">Loading…</div>
{:else if activeTab === 'reconcile'}
	<section class="card overflow-hidden">
		<header
			class="px-5 py-3 border-b border-ink-100 flex flex-wrap items-center justify-between gap-3"
		>
			<h2 class="content-section-title">
				Reconcile <span class="muted font-normal">({inbox.length})</span>
			</h2>
			<div class="flex flex-wrap items-center gap-2">
				<a class="btn-secondary-sm" href="/app/accounting/bank-rules">Bank rules</a>
				<button
					type="button"
					class="btn-secondary-sm"
					onclick={ignoreSelected}
					disabled={selectedIds.length === 0}
				>
					Ignore selected
				</button>
				<button type="button" class="btn-primary" onclick={autoReconcile} disabled={panelBusy}>
					{panelBusy ? 'Working…' : "Ok, let's reconcile"}
				</button>
			</div>
		</header>

		{#if inbox.length === 0}
			<p class="p-8 text-center muted">
				Nothing to reconcile — every statement line has been dealt with.
			</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="min-w-full text-sm">
					<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
						<tr>
							<th class="px-4 py-2 text-left w-8">
								<input
									type="checkbox"
									checked={selectedIds.length > 0 && selectedIds.length === inbox.length}
									onchange={toggleAll}
								/>
							</th>
							<th class="px-4 py-2 text-left">Date</th>
							<th class="px-4 py-2 text-left">Payee</th>
							<th class="px-4 py-2 text-left">Reference</th>
							<th class="px-4 py-2 text-left">Source</th>
							<th class="px-4 py-2 text-left">Suggested coding</th>
							<th class="px-4 py-2 text-right">Spent</th>
							<th class="px-4 py-2 text-right">Received</th>
							<th class="px-4 py-2 text-right">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each inbox as l (l.StatementLineID)}
							{@const amount = Number(l.Amount ?? 0)}
							<tr class="hover:bg-ink-50 align-top">
								<td class="px-4 py-2">
									<input
										type="checkbox"
										checked={!!selected[l.StatementLineID]}
										onchange={() => toggle(l.StatementLineID)}
									/>
								</td>
								<td class="px-4 py-2 tabular-nums">{formatDate(l.PostedAt)}</td>
								<td class="px-4 py-2">
									<div>{l.Payee || l.Counterparty || '—'}</div>
									{#if l.Description && l.Description !== l.Payee}
										<div class="text-xs muted">{l.Description}</div>
									{/if}
								</td>
								<td class="px-4 py-2">{l.Reference || ''}</td>
								<td class="px-4 py-2 muted">
									{l.Source === 'FEED' ? 'Bank feed' : 'Imported'}
								</td>
								<td class="px-4 py-2">
									{#if l.Suggestions?.length}
										<span class="text-xs text-brand-700">
											{l.Suggestions[0].RuleName}
										</span>
									{:else}
										<span class="muted">—</span>
									{/if}
								</td>
								<td class="px-4 py-2 text-right tabular-nums">
									{amount < 0 ? formatCurrency(Math.abs(amount), l.CurrencyCode ?? currency) : ''}
								</td>
								<td class="px-4 py-2 text-right tabular-nums">
									{amount >= 0 ? formatCurrency(amount, l.CurrencyCode ?? currency) : ''}
								</td>
								<td class="px-4 py-2 text-right whitespace-nowrap">
									<button
										type="button"
										class="btn-secondary-sm"
										onclick={() => openPanel('create', l)}
									>
										Create
									</button>
									<button
										type="button"
										class="btn-secondary-sm ml-1"
										onclick={() => openPanel('match', l)}
									>
										Match
									</button>
									<button
										type="button"
										class="btn-secondary-sm ml-1"
										onclick={() => openPanel('transfer', l)}
									>
										Transfer
									</button>
									<button
										type="button"
										class="btn-ghost-sm ml-1"
										onclick={() => ignoreLine(l.StatementLineID)}
									>
										Ignore
									</button>
								</td>
							</tr>

							{#if panel && panel.line.StatementLineID === l.StatementLineID}
								<tr class="bg-brand-50/40">
									<td colspan="9" class="px-4 py-4">
										{#if panel.kind === 'create'}
											<div class="grid sm:grid-cols-4 gap-3 items-end">
												<label class="text-sm">
													<span class="block muted mb-1">Account</span>
													<select
														class="input py-1"
														value={coding[l.StatementLineID]?.AccountCode ?? ''}
														onchange={(e) =>
															setCoding(l.StatementLineID, 'AccountCode', e.currentTarget.value)}
													>
														<option value="">Select an account</option>
														{#each codingAccounts as a (a.AccountID)}
															<option value={a.Code}>{a.Code} · {a.Name}</option>
														{/each}
													</select>
												</label>
												<label class="text-sm">
													<span class="block muted mb-1">Tax rate</span>
													<select
														class="input py-1"
														value={coding[l.StatementLineID]?.TaxType ?? ''}
														onchange={(e) => setCoding(l.StatementLineID, 'TaxType', e.currentTarget.value)}
													>
														<option value="">No tax</option>
														{#each taxRates as r (r.TaxRateID)}
															<option value={r.TaxType}>{r.Name}</option>
														{/each}
													</select>
												</label>
												<label class="text-sm">
													<span class="block muted mb-1">Reference</span>
													<input
														class="input py-1"
														value={coding[l.StatementLineID]?.Reference ?? ''}
														oninput={(e) =>
															setCoding(l.StatementLineID, 'Reference', e.currentTarget.value)}
													/>
												</label>
												<div class="flex gap-2">
													<button
														type="button"
														class="btn-primary"
														onclick={createFromPanel}
														disabled={panelBusy}
													>
														Create transaction
													</button>
													<button type="button" class="btn-secondary" onclick={() => (panel = null)}>
														Cancel
													</button>
												</div>
											</div>
										{:else if panel.kind === 'match'}
											<div class="flex flex-wrap items-center gap-2 mb-3">
												<input
													class="input py-1 max-w-xs"
													placeholder="Search transactions"
													value={candidateSearch}
													oninput={(e) => (candidateSearch = e.currentTarget.value)}
													onkeydown={(e) => {
														if (e.key === 'Enter')
															void loadCandidates(l.StatementLineID, candidateSearch);
													}}
												/>
												<button
													type="button"
													class="btn-secondary-sm"
													onclick={() => loadCandidates(l.StatementLineID, candidateSearch)}
												>
													Find &amp; match
												</button>
												<button type="button" class="btn-ghost-sm" onclick={() => (panel = null)}>
													Cancel
												</button>
											</div>
											{#if panelBusy}
												<p class="text-sm muted">Looking…</p>
											{:else if candidates.length === 0}
												<p class="text-sm muted">
													No unreconciled transaction in this account looks like this line. Create it
													instead.
												</p>
											{:else}
												<ul class="divide-y divide-ink-100 border border-ink-100 rounded-md bg-white">
													{#each candidates as c (c.BankTransactionID)}
														<li class="px-3 py-2 flex flex-wrap items-center justify-between gap-2">
															<div class="text-sm">
																<span class="tabular-nums">{formatDate(c.Date)}</span>
																<span class="ml-2">{c.Contact?.Name ?? c.Type}</span>
																{#if c.Reference}
																	<span class="ml-2 muted">{c.Reference}</span>
																{/if}
																{#if c.IsReconciled}
																	<span class="ml-2 text-xs text-emerald-700">already reconciled</span>
																{/if}
															</div>
															<div class="flex items-center gap-3">
																<span class="tabular-nums text-sm">
																	{formatCurrency(c.Total ?? 0, c.CurrencyCode ?? currency)}
																</span>
																<button
																	type="button"
																	class="btn-secondary-sm"
																	onclick={() => matchTo(c.BankTransactionID)}
																	disabled={panelBusy}
																>
																	Match
																</button>
															</div>
														</li>
													{/each}
												</ul>
											{/if}
										{:else}
											<div class="grid sm:grid-cols-3 gap-3 items-end">
												<label class="text-sm">
													<span class="block muted mb-1">
														{Number(l.Amount) < 0
															? 'Money went to'
															: 'Money came from'}
													</span>
													<select class="input py-1" bind:value={transferTo}>
														<option value="">Select an account</option>
														{#each otherBankAccounts as a (a.AccountID)}
															<option value={a.AccountID}>{a.Code} · {a.Name}</option>
														{/each}
													</select>
												</label>
												<div class="text-sm muted">
													{formatCurrency(Math.abs(Number(l.Amount)), currency)} on
													{formatDate(l.PostedAt)}
												</div>
												<div class="flex gap-2">
													<button
														type="button"
														class="btn-primary"
														onclick={transferFromPanel}
														disabled={panelBusy}
													>
														Create transfer
													</button>
													<button type="button" class="btn-secondary" onclick={() => (panel = null)}>
														Cancel
													</button>
												</div>
											</div>
											{#if otherBankAccounts.length === 0}
												<p class="text-sm muted mt-2">
													There is no other bank account to transfer to. Add one first.
												</p>
											{/if}
										{/if}
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>

			<footer class="px-5 py-3 bg-ink-50 text-sm muted flex flex-wrap gap-4">
				<span>{inbox.length} statement line(s) waiting</span>
				{#if balance}
					<span>
						{balance.ReconciledCount} reconciled · {balance.UnreconciledCount} unreconciled
					</span>
				{/if}
			</footer>
		{/if}
	</section>
{:else if activeTab === 'cash-coding'}
	<section class="card overflow-hidden">
		<header
			class="px-5 py-3 border-b border-ink-100 flex flex-wrap items-center justify-between gap-3"
		>
			<h2 class="content-section-title">Cash coding</h2>
			<div class="flex flex-wrap items-center gap-2">
				<button type="button" class="btn-secondary-sm" onclick={toggleAll}>
					{selectedIds.length > 0 ? 'Uncheck all' : 'Check all'}
				</button>
				<button type="button" class="btn-secondary-sm" onclick={applyRule}>
					Apply rule
				</button>
				<button
					type="button"
					class="btn-primary"
					onclick={saveAndReconcile}
					disabled={panelBusy}
				>
					Save &amp; Reconcile All
				</button>
			</div>
		</header>
		<p class="px-5 py-3 text-sm muted border-b border-ink-100">
			Code as many lines as you like in one pass. Nothing is posted until you save — and
			saving turns each coded line into a reconciled transaction.
		</p>
		{#if inbox.length === 0}
			<p class="p-8 text-center muted">No statement lines to code.</p>
		{:else}
			<div class="overflow-x-auto">
				<table class="min-w-full text-sm">
					<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
						<tr>
							<th class="px-3 py-2 text-left w-8">
								<input
									type="checkbox"
									checked={selectedIds.length > 0 && selectedIds.length === inbox.length}
									onchange={toggleAll}
								/>
							</th>
							<th class="px-3 py-2 text-left">Date</th>
							<th class="px-3 py-2 text-left">Payee</th>
							<th class="px-3 py-2 text-left">Reference</th>
							<th class="px-3 py-2 text-left">Account</th>
							<th class="px-3 py-2 text-left">Tax rate</th>
							<th class="px-3 py-2 text-right">Spent</th>
							<th class="px-3 py-2 text-right">Received</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each inbox as l (l.StatementLineID)}
							{@const amount = Number(l.Amount ?? 0)}
							<tr class="hover:bg-ink-50">
								<td class="px-3 py-2">
									<input
										type="checkbox"
										checked={!!selected[l.StatementLineID]}
										onchange={() => toggle(l.StatementLineID)}
									/>
								</td>
								<td class="px-3 py-2 tabular-nums">{formatDate(l.PostedAt)}</td>
								<td class="px-3 py-2">{l.Payee || l.Counterparty || '—'}</td>
								<td class="px-3 py-2">
									<input
										class="input py-1"
										value={coding[l.StatementLineID]?.Reference ?? ''}
										oninput={(e) => setCoding(l.StatementLineID, 'Reference', e.currentTarget.value)}
									/>
								</td>
								<td class="px-3 py-2">
									<select
										class="input py-1"
										value={coding[l.StatementLineID]?.AccountCode ?? ''}
										onchange={(e) =>
											setCoding(l.StatementLineID, 'AccountCode', e.currentTarget.value)}
									>
										<option value="">Select an account</option>
										{#each codingAccounts as a (a.AccountID)}
											<option value={a.Code}>{a.Code} · {a.Name}</option>
										{/each}
									</select>
								</td>
								<td class="px-3 py-2">
									<select
										class="input py-1"
										value={coding[l.StatementLineID]?.TaxType ?? ''}
										onchange={(e) => setCoding(l.StatementLineID, 'TaxType', e.currentTarget.value)}
									>
										<option value="">No tax</option>
										{#each taxRates as r (r.TaxRateID)}
											<option value={r.TaxType}>{r.Name}</option>
										{/each}
									</select>
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{amount < 0 ? formatCurrency(Math.abs(amount), currency) : ''}
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{amount >= 0 ? formatCurrency(amount, currency) : ''}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>
{:else if activeTab === 'statements'}
	<section class="card overflow-hidden mb-4">
		<header
			class="px-5 py-3 border-b border-ink-100 flex flex-wrap items-center justify-between gap-3"
		>
			<h2 class="content-section-title">Bank statements</h2>
			<a class="btn-primary" href="/app/accounting/bank-accounts/{accountId}/import">
				Import statement
			</a>
		</header>
		{#if statementRows.length === 0}
			<p class="p-8 text-center muted">
				No statement lines yet. Import a statement, or connect a bank feed to fill this in
				automatically.
			</p>
		{:else}
			<div class="overflow-x-auto max-h-[32rem]">
				<table class="min-w-full text-sm">
					<thead class="bg-ink-50 text-ink-500 text-xs uppercase sticky top-0">
						<tr>
							<th class="px-3 py-2 text-left">Date</th>
							<th class="px-3 py-2 text-left">Type</th>
							<th class="px-3 py-2 text-left">Payee</th>
							<th class="px-3 py-2 text-left">Reference</th>
							<th class="px-3 py-2 text-left">Source</th>
							<th class="px-3 py-2 text-right">Spent</th>
							<th class="px-3 py-2 text-right">Received</th>
							<th class="px-3 py-2 text-right">Balance</th>
							<th class="px-3 py-2 text-left">Status</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each statementRows as row (row.line.StatementLineID)}
							{@const amount = Number(row.line.Amount ?? 0)}
							<tr class="hover:bg-ink-50">
								<td class="px-3 py-2 tabular-nums">{formatDate(row.line.PostedAt)}</td>
								<td class="px-3 py-2">{amount < 0 ? 'Debit' : 'Credit'}</td>
								<td class="px-3 py-2">{row.line.Payee || row.line.Counterparty || '—'}</td>
								<td class="px-3 py-2">{row.line.Reference || ''}</td>
								<td class="px-3 py-2 muted">
									{row.line.Source === 'FEED' ? 'Bank feed' : 'Imported'}
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{amount < 0 ? formatCurrency(Math.abs(amount), row.line.CurrencyCode ?? currency) : ''}
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{amount >= 0 ? formatCurrency(amount, row.line.CurrencyCode ?? currency) : ''}
								</td>
								<td
									class="px-3 py-2 text-right tabular-nums"
									title={row.fromBank ? "Balance printed on the bank's statement" : 'Running total of the lines below'}
								>
									{formatCurrency(row.balance, row.line.CurrencyCode ?? currency)}
								</td>
								<td class="px-3 py-2">
									{#if row.line.Status === 'IMPORTED'}
										<span class="text-emerald-700">Reconciled</span>
									{:else if row.line.Status === 'IGNORED'}
										<span class="muted">Ignored</span>
										<button
											type="button"
											class="btn-ghost-sm ml-2"
											onclick={() => unignoreLine(row.line.StatementLineID)}
										>
											Restore
										</button>
									{:else}
										<span class="text-amber-600">Unreconciled</span>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<section class="card overflow-hidden">
		<header class="px-5 py-3 border-b border-ink-100">
			<h2 class="content-section-title">Imports</h2>
		</header>
		{#if imports.length === 0}
			<p class="p-6 text-center muted text-sm">Nothing has been imported into this account.</p>
		{:else}
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-4 py-2 text-left">File</th>
						<th class="px-4 py-2 text-left">Format</th>
						<th class="px-4 py-2 text-left">Imported</th>
						<th class="px-4 py-2 text-right">Lines</th>
						<th class="px-4 py-2 text-right">Duplicates held back</th>
						<th class="px-4 py-2 text-left">Status</th>
						<th class="px-4 py-2 text-right"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each imports as imp (imp.ImportID)}
						<tr class="hover:bg-ink-50">
							<td class="px-4 py-2">{imp.Filename ?? '—'}</td>
							<td class="px-4 py-2">{imp.Format}</td>
							<td class="px-4 py-2 tabular-nums">{formatDate(imp.CreatedDateUTC)}</td>
							<td class="px-4 py-2 text-right tabular-nums">{imp.LineCount}</td>
							<td class="px-4 py-2 text-right tabular-nums">{imp.DuplicateCount}</td>
							<td class="px-4 py-2">
								{#if imp.Status === 'STAGED'}
									<span class="text-amber-600">Staged</span>
								{:else}
									<span class="text-emerald-700">{imp.Status}</span>
								{/if}
							</td>
							<td class="px-4 py-2 text-right">
								{#if imp.Status !== 'STAGED'}
									<button type="button" class="btn-ghost-sm" onclick={() => undoImport(imp)}>
										Undo import
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
{:else if activeTab === 'transactions'}
	<section class="card overflow-hidden">
		<header
			class="px-5 py-3 border-b border-ink-100 flex flex-wrap items-center justify-between gap-3"
		>
			<h2 class="content-section-title">Account transactions</h2>
			<a class="btn-secondary-sm" href="/app/bank-transactions?accountId={accountId}">
				Open full view
			</a>
		</header>
		{#if transactions.length === 0}
			<p class="p-8 text-center muted">
				No transactions in this account yet. Coding a statement line creates one.
			</p>
		{:else}
			<table class="min-w-full text-sm">
				<thead class="bg-ink-50 text-ink-500 text-xs uppercase">
					<tr>
						<th class="px-3 py-2 text-left">Date</th>
						<th class="px-3 py-2 text-left">Contact</th>
						<th class="px-3 py-2 text-left">Reference</th>
						<th class="px-3 py-2 text-left">Account</th>
						<th class="px-3 py-2 text-right">Spent</th>
						<th class="px-3 py-2 text-right">Received</th>
						<th class="px-3 py-2 text-left">Status</th>
						<th class="px-3 py-2 text-right"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each transactions as t (t.BankTransactionID)}
						<tr class="hover:bg-ink-50">
							<td class="px-3 py-2 tabular-nums">{formatDate(t.Date)}</td>
							<td class="px-3 py-2">{t.Contact?.Name ?? t.Type}</td>
							<td class="px-3 py-2">{t.Reference ?? ''}</td>
							<td class="px-3 py-2">{t.LineItems?.[0]?.AccountCode ?? ''}</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'SPEND'}{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? currency)}{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'RECEIVE'}{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? currency)}{/if}
							</td>
							<td class="px-3 py-2">
								{#if t.IsReconciled}
									<span class="text-emerald-700">Reconciled</span>
								{:else}
									<span class="text-amber-600">Unreconciled</span>
								{/if}
							</td>
							<td class="px-3 py-2 text-right">
								{#if t.IsReconciled}
									<button type="button" class="btn-ghost-sm" onclick={() => unreconcile(t)}>
										Unreconcile
									</button>
								{:else}
									<button
										type="button"
										class="btn-secondary-sm"
										onclick={() => markReconciled(t)}
									>
										Reconcile
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
{:else if activeTab === 'period'}
	<section class="card p-6 space-y-4">
		<header class="flex flex-wrap items-center justify-between gap-3">
			<h2 class="content-section-title">Reconcile period</h2>
		</header>
		<p class="text-sm text-ink-700 bg-amber-50 border-l-4 border-amber-400 p-3 rounded">
			A reconcile period locks a range of dates once it has been reconciled with the bank. For
			the most accurate records it is best to start at the beginning of your financial year.
		</p>

		<div class="grid sm:grid-cols-4 gap-3 items-end">
			<label class="text-sm">
				<span class="block muted mb-1">Start date</span>
				<input type="date" class="input" bind:value={newPeriod.start} />
			</label>
			<label class="text-sm">
				<span class="block muted mb-1">End date</span>
				<input type="date" class="input" bind:value={newPeriod.end} />
			</label>
			<label class="text-sm">
				<span class="block muted mb-1">Statement balance</span>
				<input
					type="number"
					step="0.01"
					class="input tabular-nums"
					placeholder={statementBalance.toFixed(2)}
					bind:value={newPeriod.balance}
				/>
			</label>
			<button type="button" class="btn-primary" onclick={createPeriod}>Create period</button>
		</div>

		<div class="border border-ink-100 rounded-md">
			<header class="px-4 py-2 font-semibold text-sm bg-ink-50 rounded-t-md">All periods</header>
			{#if periods.length === 0}
				<p class="p-6 text-center muted text-sm">
					No periods have been reconciled yet. Reconciled periods let you lock ranges so that
					only authorised users can alter them.
				</p>
			{:else}
				<table class="min-w-full text-sm">
					<thead class="text-ink-500 text-xs uppercase">
						<tr>
							<th class="px-4 py-2 text-left">From</th>
							<th class="px-4 py-2 text-left">To</th>
							<th class="px-4 py-2 text-right">Statement balance</th>
							<th class="px-4 py-2 text-left">Created</th>
							<th class="px-4 py-2 text-right"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each periods as p (p.PeriodID)}
							<tr>
								<td class="px-4 py-2 tabular-nums">{formatDate(p.StartDate)}</td>
								<td class="px-4 py-2 tabular-nums">{formatDate(p.EndDate)}</td>
								<td class="px-4 py-2 text-right tabular-nums">
									{formatCurrency(p.StatementBalance, currency)}
								</td>
								<td class="px-4 py-2 tabular-nums muted">{formatDate(p.CreatedDateUTC)}</td>
								<td class="px-4 py-2 text-right">
									<button type="button" class="btn-ghost-sm" onclick={() => deletePeriod(p)}>
										Delete
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</div>
	</section>
{/if}
