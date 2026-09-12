<script lang="ts">
	import { browser } from '$app/environment';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { onDestroy, untrack } from 'svelte';
	import {
		accountApi,
		bankTransactionApi,
		contactApi,
		orgApi,
		reconcilePeriodApi,
		statementApi,
		statementImportApi,
		taxRateApi
	} from '$lib/api';
	import { session } from '$lib/stores/session';
	import {
		formatAmount,
		formatAmountCode,
		formatCodeAmount,
		formatCurrency,
		formatDate
	} from '$lib/utils/format';
	import type {
		Account,
		AutoReconcileReport,
		BankReconcilePeriod,
		BankRuleSuggestion,
		BankStatementLine,
		BankStatementLineComment,
		BankTransaction,
		Contact,
		Organisation,
		Pagination,
		PreviousEntrySuggestion,
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
	type PanelKind = 'create' | 'match' | 'transfer' | 'discuss';
	interface Panel {
		kind: PanelKind;
		line: BankStatementLine;
	}

	/**
	 * The tabs across the top of a row's right-hand column. They are the four
	 * things Xero offers on a statement line, and for the same reason: each is a
	 * different answer to "what is this?", so they sit side by side and only one
	 * is open at a time. Searching for a match is the fifth thing Xero puts
	 * there — a smaller tab at the far right of the same strip, because finding
	 * a match is the same question asked against the whole ledger.
	 */
	const lineTabs: { id: PanelKind; label: string }[] = [
		{ id: 'match', label: 'Match' },
		{ id: 'create', label: 'Create' },
		{ id: 'transfer', label: 'Transfer' },
		{ id: 'discuss', label: 'Discuss' }
	];

	/*
	 * DELIBERATE DEVIATION FROM XERO. Once a row is expanded Xero sets
	 * display:none on Create, Transfer *and* Find & Match, leaving only
	 * Match and Discuss reachable — so a line you have opened can no longer be
	 * created or transferred without collapsing it again. That is a bug in
	 * Xero, not a design, and copying it would take the two panels this screen
	 * exists for away at exactly the moment the user is looking at the line.
	 * All five tabs therefore stay visible and reachable in every row state.
	 */
	const MATCH_PAGE_SIZE = 25;

	/** The sortable columns of the step 1 results table, in Xero's order. */
	type MatchSortKey = 'Date' | 'Name' | 'Reference' | 'Spent' | 'Received';

	const matchColumns: { key: MatchSortKey; label: string }[] = [
		{ key: 'Date', label: 'Date' },
		{ key: 'Name', label: 'Name' },
		{ key: 'Reference', label: 'Reference' },
		{ key: 'Spent', label: 'Spent' },
		{ key: 'Received', label: 'Received' }
	];

	interface Coding {
		AccountCode: string;
		TaxType: string;
		Description: string;
		Reference: string;
		/**
		 * The "Who" field. Xero keeps whatever was typed and resolves it on
		 * save — matching an existing contact by name, or making one — so both
		 * halves are held here and ContactID is filled in at commit.
		 */
		ContactID?: string;
		ContactName?: string;
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
	/**
	 * The account whose data the screen is holding. The "Loading…" placeholder
	 * is for an account the page has nothing to show for yet; a reload of the
	 * account already on screen is a refresh and must not blank it. Plain state
	 * rather than $state on purpose — it is read while loading and never
	 * rendered, and reload runs from an $effect, which reactive state read
	 * inside it would only send round again.
	 */
	let loadedAccountId: string | null = null;
	let err = $state('');
	let notice = $state('');

	let activeTab = $state<TabId>('reconcile');
	let selected = $state<Record<string, boolean>>({});
	let coding = $state<Record<string, Coding>>({});
	let panel = $state<Panel | null>(null);
	let panelBusy = $state(false);
	/**
	 * The statement line whose "More details" panel is open. Xero's panel is
	 * per-line rather than per-tab, so one piece of state serves the reconcile
	 * list, the statement list and the transactions list alike.
	 */
	let details = $state<BankStatementLine | null>(null);
	/** Free text over the inbox — the search box above Xero's reconcile list. */
	let lineSearch = $state('');
	/**
	 * The transaction a jump from the statement list landed on. The highlight
	 * lasts until the next jump: it answers "which one was it?" for the one
	 * moment the question is being asked.
	 */
	let focusTransaction = $state<string | null>(null);
	/**
	 * Xero's "Suggest previous entries": prefill each line with the coding this
	 * account used last time it saw the same payee. On by default, and the
	 * choice is remembered per browser because it is a working preference, not
	 * a property of the books.
	 */
	let suggestPrevious = $state(readSuggestPrevious());
	/** Lines the user has typed in, which a re-seed must not overwrite. */
	let touched = $state<Record<string, boolean>>({});
	/**
	 * The transactions ticked in the Match panel's results table. Xero
	 * reconciles a statement line against *several* transactions at once when
	 * their total is the line — three invoices that add up to one deposit is
	 * the ordinary case — so it is the selection, and not one chosen
	 * transaction, that step 3 adds up and that the row's OK commits.
	 */
	let matchSelection = $state<BankTransaction[]>([]);
	/**
	 * Step 1's four filters. They are sent to the server rather than applied in
	 * the browser, so the "Showing X - Y of Z" footer counts what the filter
	 * really matched and not what happened to land on the page.
	 */
	let matchFilters = $state({ showSpent: false, currencyOnly: true, search: '', amount: '' });
	let matchCandidates = $state<BankTransaction[]>([]);
	let matchPagination = $state<Pagination | null>(null);
	let matchPage = $state(1);
	let matchSort = $state<{ key: MatchSortKey; dir: 'asc' | 'desc' }>({ key: 'Date', dir: 'desc' });
	/** The step 3 "Adjustments" menu; at most one row has it open. */
	let adjustmentsFor = $state<string | null>(null);
	/**
	 * The field whose suggestions are open under it — the Create panel's "Who"
	 * or "What", and on which row. Xero opens these two the same way, as a list
	 * *inside the page* under the field being typed in; a native datalist is
	 * drawn by the browser out of the page's reach, which is how a suggestion
	 * ends up somewhere other than under its field.
	 */
	let picker = $state<{ lineId: string; field: 'who' | 'what' } | null>(null);
	/** Which suggestion the keyboard is on, as an index into that list. */
	let pickerIndex = $state(0);
	/** Which rows have "Add details" open, by line id. */
	let showDetails = $state<Record<string, boolean>>({});
	/** The transfer panel's Reference, which the line usually supplies. */
	let transferReference = $state('');
	/** Every contact, for the "Who" autocomplete. */
	let contacts = $state<Contact[]>([]);
	/**
	 * Xero's Filter: the bounds the inbox is narrowed to. `filters` is what is
	 * typed in the panel, `applied` is what the server was last asked for — a
	 * filter that has not been applied is not on, and the badge counts the
	 * second one so the list and the badge can never disagree.
	 */
	interface LineFilters {
		from: string;
		to: string;
		min: string;
		max: string;
	}
	const NO_FILTERS: LineFilters = { from: '', to: '', min: '', max: '' };
	let filterOpen = $state(false);
	let filters = $state<LineFilters>({ ...NO_FILTERS });
	let applied = $state<LineFilters>({ ...NO_FILTERS });
	/**
	 * Xero's "Compact view": the same rows with the evidence the eye does not
	 * need for a quick pass hidden. A working preference, so it is remembered
	 * per browser rather than on the account.
	 */
	let compact = $state(readCompact());
	/** The per-row Options menu, by line id — at most one is open. */
	let optionsFor = $state<string | null>(null);
	/**
	 * Whether that menu had to open upward. The list is a scroll container, so a
	 * menu on one of the last rows would otherwise be cut off by the bottom edge
	 * of the list rather than shown over what is underneath it.
	 */
	let optionsUp = $state(false);
	/**
	 * Xero's two explanations on this screen — the tip above the instruction
	 * row and the one behind the header's "Different balances?". Both are
	 * disclosures rather than dialogs: they open over what is underneath
	 * instead of moving it, and both can be dismissed.
	 */
	let tipsOpen = $state(false);
	let balanceHelpOpen = $state(false);
	/** The Discuss thread for the open panel, oldest note first. */
	let comments = $state<BankStatementLineComment[]>([]);
	let commentDraft = $state('');
	let threadBusy = $state(false);
	/** The numbers behind the auto-reconcile banner, plus the setting itself. */
	let autoReport = $state<AutoReconcileReport | null>(null);
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
	 * Statement lines oldest-first with their balance, which is how the bank
	 * prints them. The API already prices every line it can — the bank's own
	 * figure where the statement carried one, otherwise the account's running
	 * total up to that line — and that number is the one to show. The local
	 * running total survives only as the fallback for a line the API could not
	 * price, because it is the total of the lines this page happens to hold and
	 * not of the account, and the two differ as soon as the account is longer
	 * than one page.
	 *
	 * The tie-break matches the order the API computes the running total in, so
	 * the column reads as a chain rather than jumping around inside one day.
	 */
	const statementRows = $derived.by(() => {
		const sorted = [...lines].sort((a, b) =>
			a.PostedAt < b.PostedAt
				? -1
				: a.PostedAt > b.PostedAt
					? 1
					: a.StatementLineID < b.StatementLineID
						? -1
						: a.StatementLineID > b.StatementLineID
							? 1
							: 0
		);
		let running = 0;
		return sorted.map((line) => {
			running += Number(line.Amount ?? 0);
			const priced = line.Balance != null;
			return { line, balance: priced ? Number(line.Balance) : running, priced };
		});
	});

	/** How many bounds the inbox is actually narrowed by. */
	const appliedCount = $derived(
		[applied.from, applied.to, applied.min, applied.max].filter((v) => v !== '').length
	);

	/**
	 * The dates the inbox already spans — Xero's "Suggested dates". Offering the
	 * range the account has rather than a guess is the point: it is the range
	 * where a date filter can actually do something.
	 */
	const suggestedRange = $derived.by(() => {
		if (lines.length === 0) return null;
		const dates = lines.map((l) => l.PostedAt.slice(0, 10)).sort();
		return { from: dates[0], to: dates[dates.length - 1] };
	});

	const selectedIds = $derived(
		Object.entries(selected)
			.filter(([, on]) => on)
			.map(([id]) => id)
	);

	/** The line the Match panel is working on, if it is open. */
	const matchLine = $derived(panel?.line ?? null);
	/** The currency the step 1 filters and the totals speak in. */
	const matchCurrency = $derived(matchLine?.CurrencyCode || currency);
	/**
	 * Xero's "Region" field under Create → Add details picks which tax regime
	 * a line's rates are drawn from. goXero has no per-region rate table, so
	 * the only region that can honestly be offered is the organisation's own —
	 * a list of others would promise a change of rates that could not happen.
	 * It is rendered because Add details reveals it in Xero, and a form with a
	 * field missing is a worse answer than one with a single honest entry.
	 */
	const taxRegion = $derived(org?.CountryCode ?? '');
	/**
	 * The money that has to be matched: the statement line itself, as the bank
	 * sent it. Its sign is the direction, and the direction is the word in
	 * "Must match money received" — a debit means the money was spent.
	 */
	const matchRequired = $derived(Number(matchLine?.Amount ?? 0));
	const matchDirectionWord = $derived(matchRequired < 0 ? 'spent' : 'received');
	/** What the ticked transactions come to, signed the same way as the line. */
	const matchSelectedTotal = $derived(
		matchSelection.reduce((sum, t) => sum + signedTotal(t), 0)
	);
	const matchDifference = $derived(round2(matchRequired - matchSelectedTotal));
	/**
	 * Reconcile is only available once the two agree. A cent of tolerance
	 * rather than equality, because the figures being compared came through
	 * two decimal round-trips and a float that is 1e-15 away from zero is
	 * balanced, not out.
	 */
	const matchBalanced = $derived(matchSelection.length > 0 && Math.abs(matchDifference) < 0.005);

	/** Step 1's results, sorted by whichever header the user last clicked. */
	const sortedCandidates = $derived.by(() => {
		const rows = [...matchCandidates];
		const sign = matchSort.dir === 'asc' ? 1 : -1;
		rows.sort((a, b) => {
			switch (matchSort.key) {
				case 'Name':
					return sign * counterparty(a).localeCompare(counterparty(b));
				case 'Reference':
					return sign * (a.Reference ?? '').localeCompare(b.Reference ?? '');
				case 'Spent':
				case 'Received':
					return sign * (Number(a.Total ?? 0) - Number(b.Total ?? 0));
				default:
					return sign * (a.Date ?? '').localeCompare(b.Date ?? '');
			}
		});
		return rows;
	});

	/** The page of results the footer is describing, 1-based and inclusive. */
	const matchRange = $derived.by(() => {
		const total = matchPagination?.total ?? matchCandidates.length;
		if (total === 0) return { from: 0, to: 0, total: 0 };
		const from = (matchPage - 1) * MATCH_PAGE_SIZE + 1;
		return { from, to: Math.min(from + matchCandidates.length - 1, total), total };
	});

	const matchAllOnPage = $derived(
		matchCandidates.length > 0 && matchCandidates.every((c) => matchIsSelected(c))
	);


	/** "620 · Entertainment", or just the code when the name is unknown. */
	function codingLabel(code: string, name: string): string {
		return name ? `${code} · ${name}` : code;
	}

	/** The name behind an account code, for the columns that show both. */
	function accountName(code: string): string {
		return accounts.find((a) => a.Code === code)?.Name ?? '';
	}

	/** The statement line each transaction came from, by transaction. */
	const lineByTransaction = $derived.by(() => {
		const byID = new Map<string, BankStatementLine>();
		for (const l of lines) {
			if (l.BankTransactionID) byID.set(l.BankTransactionID, l);
		}
		return byID;
	});

	/**
	 * The rows of Xero's "Statement Details" panel, in Xero's order and under
	 * Xero's labels. Everything here is what the bank sent rather than what we
	 * made of it, which is why it is the panel to open when a coding looks
	 * wrong: it is the only place the raw line can be read back.
	 */
	function detailsRows(l: BankStatementLine) {
		const amount = Number(l.Amount ?? 0);
		return [
			{ label: 'Transaction Date', value: formatDate(l.PostedAt) },
			{ label: 'Payee', value: l.Payee || l.Counterparty || '—' },
			{ label: 'Reference', value: l.Reference || '—' },
			{ label: 'Description', value: l.Description || '—' },
			{
				label: 'Transaction Amount',
				value: formatCurrency(Math.abs(amount), l.CurrencyCode ?? currency)
			},
			{ label: 'Transaction Type', value: amount < 0 ? 'Debit' : 'Credit' },
			{ label: 'Cheque No.', value: l.ChequeNumber || '—' },
			// A statement line carries no tracking category, so Xero's Analysis
			// Code row is empty here for the same reason it is empty on a line
			// nobody has analysed there either.
			{ label: 'Analysis Code', value: '—' }
		];
	}

	/**
	 * Cross to the transaction a statement line became. There is no screen per
	 * bank transaction, so the jump lands on the account's transaction list
	 * with that row picked out — which is where the user would have to look
	 * anyway to see it in context.
	 */
	function showTransaction(bankTransactionId: string) {
		focusTransaction = bankTransactionId;
		setTab('transactions');
	}

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
		// A jump highlight belongs to the jump. Leaving the tab by hand — any
		// way other than through showTransaction — drops it.
		if (t !== 'transactions') focusTransaction = null;
		const u = new URL($page.url.toString());
		u.searchParams.set('tab', t);
		void goto(u.pathname + '?' + u.searchParams.toString(), {
			replaceState: true,
			keepFocus: true,
			noScroll: true
		});
	}

	/**
	 * Which inbox request is the current one. The search box debounces and every
	 * action reloads, so more than one request can be in flight; only the newest
	 * may write `inbox`, or a slow answer to an older question would replace a
	 * newer one.
	 */
	let inboxRequest = 0;

	/**
	 * The inbox query, in one place so the search cannot drift from the first
	 * load — the two must ask for the same set of lines or the count and the
	 * list would disagree.
	 *
	 * `search` is passed rather than read from `lineSearch` so a reload keeps the
	 * term the box is showing: without it, saving a coding refetched the whole
	 * inbox while the box still read "coffee" and the filtered count still
	 * rendered.
	 */
	function inboxParams(search = ''): Record<string, string> {
		// Both the bounds and the suggestion setting are read untracked: they
		// decide what the server is asked for, and each is changed by a handler
		// that reloads once — rather than also re-running the load effect.
		const f = untrack(() => applied);
		return {
			bankAccountId: accountId,
			unreconciled: 'true',
			suggestions: 'true',
			previousEntries: String(untrack(() => suggestPrevious)),
			pageSize: '500',
			...(f.from ? { fromDate: f.from } : {}),
			...(f.to ? { toDate: f.to } : {}),
			...(f.min ? { minAmount: f.min } : {}),
			...(f.max ? { maxAmount: f.max } : {}),
			...(search ? { search } : {})
		};
	}

	/**
	 * Narrow the inbox to the bounds in the panel. The whole inbox is refetched
	 * rather than filtered in the browser, so a filtered count is the real count
	 * and not the count of what happened to be on the page.
	 */
	function applyFilters() {
		applied = { ...filters };
		void reload();
	}

	function resetFilters() {
		filters = { ...NO_FILTERS };
		applied = { ...NO_FILTERS };
		void reload();
	}

	function useSuggestedDates() {
		const r = suggestedRange;
		if (!r) return;
		filters = { ...filters, from: r.from, to: r.to };
	}

	async function reload() {
		if (!accountId) return;
		const seq = ++inboxRequest;
		/*
		 * Only a load with nothing behind it raises the placeholder. Every later
		 * reload is the refresh a commit makes, and it has to leave the screen
		 * standing: the reconcile list is what gives this page its height, so
		 * replacing it with one line of "Loading…" collapses the document, the
		 * browser clamps the scroll back to the top, and the row the user just
		 * clicked OK on jumps off the screen. Xero stays where the hand left it.
		 */
		loading = loadedAccountId !== accountId;
		err = '';
		try {
			const [a, o, bal, box, all, tx, imp, per, accs, rates, auto, people] = await Promise.all([
				accountApi.get(accountId),
				orgApi.current().catch(() => null),
				statementApi.balance(accountId).catch(() => null),
				// The term is read untracked: reload runs from an $effect, and
				// tracking it would re-run the whole load on every keystroke that
				// the debounce exists to avoid.
				statementApi
					.list(inboxParams(untrack(() => lineSearch)))
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
				taxRateApi.list().catch(() => []),
				statementApi
					.autoReconcileStatus(accountId)
					.then((r) => r.AutoReconcile ?? null)
					.catch(() => null),
				// The Create panel's "Who" field autocompletes against these,
				// so they are loaded with the screen rather than on first
				// keystroke — a list that arrives while you are typing is worse
				// than no list at all.
				contactApi.list({ pageSize: '500' }).catch(() => ({ Contacts: [] }))
			]);
			// A newer load or search has taken over; its answer is the current
			// one and this one must not write over it.
			if (seq !== inboxRequest) return;
			// What is on screen is now this account's, so the next reload of the
			// same account refreshes it in place instead of blanking the page.
			loadedAccountId = accountId;
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
			autoReport = auto ?? null;
			contacts = people?.Contacts ?? [];
			// A menu belongs to the row it was opened on; after a reload the rows
			// are new objects, so it closes rather than pointing at nothing.
			optionsFor = null;
			seedCoding(inbox);
		} catch (e) {
			err = e instanceof Error ? e.message : 'Failed to load bank account';
		} finally {
			// Cleared unconditionally: only a reload raises the flag, so a search
			// that superseded this one must not leave the screen spinning.
			loading = false;
		}
	}

	/**
	 * Search the inbox. The search runs on the server rather than over the page
	 * already in hand: the inbox is paged, so filtering the 500 lines we happen
	 * to hold would quietly miss the rest — the one failure mode a search box
	 * must not have.
	 */
	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	function searchInbox(term: string) {
		lineSearch = term;
		clearTimeout(searchTimer);
		searchTimer = setTimeout(async () => {
			// Taken when the request goes out, not when the box was typed in: the
			// newest request to start is the one whose term the user last asked
			// for, and an earlier answer arriving later must be dropped.
			const seq = ++inboxRequest;
			try {
				const box = await statementApi.list(inboxParams(term));
				if (seq !== inboxRequest) return;
				inbox = box?.StatementLines ?? [];
				seedCoding(inbox);
			} catch (e) {
				if (seq !== inboxRequest) return;
				err = e instanceof Error ? e.message : 'Search failed';
			}
		}, 250);
	}

	// The debounce outlives the page if it is never cleared: leaving within the
	// 250 ms would fire a request for a screen that no longer exists and then
	// write its answer into state nobody is rendering.
	onDestroy(() => clearTimeout(searchTimer));

	/**
	 * The coding a line's own evidence argues for: the bank rule first, because
	 * a rule is what the user wrote for exactly this case, then the coding this
	 * payee was given last time, then whatever the line arrived with.
	 *
	 * One function and not two, because the Create panel has to be able to ask
	 * *what was suggested* as well as *what to fill in*: what is on show is a
	 * suggestion only for as long as the fields still hold these values.
	 */
	function seedFor(l: BankStatementLine): Coding {
		const rule = l.Suggestions?.[0];
		const prev = l.PreviousEntry;
		const ruleAcc = rule?.AccountID
			? accounts.find((a) => a.AccountID === rule.AccountID)
			: undefined;
		const ruleWho = rule?.ContactID
			? contacts.find((c) => c.ContactID === rule.ContactID)
			: undefined;
		return {
			AccountCode: ruleAcc?.Code ?? prev?.AccountCode ?? l.AccountCode ?? '',
			TaxType: rule?.TaxType ?? prev?.TaxType ?? l.TaxType ?? '',
			Description: l.Description ?? prev?.Description ?? '',
			Reference: l.Reference || prev?.Reference || '',
			/*
			 * Who comes with the rest of it. Xero's own suggestion carries a
			 * contact beside the account — its prediction mapper returns
			 * ContactName and ContactID next to AccountID — and its Create
			 * panel's OK asks for exactly two things, an account and a name.
			 * So a line the books already have an answer for opens with Who
			 * filled in and its OK showing: that is the point of the
			 * suggestion, and on a line nobody has touched it is one click.
			 */
			ContactID: ruleWho?.ContactID ?? prev?.ContactID,
			ContactName: ruleWho?.Name ?? prev?.ContactName ?? ''
		};
	}

	/**
	 * Give every inbox line a coding row, prefilling from whatever suggestion
	 * the line came with.
	 *
	 * Anything the user has typed over is left alone, which is what makes it
	 * safe to re-seed when the suggestion setting changes underneath them.
	 */
	function seedCoding(rows: BankStatementLine[]) {
		const next = { ...coding };
		for (const l of rows) {
			if (touched[l.StatementLineID]) continue;
			next[l.StatementLineID] = seedFor(l);
		}
		coding = next;
	}

	function setCoding(id: string, field: keyof Coding, value: string) {
		touched = { ...touched, [id]: true };
		coding = { ...coding, [id]: { ...coding[id], [field]: value } };
	}

	/**
	 * How the previous-entry hint reads. Xero shows the suggestion without
	 * saying where it came from; naming the entry it came from is what lets
	 * someone decide whether to trust it.
	 */
	function suggestionBasis(s: PreviousEntrySuggestion): string {
		const who = s.ContactName || s.Payee || s.Reference || 'this payee';
		return s.MatchCount === 1
			? `Suggested from the last entry for ${who}`
			: `Suggested from ${s.MatchCount} previous entries for ${who}`;
	}

	/** The account a bank rule would code this line to, as it is shown in the UI. */
	function ruleCoding(rule: BankRuleSuggestion): string {
		const a = accounts.find((x) => x.AccountID === rule.AccountID);
		return a ? codingLabel(a.Code, a.Name) : '';
	}

	/**
	 * The heading over the Create panel. A rule and a previous entry both open
	 * the panel pre-filled, and the heading has to say which one did it — the
	 * two deserve different amounts of trust.
	 */
	function createHeading(l: BankStatementLine): string {
		const rule = l.Suggestions?.[0];
		if (rule) return `Rule “${rule.RuleName}” filled this in`;
		return 'Create a transaction from this line';
	}

	/**
	 * Whether the books have an answer for this line at all — a bank rule, or
	 * the coding this payee was given last time. It is what the Create panel
	 * has to say for itself, and what "Clear it" clears.
	 */
	function hasPrefill(l: BankStatementLine): boolean {
		return !!l.Suggestions?.length || !!l.PreviousEntry;
	}

	/** Where the prefill on this line came from, for the Create panel. */
	function prefillBasis(l: BankStatementLine): PreviousEntrySuggestion | null {
		if (l.Suggestions?.length) return null;
		return l.PreviousEntry ?? null;
	}

	/**
	 * Whether the account and tax code on show are still the books' answer
	 * rather than the user's — a bank rule, or the coding this payee was given
	 * last time. Xero paints coding it worked out for itself, so that the one
	 * thing on the screen nobody typed is also the one thing that is marked,
	 * and the mark goes the moment the user answers differently.
	 *
	 * It is read off the values and not off `touched`: typing a description is
	 * not an answer about the account, and the account is what is being marked.
	 */
	function isSuggested(l: BankStatementLine): boolean {
		if (!hasPrefill(l)) return false;
		const c = coding[l.StatementLineID];
		if (!c) return false;
		const s = seedFor(l);
		return c.AccountCode === s.AccountCode && (c.TaxType ?? '') === (s.TaxType ?? '');
	}

	/** The account behind a code, or undefined while the text is not one yet. */
	function accountFor(code: string | undefined): Account | undefined {
		return code ? accounts.find((a) => a.Code === code) : undefined;
	}

	/**
	 * Xero's rule for the Create panel's OK, taken from its own initOKButton:
	 * the button is on show once there is an answer to commit — an account has
	 * been chosen *and* the Who field says who the money went to — and it goes
	 * again the moment either is cleared. It is therefore a property of what has
	 * been typed and not of which row was clicked, which is why Xero shows the
	 * OK on rows nobody has opened.
	 */
	function canCreate(l: BankStatementLine): boolean {
		const c = coding[l.StatementLineID];
		return !!accountFor(c?.AccountCode) && !!c?.ContactName?.trim();
	}

	/**
	 * Which tab's panel a row is showing. Xero's right-hand column is never
	 * empty: every row has a tab open and Create is the one it opens by
	 * default, so a row nobody has touched shows Create rather than nothing.
	 */
	function shownTab(l: BankStatementLine): PanelKind {
		return panel?.line.StatementLineID === l.StatementLineID ? panel.kind : 'create';
	}

	/**
	 * The OK a row has to show, if it has one. The rule is Xero's own, read off
	 * its initOKButton(), which re-runs on every keystroke and every blur for
	 * every row on the screen rather than once for the row that was clicked:
	 *
	 *   Create    an account has been chosen and Who says who the money went to
	 *   Match     the selected transactions add up to the statement line
	 *   Transfer  an account to move the money to or from
	 *   Discuss   none — a note is saved with Ctrl + S, as its own hint says
	 *
	 * Because it follows what has been typed rather than which panel is open,
	 * Xero shows an OK on rows nobody has opened and takes it away again
	 * mid-typing. goXero hung the OK off whichever panel was open, which is how
	 * it ended up in the wrong form: the row that had an answer had no button,
	 * and the row that was open had one.
	 */
	function rowOk(l: BankStatementLine): { title: string; run: () => void } | null {
		switch (shownTab(l)) {
			case 'create':
				return canCreate(l)
					? {
							title: 'Create the transaction and reconcile this line',
							run: () => createFromPanel(l)
						}
					: null;
			case 'match':
				return matchBalanced
					? {
							title: 'Reconcile this line against the selected transactions',
							run: () => reconcileSelection(l)
						}
					: null;
			case 'transfer':
				return transferTo
					? {
							title: 'Create the transfer and reconcile this line',
							run: () => transferFrom(l)
						}
					: null;
			default:
				return null;
		}
	}

	function clearSuggestion(l: BankStatementLine) {
		touched = { ...touched, [l.StatementLineID]: true };
		coding = {
			...coding,
			[l.StatementLineID]: { ...coding[l.StatementLineID], AccountCode: '', TaxType: '' }
		};
	}

	const SUGGEST_PREVIOUS_KEY = 'goxero.bankReconcile.suggestPreviousEntries.v1';

	function readSuggestPrevious(): boolean {
		if (!browser) return true;
		try {
			// Only an explicit "off" turns it off; a fresh browser gets Xero's
			// default, which is on.
			return localStorage.getItem(SUGGEST_PREVIOUS_KEY) !== 'false';
		} catch {
			return true;
		}
	}

	/**
	 * Flip the setting and re-fill the inbox. Everything the user has typed
	 * survives; everything they have not is re-derived, so the setting visibly
	 * does what it says instead of only affecting the next page load.
	 */
	function toggleSuggestPrevious() {
		suggestPrevious = !suggestPrevious;
		if (browser) {
			try {
				localStorage.setItem(SUGGEST_PREVIOUS_KEY, String(suggestPrevious));
			} catch {
				/* the setting just will not be remembered */
			}
		}
		const keep: Record<string, Coding> = {};
		for (const [id, v] of Object.entries(coding)) {
			if (touched[id]) keep[id] = v;
		}
		coding = keep;
		touched = {};
		void reload();
	}

	/**
	 * Compact view: the same rows with the second line of evidence — reference,
	 * description and the panel button — folded away. It is the difference
	 * between reading the inbox and working it, which is why Xero makes it a
	 * switch rather than a separate screen.
	 */
	const COMPACT_KEY = 'goxero.bankReconcile.compactView.v1';

	function readCompact(): boolean {
		if (!browser) return false;
		try {
			return localStorage.getItem(COMPACT_KEY) === 'true';
		} catch {
			return false;
		}
	}

	function toggleCompact() {
		compact = !compact;
		if (browser) {
			try {
				localStorage.setItem(COMPACT_KEY, String(compact));
			} catch {
				/* the setting just will not be remembered */
			}
		}
	}

	/**
	 * The account this line is being coded to, as an id. A new bank rule codes
	 * its matches to an account, so this is what the Options menu hands over
	 * when it sends the user off to write one.
	 */
	function codingAccountID(l: BankStatementLine): string {
		const rule = l.Suggestions?.[0];
		if (rule?.AccountID) return rule.AccountID;
		const code = coding[l.StatementLineID]?.AccountCode || l.AccountCode || '';
		return accounts.find((a) => a.Code === code)?.AccountID ?? '';
	}

	/**
	 * "Create bank rule" from a row: Xero opens the rule form with the line's
	 * payee already in the condition, because writing a rule from a line you are
	 * looking at is the only moment the rule is obvious. Everything the form can
	 * prefill travels in the query string.
	 */
	function rowRuleHref(l: BankStatementLine): string {
		const amount = Number(l.Amount ?? 0);
		const params = new URLSearchParams();
		params.set('type', amount < 0 ? 'SPEND' : 'RECEIVE');
		const payee = l.Payee || l.Counterparty || '';
		if (payee) params.set('payee', payee);
		if (l.Reference) params.set('reference', l.Reference);
		if (l.Description) params.set('description', l.Description);
		const accountId_ = codingAccountID(l);
		if (accountId_) params.set('accountId', accountId_);
		if (l.BankAccountID) params.set('bankAccountId', l.BankAccountID);
		return `/app/accounting/bank-rules/new?${params.toString()}`;
	}

	/**
	 * Drop a line from the inbox. The server refuses once the line has been
	 * reconciled, and the message it sends back is the one shown — a line that
	 * became a transaction cannot be deleted, only unreconciled first.
	 */
	async function deleteLine(l: BankStatementLine) {
		optionsFor = null;
		const what = l.Payee || l.Description || formatDate(l.PostedAt);
		if (!confirm(`Delete the statement line for ${what}?`)) return;
		err = '';
		notice = '';
		try {
			await statementApi.remove(l.StatementLineID);
			notice = 'Statement line deleted.';
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not delete that line';
		}
	}

	/** Turn automatic reconciliation on or off for this account, from the banner. */
	async function toggleAutoReconcile(on: boolean) {
		err = '';
		notice = '';
		panelBusy = true;
		try {
			const res = await statementApi.setAutoReconcile(accountId, on);
			autoReport = res.AutoReconcile ?? null;
			notice = on
				? 'Auto-reconcile is on: imports will reconcile what they can on their own.'
				: 'Auto-reconcile is off for this account.';
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not change that setting';
		} finally {
			panelBusy = false;
		}
	}

	/** The line's discussion, oldest note first. */
	async function loadComments(lineId: string) {
		threadBusy = true;
		try {
			const res = await statementApi.comments(lineId);
			comments = res.Comments ?? [];
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not load the discussion';
		} finally {
			threadBusy = false;
		}
	}

	/**
	 * Post a note. The thread is re-read rather than appended to, so what the
	 * user sees is what was stored — including who it says wrote it.
	 */
	async function postComment() {
		const p = panel;
		if (!p || p.kind !== 'discuss') return;
		const text = commentDraft.trim();
		if (!text) {
			err = 'Write something before posting.';
			return;
		}
		err = '';
		notice = '';
		threadBusy = true;
		try {
			await statementApi.addComment(p.line.StatementLineID, text);
			commentDraft = '';
			await loadComments(p.line.StatementLineID);
			notice = 'Note added to the discussion.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not save that note';
		} finally {
			threadBusy = false;
		}
	}

	/**
	 * Ctrl + S (or Cmd + S) saves the note being written, which is what Xero's
	 * "Ctrl + S at any time to save" promises. It only applies while the Discuss
	 * panel is open — anywhere else the keystroke is left to the browser.
	 */
	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			if (adjustmentsFor !== null) {
				adjustmentsFor = null;
				return;
			}
			if (picker !== null) {
				closePicker();
				return;
			}
			if (optionsFor !== null) {
				optionsFor = null;
				return;
			}
			if (panel) {
				panel = null;
				return;
			}
			if (filterOpen) filterOpen = false;
			if (tipsOpen) tipsOpen = false;
			if (balanceHelpOpen) balanceHelpOpen = false;
		}
		if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 's') return;
		if (panel?.kind !== 'discuss') return;
		e.preventDefault();
		void postComment();
	}

	/**
	 * Open (or close) one row's Options menu, choosing the side to open on from
	 * the room actually left in the list below the button.
	 */
	function openOptions(lineId: string, ev: MouseEvent) {
		if (optionsFor === lineId) {
			optionsFor = null;
			return;
		}
		const btn = ev.currentTarget as HTMLElement | null;
		const box = btn?.getBoundingClientRect();
		const list = btn?.closest('[data-reconcile-list]')?.getBoundingClientRect();
		const floor = list ? list.bottom : window.innerHeight;
		optionsUp = box ? floor - box.bottom < 96 : false;
		optionsFor = lineId;
	}

	/**
	 * An open Options menu closes on the next click that is not inside another
	 * Options menu, so a menu can never be left behind on a row the user has
	 * moved on from.
	 */
	function onDocumentClick(e: MouseEvent) {
		const target = e.target as HTMLElement | null;
		// A disclosure closes on the next click outside it, which is the rule
		// the Options menu already follows.
		if (tipsOpen && !target?.closest('[data-tips]')) tipsOpen = false;
		if (balanceHelpOpen && !target?.closest('[data-balance-help]')) balanceHelpOpen = false;
		if (adjustmentsFor !== null && !target?.closest('[data-adjustments]')) adjustmentsFor = null;
		if (picker !== null && !target?.closest('[data-xero-picker]')) closePicker();
		if (optionsFor === null) return;
		if (target?.closest('[data-line-options]')) return;
		optionsFor = null;
	}

	// ── The Match panel's arithmetic ────────────────────────────────────────
	//
	// Everything in step 3 comes out of these four lines. They live together
	// because the rule they encode is one rule: a bank transaction's Total is
	// always a positive magnitude and its Type says which way the money moved,
	// while a statement line carries direction in the sign of its amount. Both
	// are reduced to a signed contribution to the bank account before anything
	// is added up, so "does this selection equal the line?" is one comparison
	// rather than one per direction.

	/** Whether the transaction brought money into the account. */
	function isReceive(t: BankTransaction): boolean {
		return (t.Type ?? '').toUpperCase().startsWith('RECEIVE');
	}

	/** What the transaction did to the bank account: in is positive, out is negative. */
	function signedTotal(t: BankTransaction): number {
		const total = Math.abs(Number(t.Total ?? 0));
		return isReceive(t) ? total : -total;
	}

	/** Money, to the cent — the unit every figure on this row is compared in. */
	function round2(n: number): number {
		return Math.round(n * 100) / 100;
	}

	/** Who the transaction was with, as the results table names it. */
	function counterparty(t: BankTransaction): string {
		return t.Contact?.Name || t.Type || '—';
	}

	function matchIsSelected(t: BankTransaction): boolean {
		return matchSelection.some((s) => s.BankTransactionID === t.BankTransactionID);
	}

	function toggleMatchCandidate(t: BankTransaction) {
		matchSelection = matchIsSelected(t)
			? matchSelection.filter((s) => s.BankTransactionID !== t.BankTransactionID)
			: [...matchSelection, t];
	}

	/** "Select all on this page" — only ever the page, never the whole result set. */
	function toggleAllCandidates() {
		const onPage = new Set(matchCandidates.map((c) => c.BankTransactionID));
		if (matchAllOnPage) {
			matchSelection = matchSelection.filter((s) => !onPage.has(s.BankTransactionID));
			return;
		}
		const added = matchCandidates.filter((c) => !matchIsSelected(c));
		matchSelection = [...matchSelection, ...added];
	}

	/**
	 * Sort by a column header. Clicking the column already sorted reverses it,
	 * and a new column starts descending because the largest and the most
	 * recent are the ones worth seeing first.
	 */
	function sortMatchBy(key: MatchSortKey) {
		matchSort =
			matchSort.key === key
				? { key, dir: matchSort.dir === 'asc' ? 'desc' : 'asc' }
				: { key, dir: 'desc' };
	}

	/** "Go" re-runs the search; an empty search is a reset, not an error. */
	function runMatchSearch(lineId: string) {
		void loadMatchCandidates(lineId, 1);
	}

	function clearMatchSearch(lineId: string) {
		matchFilters = { ...matchFilters, search: '', amount: '' };
		void loadMatchCandidates(lineId, 1);
	}

	/** Load (or re-load) step 1's results under the current filters. */
	async function loadMatchCandidates(lineId: string, page = 1) {
		matchPage = page;
		panelBusy = true;
		err = '';
		try {
			const res = await statementApi.matches(lineId, {
				search: matchFilters.search.trim(),
				showSpent: matchFilters.showSpent,
				// "Show USD items only" is on by default, and the currency it
				// means is the line's own — the label says the code so there is
				// nothing implicit about which list is being filtered.
				currency: matchFilters.currencyOnly ? matchCurrency : '',
				amount: matchFilters.amount.trim(),
				page,
				pageSize: MATCH_PAGE_SIZE
			});
			matchCandidates = res.BankTransactions ?? [];
			matchPagination = res.Pagination ?? null;
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not look up matching transactions';
		} finally {
			panelBusy = false;
		}
	}

	/**
	 * Step 3's "Reconcile": the same commit the row's OK makes. It is a link
	 * rather than a button because it is the second way to say the same thing,
	 * and it stays disabled until the sums agree — reconciling a line against a
	 * selection that does not add up is the one thing this form exists to stop.
	 */
	function reconcileSelection(l: BankStatementLine) {
		if (!matchBalanced) {
			err = 'The selected transactions do not add up to the statement line yet.';
			return;
		}
		void act(
			() =>
				statementApi.reconcileSelection(
					l.StatementLineID,
					matchSelection.map((t) => t.BankTransactionID)
				),
			'Reconciled against the selected transactions.'
		);
	}

	/** Step 3's "Cancel": drop the selection and close the row. */
	function cancelMatch() {
		matchSelection = [];
		adjustmentsFor = null;
		panel = null;
	}

	/**
	 * Step 2's "New Transaction": the dropdown equivalent of the Create tab. It
	 * posts what Create would post but leaves it unreconciled and adds it to
	 * the selection instead, which is what makes it a *candidate* rather than
	 * the answer — the line is still waiting for whatever the user picks.
	 *
	 * It is coded from the Create panel's fields, because the two controls are
	 * asking the same question and neither should make the user answer twice.
	 */
	async function addNewTransaction(l: BankStatementLine) {
		const c = coding[l.StatementLineID];
		if (!c?.AccountCode) {
			err = 'Choose an account on the Create tab first — a new transaction needs a coding.';
			return;
		}
		panelBusy = true;
		err = '';
		notice = '';
		try {
			const res = await statementApi.create(l.StatementLineID, {
				AccountCode: c.AccountCode,
				TaxType: c.TaxType || undefined,
				Description: c.Description || undefined,
				Reference: c.Reference || undefined,
				Reconcile: false
			});
			const created = res.BankTransactions?.[0];
			if (created) matchSelection = [...matchSelection, created];
			notice = 'New transaction added to the selection.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not add that transaction';
		} finally {
			panelBusy = false;
		}
	}

	/**
	 * Step 3's "Adjustments". Both menu items post the shortfall between the
	 * line and what has been selected so far, because that is the only amount
	 * that ever makes sense here: an adjustment exists to close the gap. The
	 * new transaction joins the selection like any other.
	 */
	async function addAdjustment(kind: 'BANK_FEE' | 'MINOR_ADJUSTMENT') {
		const p = panel;
		if (!p) return;
		adjustmentsFor = null;
		const shortfall = round2(matchRequired - matchSelectedTotal);
		if (shortfall === 0) {
			err = 'There is nothing to adjust: the selection already matches the line.';
			return;
		}
		panelBusy = true;
		err = '';
		notice = '';
		try {
			const res = await statementApi.adjustment(p.line.StatementLineID, {
				Kind: kind,
				Amount: shortfall.toFixed(2),
				AccountCode: coding[p.line.StatementLineID]?.AccountCode || undefined
			});
			const created = res.BankTransactions?.[0];
			if (created) matchSelection = [...matchSelection, created];
			notice =
				kind === 'BANK_FEE'
					? 'Bank fee added to the selection.'
					: 'Minor adjustment added to the selection.';
		} catch (e) {
			err = e instanceof Error ? e.message : 'Could not add that adjustment';
		} finally {
			panelBusy = false;
		}
	}

	/**
	 * Xero prints a balance held in the organisation's own currency as a bare
	 * number — "7,430.22", not "US$7,430.22" — and names the currency only once
	 * the account is held in something else. It is the same rule the
	 * bank-accounts list follows, so the two screens cannot disagree about how
	 * one number is written.
	 */
	function formatAccountAmount(value: number | string | undefined): string {
		const held = account?.CurrencyCode ?? '';
		if (held && held !== (org?.BaseCurrency ?? '')) return formatCurrency(value, held);
		return formatAmount(value);
	}

	/** How the "What" field prints a chosen account: "620 · Entertainment". */
	function accountLabel(code: string): string {
		if (!code) return '';
		return codingLabel(code, accountName(code));
	}

	/**
	 * The "What" field's suggestions, in code order. Typing narrows them on the
	 * code *or* the name, which is what lets "bank fees" and "404" find the same
	 * account.
	 */
	function accountMatches(lineId: string): Account[] {
		const raw = (coding[lineId]?.AccountCode ?? '').trim();
		// Once an account has been chosen the field shows it as "code · name",
		// which is the answer and not something to filter by — the list opens on
		// all of them so the user can change their mind.
		const q = accountFor(raw) ? '' : raw.toLowerCase();
		if (!q) return codingAccounts;
		return codingAccounts.filter((a) =>
			`${a.Code} · ${a.Name} ${a.Code} ${a.Name}`.toLowerCase().includes(q)
		);
	}

	/** Whether a name on show is one of the contacts, and so an answer. */
	function contactNamed(name: string | undefined): boolean {
		const n = (name ?? '').trim().toLowerCase();
		return !!n && contacts.some((c) => c.Name.trim().toLowerCase() === n);
	}

	/**
	 * "Who" autocomplete options. Contacts are matched on any part of the name,
	 * which is how someone half-remembering a supplier finds them.
	 */
	function contactMatches(lineId: string): Contact[] {
		const raw = (coding[lineId]?.ContactName ?? '').trim();
		// A named contact is the answer and not something to filter by, which is
		// the same rule the What field follows: a prefilled Who must not open a
		// list holding only the name already in it.
		const q = contactNamed(raw) ? '' : raw.toLowerCase();
		if (!q) return contacts.slice(0, 8);
		return contacts.filter((c) => c.Name.toLowerCase().includes(q)).slice(0, 8);
	}

	/** The open list's options, whichever field it belongs to. */
	function pickerOptions(): (Contact | Account)[] {
		if (!picker) return [];
		return picker.field === 'who' ? contactMatches(picker.lineId) : accountMatches(picker.lineId);
	}

	function openPicker(lineId: string, field: 'who' | 'what') {
		picker = { lineId, field };
		pickerIndex = 0;
	}

	function closePicker() {
		picker = null;
		pickerIndex = 0;
	}

	/** Whether the open list belongs to this field on this row. */
	function pickerOpen(lineId: string, field: 'who' | 'what'): boolean {
		return picker?.lineId === lineId && picker.field === field;
	}

	/**
	 * Typing into What when an account is already chosen. The field shows that
	 * account as "code · name", so the keystrokes are a new question and not an
	 * edit of that answer: what is typed after the name is the search, and
	 * deleting the name is how the field is emptied.
	 */
	function onWhatInput(lineId: string, value: string) {
		const prev = coding[lineId]?.AccountCode ?? '';
		const label = accountLabel(prev);
		let typed = value;
		if (accountFor(prev)) {
			if (label && value.startsWith(label)) typed = value.slice(label.length);
			else if (label.startsWith(value)) typed = '';
		}
		setCoding(lineId, 'AccountCode', typed);
		openPicker(lineId, 'what');
	}

	/**
	 * The same rule for Who, which now needs it: a prefilled row opens with the
	 * contact already named, and a keystroke after that name is a new search and
	 * not an edit of the answer. Deleting the name is how the field is emptied.
	 * Either way the id the suggestion carried goes with the name.
	 */
	function onWhoInput(lineId: string, value: string) {
		const prev = coding[lineId]?.ContactName ?? '';
		const named = contactNamed(prev) ? prev : '';
		let typed = value;
		if (named) {
			if (value.startsWith(named)) typed = value.slice(named.length);
			else if (named.startsWith(value)) typed = '';
		}
		setCoding(lineId, 'ContactName', typed);
		setCoding(lineId, 'ContactID', '');
		openPicker(lineId, 'who');
	}

	/**
	 * Take one suggestion. What differs between the two fields is only how the
	 * answer is written down: a name for Who, a code for What.
	 */
	function pick(lineId: string, field: 'who' | 'what', value: string) {
		setCoding(lineId, field === 'who' ? 'ContactName' : 'AccountCode', value);
		if (field === 'who') setCoding(lineId, 'ContactID', '');
		closePicker();
	}

	/** Option under the keyboard, so Enter takes what the arrows walked to. */
	function pickerKey(e: KeyboardEvent, lineId: string, field: 'who' | 'what') {
		const options = pickerOptions();
		if (e.key === 'Escape') {
			closePicker();
			return;
		}
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			if (!picker || picker.lineId !== lineId || picker.field !== field) {
				openPicker(lineId, field);
				return;
			}
			e.preventDefault();
			const step = e.key === 'ArrowDown' ? 1 : -1;
			pickerIndex = (pickerIndex + step + options.length) % Math.max(options.length, 1);
			return;
		}
		if (e.key !== 'Enter' || !picker || picker.lineId !== lineId || picker.field !== field) return;
		const chosen = options[pickerIndex];
		if (!chosen) return;
		e.preventDefault();
		if (field === 'who') pick(lineId, 'who', (chosen as Contact).Name);
		else pick(lineId, 'what', (chosen as Account).Code);
	}

	/**
	 * Resolve the "Who" field to a contact id, making the contact if the name
	 * is new. Xero does this silently on save — a name typed into the field is
	 * a contact, whether or not it existed a second ago.
	 */
	async function resolveContactID(lineId: string): Promise<string | undefined> {
		const c = coding[lineId];
		const name = c?.ContactName?.trim() ?? '';
		if (!name) return c?.ContactID || undefined;
		if (c?.ContactID) return c.ContactID;
		const existing = contacts.find((x) => x.Name.toLowerCase() === name.toLowerCase());
		if (existing) {
			setCoding(lineId, 'ContactID', existing.ContactID);
			return existing.ContactID;
		}
		const res = await contactApi.create({ Name: name });
		const created = res.Contacts?.[0];
		if (created) {
			contacts = [...contacts, created];
			setCoding(lineId, 'ContactID', created.ContactID);
			return created.ContactID;
		}
		return undefined;
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

	/**
	 * Open one of the four panels on a line. The tab is the row's answer to
	 * "what is this?" and stays in the right-hand column, where Xero keeps it:
	 * measured, clicking Create or Transfer there leaves the row its own height
	 * and only Match — and Find & Match, which is the same question — draws the
	 * three-step form under it at the row's full width. Opening that form for
	 * every tab is what used to make a row grow by half a screen under the
	 * cursor the moment a tab was clicked.
	 */
	function openPanel(kind: PanelKind, line: BankStatementLine) {
		const sameLine = panel?.line.StatementLineID === line.StatementLineID;
		if (!sameLine) {
			// A different line means a different question: the selection, the
			// results and the filters all belong to the line that was open.
			matchSelection = [];
			matchCandidates = [];
			matchPagination = null;
			matchPage = 1;
			matchFilters = { showSpent: false, currencyOnly: true, search: '', amount: '' };
			matchSort = { key: 'Date', dir: 'desc' };
			transferReference = line.Reference ?? '';
		}
		panel = { kind, line };
		panelBusy = false;
		adjustmentsFor = null;
		closePicker();
		if (!sameLine) {
			// Xero preselects the only sensible answer: the other bank account.
			transferTo = otherBankAccounts[0]?.AccountID ?? '';
		}
		commentDraft = '';
		comments = [];
		if (kind === 'discuss') void loadComments(line.StatementLineID);
		// Only Match has results on show, and what is already loaded for this
		// line is still the answer — switching tabs must not re-ask the question
		// or lose the selection.
		if (kind === 'match' && (!sameLine || matchCandidates.length === 0)) {
			void loadMatchCandidates(line.StatementLineID, sameLine ? matchPage : 1);
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

	/**
	 * Commit the Create panel. It takes the line rather than reading the open
	 * panel, because the OK belongs to the row that has the answer and that row
	 * need not be the one whose form is on show — the row's own button calls
	 * this with the row it is drawn in. The contact is resolved first because
	 * "Who" is free text: a name nobody has used before becomes a contact here,
	 * which is the moment Xero makes one too.
	 */
	async function createFromPanel(l: BankStatementLine) {
		const c = coding[l.StatementLineID];
		if (!c?.AccountCode) {
			err = 'Choose an account to code this line to.';
			return;
		}
		panelBusy = true;
		err = '';
		notice = '';
		try {
			const contactID = await resolveContactID(l.StatementLineID);
			await statementApi.create(l.StatementLineID, {
				AccountCode: c.AccountCode,
				TaxType: c.TaxType || undefined,
				Description: c.Description || undefined,
				Reference: c.Reference || undefined,
				ContactID: contactID
			});
			notice = 'Transaction created and reconciled.';
			panel = null;
			await reload();
		} catch (e) {
			err = e instanceof Error ? e.message : 'That did not work';
		} finally {
			panelBusy = false;
		}
	}

	/**
	 * The Transfer panel's OK. It takes the line rather than reading the open
	 * panel, because the row whose tab strip is on show is the row the transfer
	 * belongs to, and a row can be transferred from without being the one the
	 * form is drawn under.
	 */
	function transferFrom(l: BankStatementLine) {
		if (!transferTo) {
			err = 'Choose the account the money moved to or from.';
			return;
		}
		void act(
			() =>
				statementApi.transfer(l.StatementLineID, {
					ToBankAccountID: transferTo,
					Reference: transferReference.trim() || undefined
				}),
			'Transfer created and reconciled.'
		);
	}

	/**
	 * What the bank says about a line we have already coded, in the words of the
	 * person who has to deal with it. Empty for a line the bank agrees with,
	 * which is every line until it restates or withdraws one.
	 */
	function upstreamLabel(l: BankStatementLine): string {
		if (l.UpstreamChange === 'REMOVED') return 'Withdrawn by the bank';
		if (l.UpstreamChange === 'MODIFIED') return 'Changed by the bank';
		return '';
	}

	/**
	 * The bank's own version of the line, for the tooltip and the details panel.
	 * It is what the feed parked beside our coding rather than applied over it,
	 * so this is the one place the two can be read side by side.
	 */
	function upstreamDetail(l: BankStatementLine): string {
		if (l.UpstreamChange === 'REMOVED') {
			return 'The bank has removed this transaction from the feed. Your entry is still in the books.';
		}
		if (l.UpstreamChange !== 'MODIFIED') return '';
		const amount = Number(l.UpstreamAmount ?? 0);
		const when = l.UpstreamPostedAt ? formatDate(l.UpstreamPostedAt) : '';
		return `The bank now has ${formatCurrency(Math.abs(amount), l.CurrencyCode ?? currency)}${
			amount < 0 ? ' out' : ' in'
		}${when ? ` on ${when}` : ''}, against the ${formatCurrency(
			Math.abs(Number(l.Amount ?? 0)),
			l.CurrencyCode ?? currency
		)} you coded.`;
	}

	/**
	 * Stop showing a notice the bank withdrew a line we had already coded. Nothing
	 * is posted and nothing is unbooked: the transaction is the books, and the
	 * bank's withdrawal is not a reason to move money in the ledger by itself.
	 *
	 * Only a withdrawal can be dismissed. A line the bank *changed* carries a
	 * notice derived from the bank's current version, so there is nothing to clear
	 * — the next sync would put it straight back — and the answer to it is to
	 * recode the line or leave the notice standing.
	 */
	function dismissUpstream(l: BankStatementLine) {
		void act(
			() => statementApi.dismissUpstreamChange(l.StatementLineID),
			"The bank's withdrawal was dismissed."
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
			const filledIDs: string[] = [];
			for (const l of res.StatementLines ?? []) {
				const s = l.Suggestions?.[0];
				if (!s) continue;
				const acc = s.AccountID ? accounts.find((a) => a.AccountID === s.AccountID) : undefined;
				next[l.StatementLineID] = {
					...(next[l.StatementLineID] ?? { Description: '', Reference: '' }),
					AccountCode: acc?.Code ?? l.AccountCode ?? next[l.StatementLineID]?.AccountCode ?? '',
					TaxType: s.TaxType ?? l.TaxType ?? next[l.StatementLineID]?.TaxType ?? ''
				};
				filledIDs.push(l.StatementLineID);
			}
			coding = next;
			// A rule the user asked for outranks a suggestion, so these rows are
			// marked as theirs and a later re-seed leaves them alone.
			touched = { ...touched, ...Object.fromEntries(filledIDs.map((id) => [id, true])) };
			const filled = filledIDs.length;
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
				{formatAccountAmount(statementBalance)}
			</div>
		</div>
		<div class="relative" data-balance-help>
			<div class="text-xs uppercase muted tracking-wide">Balance in {currency}</div>
			<div class="text-xl tabular-nums font-semibold">
				{formatAccountAmount(ledgerBalance)}
			</div>
			{#if Math.abs(difference) > 0.004}
				<div class="text-xs text-amber-700 tabular-nums">
					{formatAccountAmount(difference)} still to reconcile
				</div>
			{:else}
				<div class="text-xs text-emerald-700">Fully reconciled</div>
			{/if}
			<!--
				Xero puts "Different balances?" under the balance it explains, and
				opens the explanation over the page rather than beside it: the header
				never moves to make room for its own help.
			-->
			<button
				type="button"
				class="link-xero mt-1 text-[13px]"
				aria-expanded={balanceHelpOpen}
				onclick={() => (balanceHelpOpen = !balanceHelpOpen)}
			>
				Different balances?
			</button>
			{#if balanceHelpOpen}
				<div
					class="absolute left-0 top-full z-30 mt-1 w-[340px] max-w-[calc(100vw-4rem)] rounded-[3px] border border-xero-border bg-white p-3 text-left shadow-pop"
					role="dialog"
					aria-label="Different balances?"
				>
					<p class="text-[13px] leading-5 text-xero-muted">
						<span class="font-semibold text-xero-ink">Statement balance</span> is what the bank says:
						{formatCurrency(statementBalance, currency)} — the last statement's closing balance plus
						every line it has sent since.
					</p>
					<p class="mt-2 text-[13px] leading-5 text-xero-muted">
						<span class="font-semibold text-xero-ink">Balance in {currency}</span> is what the books
						say: {formatCurrency(ledgerBalance, currency)}, the postings already made to this account.
					</p>
					<p class="mt-2 text-[13px] leading-5 text-xero-muted">
						{#if Math.abs(difference) > 0.004}
							The {formatCurrency(difference, currency)} between them is the statement lines still in
							the reconcile inbox — the two agree once those are dealt with.
						{:else}
							The two agree, so nothing is outstanding on this account.
						{/if}
					</p>
					<div class="mt-3 text-right">
						<button
							type="button"
							class="btn-xero-secondary"
							onclick={() => (balanceHelpOpen = false)}
						>
							Close
						</button>
					</div>
				</div>
			{/if}
		</div>
		<div class="flex flex-col items-end gap-2">
			<a class="btn-primary" href="/app/accounting/bank-accounts/{accountId}/import">
				Import statement
			</a>
			<!-- Xero's own two header links, kept where Xero keeps them: beside the balance. -->
			<div class="flex flex-wrap items-center justify-end gap-4 text-[13px]">
				<a class="link-xero" href="/app/accounting/bank-accounts/{accountId}/edit">
					Manage Account
				</a>
				<a class="link-xero" href="/app/reports/bank-reconciliation">Reconciliation Report</a>
			</div>
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
	<!--
		Xero's reconcile list, rebuilt on Xero's principle: every row is two
		sides with a narrow OK between them. The left side is the line as the
		bank sent it — nobody edits it, it is the evidence. The right side is
		what we are going to make of it. Reconciling is the act of moving
		something from the right onto the left, and the OK button in the middle
		is where the user says the two belong together.
	-->
	<section class="overflow-hidden rounded-md border border-xero-border bg-white">
		<header
			class="px-4 py-3 border-b border-xero-rule flex flex-wrap items-center justify-between gap-3"
		>
			<h2 class="text-[15px] font-semibold text-xero-ink">
				Reconcile <span class="font-normal text-xero-muted">({inbox.length})</span>
			</h2>
			<div class="flex flex-wrap items-center gap-2">
				<input
					class="input-xero w-64"
					type="search"
					placeholder="Search payee, reference, amount or cheque no."
					value={lineSearch}
					oninput={(e) => searchInbox(e.currentTarget.value)}
				/>
				<button
					type="button"
					class="btn-xero-secondary"
					aria-expanded={filterOpen}
					onclick={() => (filterOpen = !filterOpen)}
				>
					Filter
					{#if appliedCount > 0}
						<span class="ml-1 rounded-full bg-brand-100 px-1.5 text-[11px] font-semibold text-brand-800">
							{appliedCount}
						</span>
					{/if}
				</button>
				<a class="btn-xero-secondary" href="/app/accounting/bank-rules">Bank rules</a>
				<button
					type="button"
					class="btn-xero-secondary"
					onclick={ignoreSelected}
					disabled={selectedIds.length === 0}
				>
					Ignore selected
				</button>
				<button type="button" class="btn-xero" onclick={autoReconcile} disabled={panelBusy}>
					{panelBusy ? 'Working…' : "Ok, let's reconcile"}
				</button>
			</div>
		</header>

		{#if filterOpen}
			<!--
				Xero's Filter panel: the four bounds, plus the dates the account
				itself spans. Nothing is narrowed until Apply, so the panel can be
				filled in without the list jumping around underneath it.
			-->
			<div class="px-4 py-4 border-b border-xero-rule bg-[#f7f9fb]">
				<div class="flex flex-wrap items-center justify-between gap-2 mb-3">
					<h3 class="text-[13px] font-semibold text-xero-ink">Filter</h3>
					<span class="text-[13px] text-xero-muted">
						{appliedCount === 0 ? 'No filters applied' : `${appliedCount} filter(s) applied`}
					</span>
				</div>
				<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 items-end">
					<label class="text-[13px] text-xero-ink">
						<span class="block text-[13px] text-xero-label mb-1">Start date</span>
						<input class="input-xero" type="date" bind:value={filters.from} />
					</label>
					<label class="text-[13px] text-xero-ink">
						<span class="block text-[13px] text-xero-label mb-1">End date</span>
						<input class="input-xero" type="date" bind:value={filters.to} />
					</label>
					<label class="text-[13px] text-xero-ink">
						<span class="block text-[13px] text-xero-label mb-1">Min amount</span>
						<input class="input-xero tabular-nums" type="number" step="0.01" bind:value={filters.min} />
					</label>
					<label class="text-[13px] text-xero-ink">
						<span class="block text-[13px] text-xero-label mb-1">Max amount</span>
						<input class="input-xero tabular-nums" type="number" step="0.01" bind:value={filters.max} />
					</label>
				</div>
				<div class="flex flex-wrap items-center gap-3 mt-4">
					<button type="button" class="btn-xero" onclick={applyFilters}>Apply</button>
					<button
						type="button"
						class="btn-xero-secondary"
						onclick={resetFilters}
						disabled={appliedCount === 0 && Object.values(filters).every((v) => v === '')}
					>
						Reset filters
					</button>
					{#if suggestedRange}
						<button
							type="button"
							class="link-xero text-[13px]"
							title="Fill in the dates this account's statement lines already span"
							onclick={useSuggestedDates}
						>
							Suggested dates: {formatDate(suggestedRange.from)} – {formatDate(suggestedRange.to)}
						</button>
					{/if}
				</div>
				<p class="text-[13px] text-xero-muted mt-2">
					Amounts are compared without their sign, so a debit of 42.50 is inside a range of
					10 to 50.
				</p>
				{#if filters.min && filters.max && Number(filters.min) > Number(filters.max)}
					<p class="text-xs text-amber-700 mt-1">
						The minimum is above the maximum, so nothing will match.
					</p>
				{/if}
			</div>
		{/if}

		{#if autoReport}
			<!--
				Xero's auto-reconcile banner. It reports what the button did rather
				than what happens to be reconciled, and it is where the setting
				itself lives — the number and the switch belong together.
			-->
			<div
				class="px-4 py-2.5 border-b border-xero-rule bg-xero-band flex flex-wrap items-center justify-between gap-3"
			>
				<p class="text-[13px] text-xero-ink">
					{#if autoReport.Total > 0}
						{autoReport.AutoReconciled} of {autoReport.Total} statement lines imported in the last
						{autoReport.Days} days were auto-reconciled
						{#if autoReport.UnreconciledLeft > 0}
							<span class="muted">· {autoReport.UnreconciledLeft} still to reconcile</span>
						{/if}
					{:else}
						No statement line imported in the last {autoReport.Days} days
					{/if}
				</p>
				<div class="flex flex-wrap items-center gap-3 text-[13px]">
					<button
						type="button"
						class="link-xero"
						onclick={() => setTab('statements')}
					>
						Review reconciled
					</button>
					<button
						type="button"
						class="btn-xero-secondary"
						onclick={() => toggleAutoReconcile(!autoReport?.Enabled)}
						disabled={panelBusy || !autoReport}
					>
						{autoReport.Enabled ? 'Turn auto-reconcile off' : 'Turn auto-reconcile on'}
					</button>
					{#if autoReport.Enabled}
						<span class="text-xs text-emerald-700">On</span>
					{/if}
				</div>
			</div>
		{/if}

		{#if inbox.length === 0}
			<p class="p-8 text-center text-[13px] text-xero-muted">
				{#if appliedCount > 0 || lineSearch}
					No statement line in the inbox matches that filter.
				{:else}
					Nothing to reconcile — every statement line has been dealt with.
				{/if}
			</p>
		{:else}
			<div class="overflow-x-auto" data-reconcile-list>
				<!--
					The band carries the list's side gutter, and it is the band that owns
					it rather than the page. Xero's #ItemsToReconcile is 1200 wide with
					`padding: 0 24px 40px`, and the 1150px column of rows sits inside it at
					x=25. Without that padding the rows run edge to edge and every bordered
					block reads as pinned to the band instead of laid out on it — and the
					instruction row's rules, which Xero draws under the two columns and not
					across the gutter between them, have nowhere to stop.
				-->
				<div class="min-w-[1198px] bg-xero-band px-6">
					<!--
						The instruction row, and the rule Xero draws beneath it. Xero
						measures that rule at 1px of #a6a9b0 — darker than the rules inside
						a row, because it is the one that separates the instructions from
						the work. The palette has no token for it, so it is named here.
					-->
					<div class="group relative mb-3 flex items-stretch bg-xero-band text-[13px] text-xero-muted">
						<!--
							Xero rules the two columns separately — a 532px rule under the
							statement column and another under the Xero column, with the OK
							gutter between them left open — rather than drawing one rule across
							the row. Measured at `margin: 8px 0 0` below the text, 1px of
							#a6a9b0, which is darker than the rule inside a row because it is
							the one that separates the instructions from the work.
						-->
						<div class="basis-[46.26%] shrink-0 min-w-0 border-b border-[#a6a9b0] pt-1.5 pb-2 pr-4">
							<!--
								Xero's "What's this?" sits above the instruction text, at the
								left of the statement column, and opens a tip that overlays the
								list rather than pushing it down.
							-->
							<div class="relative" data-tips>
								<button
									type="button"
									class="link-xero ml-2.5 text-[13px] font-bold underline"
									aria-expanded={tipsOpen}
									onclick={() => (tipsOpen = !tipsOpen)}
								>
									What's this?
								</button>
								{#if tipsOpen}
									<div
										class="absolute left-0 top-full z-30 mt-1 w-[430px] max-w-[calc(100vw-4rem)] rounded-[3px] border border-xero-border bg-white p-3 text-left shadow-pop"
										role="dialog"
										aria-label="What's this?"
									>
										<p class="text-[13px] leading-5 text-xero-muted">
											Every line has two halves. On the left is the statement line exactly as the bank
											sent it — the bank's evidence, and the thing you are checking against, so it is
											never edited.
										</p>
										<p class="mt-2 text-[13px] leading-5 text-xero-muted">
											On the right is what you decide the line is: Create codes it into a new
											transaction, Match ties it to one already in Xero, Transfer moves it to another
											account, and Discuss leaves the question for someone else.
										</p>
										<p class="mt-2 text-[13px] leading-5 text-xero-muted">
											The OK between the two halves is what ties them together — nothing is
											reconciled until you press it.
										</p>
										<div class="mt-3 text-right">
											<button
												type="button"
												class="btn-xero-secondary"
												onclick={() => (tipsOpen = false)}
											>
												Close
											</button>
										</div>
									</div>
								{/if}
							</div>
							<!--
								The select-all is goXero's, not Xero's, so it is kept out of the
								text's way: Xero starts "Review your bank statement lines…" flush
								with the column, and a checkbox in the flow would push it 28px in
								and make the instruction row read as a different layout. It sits
								in the band's own 24px gutter instead, where Xero has nothing.
							-->
							<div class="flex items-center">
								<input
									type="checkbox"
									class="absolute -left-[22px] top-[26px] transition group-hover:opacity-100 focus-visible:opacity-100 {selectedIds.length > 0
										? 'opacity-100'
										: 'opacity-0'}"
									checked={selectedIds.length > 0 && selectedIds.length === inbox.length}
									onchange={toggleAll}
									title="Select every line"
								/>
								<span>Review your bank statement lines…</span>
							</div>
						</div>
						<div class="basis-[7.13%] shrink-0"></div>
						<div class="flex flex-1 items-end border-b border-[#a6a9b0] bg-xero-band pt-1.5 pb-2">
							…then match with your transactions in Xero
						</div>
					</div>


					{#each inbox as l (l.StatementLineID)}
						{@const amount = Number(l.Amount ?? 0)}
						<!--
							`tab` is the tab strip's answer for every row, opened or not, and
							`ok` is the commit that tab carries — both read off what has been
							typed into the row, so the two can never disagree.
						-->
						{@const tab: PanelKind = shownTab(l)}
						{@const ok = rowOk(l)}
						{@const suggested = isSuggested(l)}
						{@const lineCurrency = l.CurrencyCode ?? currency}
						{@const menuOpen = optionsFor === l.StatementLineID}
						<div class="group mb-5" data-row={l.StatementLineID}>
							<!--
								The row's header: the bank's own line, the OK that commits it, and
								the Xero side. The three-step find & match form is deliberately NOT
								part of this line. Xero renders it at the row's full width, under
								the header and outside the right-hand column, so that the column
								stays column-width and the form gets the whole row to lay its
								filters and results out in.
							-->
							<div class="flex items-stretch">
								<!--
									The statement side: the line exactly as the bank sent it, and
									deliberately read-only — it is the evidence. Xero settles the row
									around a bordered block with Spent and Received against its right
									edge, so the column is that block with a strip of its own controls
									above it.
								-->
								<div class="basis-[46.26%] shrink-0 min-w-0 flex flex-col">
									<div class="flex h-[39px] items-start gap-3 pl-4 pt-1.5">
										<input
											type="checkbox"
											class="mt-2 transition group-hover:opacity-100 focus-visible:opacity-100 {selected[
												l.StatementLineID
											]
												? 'opacity-100'
												: 'opacity-0'}"
											checked={!!selected[l.StatementLineID]}
											onchange={() => toggle(l.StatementLineID)}
										/>
										<div class="relative ml-auto shrink-0" data-line-options>
											<button
												type="button"
												class="link-xero px-3 py-1.5 text-[13px] font-bold"
												aria-label="Options"
												aria-haspopup="menu"
												aria-expanded={menuOpen}
												title="Options for this statement line"
												onclick={(e) => openOptions(l.StatementLineID, e)}
											>
											Options <span aria-hidden="true">▾</span>
											</button>
											{#if menuOpen}
												<div
													class="absolute right-0 z-20 w-52 card p-1 {optionsUp
														? 'bottom-full mb-1'
														: 'top-full mt-1'}"
													role="menu"
												>
													<a
														class="block rounded px-3 py-2 text-sm text-ink-700 hover:bg-ink-50"
														role="menuitem"
														href={rowRuleHref(l)}
													>
														Create bank rule
													</a>
													<button
														type="button"
														class="block w-full rounded px-3 py-2 text-left text-sm text-ink-700 hover:bg-ink-50"
														role="menuitem"
														onclick={() => { optionsFor = null; ignoreLine(l.StatementLineID); }}
													>
														Ignore statement line
													</button>
													<button
														type="button"
														class="block w-full rounded px-3 py-2 text-left text-sm text-ink-700 hover:bg-ink-50"
														role="menuitem"
														onclick={() => deleteLine(l)}
													>
														Delete statement line
													</button>
												</div>
											{/if}
										</div>
									</div>
									<!-- The bordered evidence block: the line, then the two amounts. -->
									<div class="flex min-h-[112px] items-stretch border border-xero-rule bg-white">
										<div class="min-w-0 flex-1 px-4 py-2.5">
											{#if compact}
												<div class="flex items-baseline gap-3">
													<span class="text-[13px] tabular-nums text-xero-ink shrink-0">
														{formatDate(l.PostedAt)}
													</span>
													<span class="text-[15px] text-xero-ink truncate">
														{l.Payee || l.Counterparty || '—'}
													</span>
												</div>
											{:else}
												<div class="text-[13px] leading-[18px] tabular-nums text-xero-ink">
													{formatDate(l.PostedAt)}
												</div>
												<div class="text-[15px] leading-[21px] text-xero-ink truncate">
													{l.Payee || l.Counterparty || '—'}
												</div>
												{#if l.Reference}
													<div class="text-[15px] leading-[21px] text-xero-ink truncate">{l.Reference}</div>
												{/if}
												{#if l.Description && l.Description !== l.Payee}
													<div class="text-[13px] leading-[18px] text-xero-muted truncate">{l.Description}</div>
												{/if}
												<button
													type="button"
													class="link-xero text-[13px] leading-[18px] underline"
													title="Read the line exactly as the bank sent it"
													onclick={() => (details = l)}
												>
													More details
												</button>
											{/if}
										</div>
										<!--
											Xero rules the two amount cells off from the details
											and from each other with 1px of the same rule colour
											as the block's own top and bottom edge.
										-->
										<div class="w-[110px] shrink-0 border-l border-xero-rule py-2.5 pr-4 text-right">
											<div class="text-[11px] leading-[14px] text-xero-label">Spent</div>
											<div class="text-[15px] leading-[30px] tabular-nums text-xero-ink">
												{amount < 0 ? formatAmount(Math.abs(amount)) : ''}
											</div>
										</div>
										<div class="w-[110px] shrink-0 border-l border-xero-rule py-2.5 pr-4 text-right">
											<div class="text-[11px] leading-[14px] text-xero-label">Received</div>
											<div class="text-[15px] leading-[30px] tabular-nums text-xero-ink">
												{amount >= 0 ? formatAmount(amount) : ''}
											</div>
										</div>
									</div>
								</div>
								<!--
									The narrow OK between the two sides: the commit for whichever tab
									is on show, and the only commit Create and Transfer have, because
									Xero puts no save button inside either form. Discuss has none — a
									note is saved with Ctrl + S, as its own hint says.

									It is pinned to the top of the row in a fixed gutter and not
									stretched down the column. Measured on Xero, the button sits at
									y=79..111 whatever the row is doing, so it stays where the hand
									left it when the find & match form pushes the row down half a
									screen; stretching the column instead is what used to send the
									button drifting away from the row it belongs to.
								-->
								<div class="basis-[7.13%] shrink-0 self-start flex justify-center px-1 pt-[79px]">
									{#if ok}
										<button
											type="button"
											class="btn-xero-ok"
											title={ok.title}
											disabled={panelBusy}
											onclick={ok.run}
										>
											OK
										</button>
									{/if}
								</div>

								<!--
									The Xero side: what we are going to make of the line. A tab strip
									across the top — Match, Create, Transfer, Discuss, and the smaller
									Find & Match at the far right — then the panel of whichever tab is
									on show, and nothing else. The three-step find & match form is not
									here: Xero lays it out at the row's full width, under this header
									and outside the column, and only for Match — measured, Create and
									Transfer leave the row its own height (194px and 151px) while
									Match opens it to 1030px. Drawing that form under every tab is
									what used to grow the row out from under the cursor the moment a
									tab was clicked.
								-->
								<!--
									Xero's right column is a fixed 532 — the row's own 1150 is
									4px wider than its three columns put together, and that slack
									is left at the row's right edge rather than handed to the
									panel. A flex-1 column takes those 4px instead, which puts the
									panel's right border on the band's edge and quietly widens
									every field, tab and rule inside it by the same 4px.
								-->
								<div class="min-w-0 basis-[46.26%] shrink-0 flex flex-col">
									<div class="flex h-[39px] items-stretch" role="tablist">
										{#each lineTabs as t (t.id)}
											<button
												type="button"
												role="tab"
												aria-selected={tab === t.id}
												class="border-b-2 px-5 pt-[11px] pb-[7px] text-[15px] {tab === t.id
													? 'border-xero-blue text-xero-blue'
													: 'border-transparent text-xero-label hover:text-xero-blue'}"
												onclick={() => openPanel(t.id, l)}
											>
												{t.label}{#if t.id === 'discuss' && (l.CommentCount ?? 0) > 0}<span
														aria-label="has comments"> *</span>{/if}
											</button>
										{/each}
										<button
											type="button"
											class="ml-auto border-b-2 border-transparent pl-[18px] pr-4 pt-3 pb-2 text-[13px] font-bold text-xero-blue hover:underline"
											title="Search every unreconciled transaction for a match"
											onclick={() => openPanel('match', l)}
										>
											Find &amp; Match
										</button>
									</div>
									<div class="min-h-[112px] flex-1 border border-xero-rule bg-white">
										{#if tab === 'match'}
											<!--
											Xero's Match tab is not a form of its own: it is the standing answer to
											"what is this line?" — the finding and the selecting happen in step 1
											below, at the row's full width. Xero's box is 112 tall and grey and says
											what it is for.
											-->
											<div
												class="flex h-[112px] flex-col items-center justify-center bg-[#f2f3f4] px-4 text-center"
											>
												<p class="text-[13px] font-bold text-xero-ink">Find &amp; Match</p>
												<p class="mt-1 text-[15px] text-xero-muted">
													Find &amp; select matching transactions below
												</p>
											</div>
										{:else if tab === 'create'}
											{@const basis = prefillBasis(l)}
											<!--
												Xero paints the answer it worked out for itself, so that the one
												thing on the screen nobody typed is also the one thing that is
												marked: `.xoDone .create .t2` is #bae58c with #2d7300 on it,
												background and colour together. The panel is painted and not one
												field inside it, because the suggestion is the panel's answer: the
												fill and the ink sit on the block and the heading, the labels and
												the fields inside inherit them. The paint comes off the moment the
												user answers differently — which is what isSuggested() reads. The
												1px edge is Xero's #a7d35b; it is drawn inset because these panels
												carry no border of their own, and a border on some rows would make
												those rows taller.
											-->
											<div
												class="px-4 py-4 {suggested
													? 'bg-xero-done text-xero-doneInk ring-1 ring-inset ring-xero-doneRule'
													: ''}"
											>
												<!--
													Xero labels these fields to the *left* of their controls and runs Who
													and What side by side on the first row — 16px between the two groups —
													then Why on the next, so the three answers read as one sentence rather
													than three stacked forms. Each label is a fixed 35/39px column, which is
													what lands the controls on Xero's measured x; a wider label plus a gap is
													what used to push What onto a line of its own.
												-->
												<div class="flex items-center gap-x-4">
													<div class="relative flex min-w-0 flex-1 items-center" data-xero-picker>
														<label class="flex min-w-0 flex-1 items-center">
															<span
															class="w-[35px] shrink-0 text-[13px]"
															>Who</span
														>
															<input
																class="input-xero min-w-0 flex-1"
																placeholder="Name of the contact..."
																autocomplete="off"
																value={coding[l.StatementLineID]?.ContactName ?? ''}
																oninput={(e) =>
																	onWhoInput(l.StatementLineID, e.currentTarget.value)}
																onfocus={() => openPicker(l.StatementLineID, 'who')}
																onkeydown={(e) => pickerKey(e, l.StatementLineID, 'who')}
															/>
														</label>
														<!--
															The list is drawn inside the page, under the field it belongs
															to. A native datalist is drawn by the browser out of the page's
															reach, which is exactly how a suggestion ends up floating
															somewhere other than under its own field once the row moves.
														-->
														{#if pickerOpen(l.StatementLineID, 'who')}
															<div
																class="absolute left-[35px] right-0 top-full z-30 mt-[2px] max-h-64 overflow-y-auto card p-1"
																role="listbox"
																aria-label="Contacts"
															>
																{#each pickerOptions() as o, i (i)}
																	<button
																		type="button"
																		role="option"
																		aria-selected={i === pickerIndex}
																		class="block w-full rounded px-3 py-1.5 text-left text-[13px] {i ===
																		pickerIndex
																			? 'bg-xero-blue text-white'
																			: 'text-ink-700 hover:bg-ink-50'}"
																		onmousedown={(e) => e.preventDefault()}
																		onclick={() =>
																			pick(l.StatementLineID, 'who', (o as Contact).Name)}
																	>
																		{(o as Contact).Name}
																	</button>
																{/each}
															</div>
														{/if}
													</div>
													<div class="relative flex min-w-0 flex-1 items-center" data-xero-picker>
														<label class="flex min-w-0 flex-1 items-center">
															<span
															class="w-[39px] shrink-0 text-[13px]"
															>What</span
														>
															<input
																class="input-xero min-w-0 flex-1 pr-[38px]"
																placeholder="Choose the account..."
																autocomplete="off"
																value={accountLabel(coding[l.StatementLineID]?.AccountCode ?? '')}
																oninput={(e) =>
																	onWhatInput(l.StatementLineID, e.currentTarget.value)}
																onfocus={() => openPicker(l.StatementLineID, 'what')}
																onkeydown={(e) => pickerKey(e, l.StatementLineID, 'what')}
															/>
														</label>
														<!--
															Xero's whole What control is one 197x30 box: the arrow is drawn
															*inside* the field, 8px from its right edge, rather than standing
															beside it. The 30px a button beside the field costs is exactly what
															used to wrap this row.
														-->
														<button
															type="button"
															class="absolute top-1/2 right-[8px] flex h-[29px] w-[30px] -translate-y-1/2 items-center justify-center text-[11px] text-xero-label hover:text-xero-blue"
															aria-label="Choose an account"
															onclick={() =>
																pickerOpen(l.StatementLineID, 'what')
																	? closePicker()
																	: openPicker(l.StatementLineID, 'what')}
														>
															<span aria-hidden="true">▾</span>
														</button>
														{#if pickerOpen(l.StatementLineID, 'what')}
															<div
																class="absolute left-[39px] right-0 top-full z-30 mt-[2px] max-h-64 overflow-y-auto card p-1"
																role="listbox"
																aria-label="Accounts"
															>
																{#each pickerOptions() as o, i (i)}
																	<button
																		type="button"
																		role="option"
																		aria-selected={i === pickerIndex}
																		class="block w-full rounded px-3 py-1.5 text-left text-[13px] {i ===
																		pickerIndex
																			? 'bg-xero-blue text-white'
																			: 'text-ink-700 hover:bg-ink-50'}"
																		onmousedown={(e) => e.preventDefault()}
																		onclick={() =>
																			pick(l.StatementLineID, 'what', (o as Account).Code)}
																	>
																		{codingLabel((o as Account).Code, (o as Account).Name)}
																	</button>
																{/each}
															</div>
														{/if}
													</div>
												</div>
												<div class="mt-4 flex items-center">
													<label class="flex min-w-0 flex-1 items-center">
														<span
															class="w-[34px] shrink-0 text-[13px]"
															>Why</span
														>
														<input
															class="input-xero min-w-0 flex-1"
															placeholder="Enter a description..."
															value={coding[l.StatementLineID]?.Description ?? ''}
															oninput={(e) =>
																setCoding(l.StatementLineID, 'Description', e.currentTarget.value)}
														/>
													</label>
												</div>
												<!-- Xero puts "Add details" at the panel's right edge. -->
												<!--
													The suggestion line is goXero's, not Xero's, and is kept: it is
													why the row already knows what to suggest. It sits below the
													three rows rather than above them, because above them it moved
													them: Xero measures Who at 17px from the panel's top edge and
													this line put it at 53. Everything the row was going to say is
													still said; only the space it says it in is now the space Xero
													leaves at the bottom of the panel.
												-->
												<div class="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px]">
													<span>
														{basis ? suggestionBasis(basis) : createHeading(l)}
													</span>
													{#if basis?.LastUsedAt}
														<span class={suggested ? '' : 'muted'}
															>last used {formatDate(basis.LastUsedAt)}</span
														>
													{/if}
													{#if hasPrefill(l)}
														<button
															type="button"
															class="link-xero text-[13px]"
															onclick={() => clearSuggestion(l)}
														>
															Clear it
														</button>
													{/if}
												</div>
												<div class="-mr-4 mt-4 flex justify-end">
													<button
														type="button"
														class="link-xero whitespace-nowrap py-1 pl-3 text-[13px]"
														onclick={() =>
															(showDetails = {
																...showDetails,
																[l.StatementLineID]: !showDetails[l.StatementLineID]
															})}
													>
														{showDetails[l.StatementLineID] ? 'Hide details' : 'Add details'}
													</button>
												</div>
												{#if showDetails[l.StatementLineID]}
													<div class="mt-2 flex flex-wrap items-center gap-x-5 gap-y-2">
														<label class="flex items-center gap-2">
															<span class="shrink-0 text-[13px] text-xero-ink">Region</span>
															<select class="input-xero w-[181px]" value={taxRegion} disabled>
																<option value={taxRegion}>{taxRegion || '—'}</option>
															</select>
														</label>
														<label class="flex items-center gap-2">
															<span class="shrink-0 text-[13px] text-xero-ink">Tax Rate</span>
															<select
																class="input-xero w-[181px]"
																value={coding[l.StatementLineID]?.TaxType ?? ''}
																onchange={(e) =>
																	setCoding(l.StatementLineID, 'TaxType', e.currentTarget.value)}
															>
																<option value="">No tax</option>
																{#each taxRates as r (r.TaxRateID)}
																	<option value={r.TaxType}>{r.Name}</option>
																{/each}
															</select>
														</label>
														<label class="flex items-center gap-2">
															<span class="shrink-0 text-[13px] text-xero-ink">Reference</span>
															<input
																class="input-xero w-[181px]"
																value={coding[l.StatementLineID]?.Reference ?? ''}
																oninput={(e) =>
																	setCoding(l.StatementLineID, 'Reference', e.currentTarget.value)}
															/>
														</label>
													</div>
												{/if}
											</div>
										{:else if tab === 'transfer'}
												<div class="p-4">
													<!--
														Xero's Transfer panel is two columns on one row, not three rows:
														the legend and the Reference label sit on the same line, and the radio
														and the Reference field sit on the next one, side by side.
														Measured live (div.info.c4): legend at (17,21) 22 tall, its radio
														input at (24,52) 16x16, the radio's label text at (46,51) 154x21,
														and the Reference column at x266 with its label at y21 and a
														249x34 input directly under it. The panel is 30px deeper at the
														bottom than its contents need.
													-->
													<div class="flex items-start">
														<fieldset class="w-[249px] shrink-0">
															<legend class="block text-[13px] leading-[22px] text-xero-ink"
																>Select a bank account</legend
															>
															{#if otherBankAccounts.length === 0}
																<p class="mt-[1px] text-[13px] text-xero-muted">
																	There is no other bank account to transfer to. Add one first.
																</p>
															{:else}
																<ul class="mt-[1px]">
																	{#each otherBankAccounts as a (a.AccountID)}
																		<li class="h-[32px]">
																			<label
																				class="flex h-[32px] items-center gap-[6px] pl-[7px] text-[13px] text-xero-ink"
																			>
																				<input
																					type="radio"
																					class="h-[16px] w-[16px] shrink-0"
																					name="transfer-{l.StatementLineID}"
																					value={a.AccountID}
																					checked={transferTo === a.AccountID}
																					onchange={() => (transferTo = a.AccountID)}
																				/>
																				<!-- Xero labels the radio with the account NAME alone; the code
																					 is a reference, not part of the answer. -->
																				<span class="leading-[21px]">{a.Name}</span>
																			</label>
																		</li>
																	{/each}
																</ul>
															{/if}
														</fieldset>
														<label class="block w-[249px] shrink-0">
															<span class="block text-[13px] leading-[22px] text-xero-ink">Reference</span>
															<input class="input-xero w-full" bind:value={transferReference} />
														</label>
													</div>
												</div>
											{:else if tab === 'discuss'}
											<!--
											Xero's Discuss: the question about a line belongs on the line. Everything
											else on this screen is about deciding what the line is; this is about
											asking someone who knows.

											The note box is the whole of it in Xero — 487x86, three rows, at the
											panel's own 17px inset — with the Ctrl + S hint right-aligned under it.
											Xero keeps that hint on show whether or not the box has anything in it;
											goXero hid it while you typed, which moved the box under the cursor.

											The thread below the box is goXero's, not Xero's: Xero shows only the
											note box and the comment's own text. Keeping the thread is a deliberate
											superset, and it sits after the box so the box keeps its geometry.
											-->
											<div class="px-4 pt-5 pb-[30px]">
												<label class="block">
													<!-- Xero's "Comment" label is there for screen readers only. -->
													<span class="sr-only">Comment</span>
													<textarea
														class="input-xero block h-[86px] w-full"
														rows="3"
														bind:value={commentDraft}
													></textarea>
												</label>
												<div class="mt-[5px] text-right text-[13px] leading-6 text-xero-muted">
													Ctrl + S at any time to save
												</div>
												{#if threadBusy && comments.length === 0}
													<p class="mt-4 text-[13px] text-xero-muted">Loading…</p>
												{:else if comments.length > 0}
													<ul class="mt-4 space-y-3">
														{#each comments as c (c.CommentID)}
															<li class="text-[13px] text-xero-ink">
																<div
																	class="flex flex-wrap items-baseline gap-2 text-[13px] text-xero-muted"
																>
																	<span class="font-medium text-xero-ink">
																		{c.AuthorName || 'Someone'}
																	</span>
																	<span class="tabular-nums">{formatDate(c.CreatedDateUTC)}</span>
																</div>
																<p class="text-ink-900 whitespace-pre-wrap">{c.Body}</p>
															</li>
														{/each}
													</ul>
												{/if}
											</div>
										{/if}

									</div>

								</div>
							</div>
							{#if tab === 'match'}
								<!--
									The three steps, in Xero's order and under its headings, at the
									row's full width under the header. Xero draws them for Match and
									for nothing else — measured, a Create row stays 194px and a
									Transfer row 151px while a Match row opens to 1030px — so the
									form appearing under every tab was never Xero's behaviour, and it
									is what grew the row out from under the cursor on a tab click.
								-->
								<div class="border-t border-xero-rule px-4 py-3">
									<!--
									Xero's step 1 is one line, not four: the heading and its question mark
									hold the left, the two checkboxes stack in a narrow column beside them,
									and the search pair sits against the right edge with Go and Clear search
									after it. Each search box carries its own label rather than a placeholder,
									which is what lets the two read as one search column.
									-->
									<div class="flex flex-wrap items-start">
										<!-- 310px of heading puts the question mark where Xero has it. -->
										<div class="flex w-[334px] shrink-0 items-start">
											<span class="w-[310px] text-[13px] text-xero-ink">
												1. Find &amp; select matching transactions
											</span>
											<button
												type="button"
												class="ml-[5px] inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full border border-xero-border text-[10px] leading-none text-xero-label"
												title="Pick the transactions that are the same money as this statement line. Several of them may add up to it."
											>
												?
											</button>
										</div>
										<div class="ml-9 flex w-[171px] shrink-0 flex-col">
											<label class="flex items-center gap-1 text-[13px] text-xero-ink">
												<input
													type="checkbox"
													checked={matchFilters.showSpent}
													onchange={(e) => {
														matchFilters = { ...matchFilters, showSpent: e.currentTarget.checked };
														void loadMatchCandidates(l.StatementLineID, 1);
													}}
												/>
												Show Spent Items
											</label>
											<label class="mt-[20px] flex items-center gap-1 text-[13px] text-xero-ink">
												<input
													type="checkbox"
													checked={matchFilters.currencyOnly}
													onchange={(e) => {
														matchFilters = {
															...matchFilters,
															currencyOnly: e.currentTarget.checked
														};
														void loadMatchCandidates(l.StatementLineID, 1);
													}}
												/>
												Show {lineCurrency} items only
											</label>
										</div>
										<!--
										ml-auto, so the column sits against the right edge the way Xero's
										floated search form does, however wide the row turns out to be.
										-->
										<div class="ml-auto flex flex-wrap items-start gap-x-[13px]">
											<label class="block shrink-0">
												<span class="mb-[3px] block whitespace-nowrap text-[13px] font-bold text-xero-label">
													Search by name or reference
												</span>
												<input
													class="input-xero h-[40px] w-[179px]"
													aria-label="Search by name or reference"
													value={matchFilters.search}
													oninput={(e) => (matchFilters = { ...matchFilters, search: e.currentTarget.value })}
													onkeydown={(e) => {
														if (e.key === 'Enter') runMatchSearch(l.StatementLineID);
													}}
												/>
											</label>
											<label class="block shrink-0">
												<span class="mb-[3px] block whitespace-nowrap text-[13px] font-bold text-xero-label">
													Search by amount
												</span>
												<input
													class="input-xero h-[40px] w-[172px]"
													aria-label="Search by amount"
													value={matchFilters.amount}
													oninput={(e) => (matchFilters = { ...matchFilters, amount: e.currentTarget.value })}
													onkeydown={(e) => {
														if (e.key === 'Enter') runMatchSearch(l.StatementLineID);
													}}
												/>
											</label>
											<button
												type="button"
												class="btn-xero mt-auto"
												onclick={() => runMatchSearch(l.StatementLineID)}
											>
												Go
											</button>
											<button
												type="button"
												class="link-xero mt-auto text-[13px]"
												onclick={() => clearMatchSearch(l.StatementLineID)}
											>
												Clear search
											</button>
										</div>
									</div>

									<div class="mt-3 overflow-x-auto">
										<table class="w-full border-collapse text-[13px]">
											<thead>
												<tr class="bg-xero-band">
													<th class="w-6 border border-xero-rule px-2 py-1"></th>
													{#each matchColumns as col (col.key)}
														<th
															class="border border-xero-rule px-2 py-1 text-left font-normal whitespace-nowrap"
														>
															<button
																type="button"
																class="link-xero font-normal"
																onclick={() => sortMatchBy(col.key)}
															>
																{col.label}{#if matchSort.key === col.key}<span aria-hidden="true"
																		> {matchSort.dir === 'asc' ? '↑' : '↓'}</span
																	>{/if}
															</button>
														</th>
													{/each}
												</tr>
											</thead>
											<tbody>
												{#each sortedCandidates as c (c.BankTransactionID)}
													{@const received = isReceive(c)}
													<tr>
														<td class="border border-xero-rule px-2 py-1">
															<input
																type="checkbox"
																checked={matchIsSelected(c)}
																onchange={() => toggleMatchCandidate(c)}
															/>
														</td>
														<td
															class="border border-xero-rule px-2 py-1 tabular-nums whitespace-nowrap"
														>
															{formatDate(c.Date)}
														</td>
														<td class="border border-xero-rule px-2 py-1">
															<span aria-hidden="true" class="mr-1 text-xero-muted">
																{received ? '↓' : '↑'}
															</span>
															<button
																type="button"
																class="link-xero"
																onclick={() => showTransaction(c.BankTransactionID)}
															>
																{counterparty(c)}
															</button>
														</td>
														<td class="border border-xero-rule px-2 py-1">
															{c.Reference ?? ''}
														</td>
														<td
															class="border border-xero-rule px-2 py-1 text-right tabular-nums whitespace-nowrap"
														>
															{received
																? ''
																: formatAmountCode(c.Total ?? 0, c.CurrencyCode || lineCurrency)}
														</td>
														<td
															class="border border-xero-rule px-2 py-1 text-right tabular-nums whitespace-nowrap"
														>
															{received
																? formatAmountCode(c.Total ?? 0, c.CurrencyCode || lineCurrency)
																: ''}
														</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
									{#if matchCandidates.length === 0 && !panelBusy}
										<p class="mt-2 text-[13px] text-xero-muted">
											No unreconciled transaction in this account looks like this line.
										</p>
									{/if}
									<div class="mt-2 flex flex-wrap items-center justify-between gap-3">
										<label class="flex items-center gap-1 text-[13px] text-xero-ink">
											<input type="checkbox" checked={matchAllOnPage} onchange={toggleAllCandidates} />
											Select all on this page
										</label>
										<div class="flex items-center gap-3 text-[13px] text-xero-muted">
											{#if matchPage > 1}
												<button
													type="button"
													class="link-xero"
													onclick={() => loadMatchCandidates(l.StatementLineID, matchPage - 1)}
												>
													Previous
												</button>
											{/if}
											<span>Showing {matchRange.from} - {matchRange.to} of {matchRange.total}</span>
											{#if matchRange.to < matchRange.total}
												<button
													type="button"
													class="link-xero"
													onclick={() => loadMatchCandidates(l.StatementLineID, matchPage + 1)}
												>
													Next
												</button>
											{/if}
										</div>
									</div>

									<div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2">
										<span class="text-[13px] text-xero-ink">
											2. View your selected transactions. Add new transactions, as needed.
										</span>
										<label class="flex items-center gap-2">
											<span class="text-[13px] text-xero-ink">New Transaction</span>
											<select
												class="input-xero w-[160px]"
												value=""
												onchange={(e) => {
													if (e.currentTarget.value) void addNewTransaction(l);
													e.currentTarget.value = '';
												}}
											>
												<option value="">New Transaction</option>
												{#if amount < 0}
													<option value="SPEND">Spend money</option>
												{:else}
													<option value="RECEIVE">Receive money</option>
												{/if}
											</select>
										</label>
									</div>
									<div class="mt-2 min-h-[56px] rounded-[3px] border border-xero-border bg-white">
										{#if matchSelection.length === 0}
											<p class="px-3 py-4 text-[13px] text-xero-muted">
												No transactions have been selected
											</p>
										{:else}
											<ul>
												{#each matchSelection as t (t.BankTransactionID)}
													<li
														class="flex flex-wrap items-center justify-between gap-2 border-b border-xero-rule px-3 py-1.5 text-[13px] last:border-b-0"
													>
														<span class="min-w-0 truncate">
															<span class="tabular-nums text-xero-muted">
																{formatDate(t.Date)}
															</span>
															<span class="ml-2">{counterparty(t)}</span>
															{#if t.Reference}
																<span class="ml-2 muted">{t.Reference}</span>
															{/if}
														</span>
														<span class="flex items-center gap-3">
															<span class="tabular-nums">
																{formatCodeAmount(signedTotal(t), t.CurrencyCode || lineCurrency)}
															</span>
															<button
																type="button"
																class="link-xero"
																onclick={() => toggleMatchCandidate(t)}
															>
																Remove
															</button>
														</span>
													</li>
												{/each}
											</ul>
										{/if}
									</div>

									<div class="mt-4">
										<span class="text-[13px] text-xero-ink">
											3. The sum of your selected transactions must match the money {matchDirectionWord}.
											Make adjustments, as needed.
										</span>
										<div class="mt-2 flex flex-col items-end gap-1 text-[13px]">
											<div class="flex items-center gap-4">
												<span class="text-xero-ink">Must match money {matchDirectionWord}</span>
												<span class="w-[120px] text-right tabular-nums text-xero-ink">
													{formatCodeAmount(Math.abs(matchRequired), lineCurrency)}
												</span>
											</div>
											<div class="flex items-center gap-4">
												<span class="text-xero-ink">&nbsp;</span>
												<span class="w-[120px] text-right tabular-nums text-xero-ink">
													{formatCodeAmount(matchSelectedTotal, lineCurrency)}
												</span>
											</div>
											{#if Math.abs(matchDifference) >= 0.005}
												<div class="text-xero-negative flex items-center gap-4">
													<span>Total is out by:</span>
													<span class="w-[120px] text-right tabular-nums">
														{formatAmount(Math.abs(matchDifference))}
													</span>
												</div>
											{/if}
										</div>
									</div>

									<div
										class="mt-4 flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-xero-rule pt-3"
									>
										<span class="relative" data-adjustments>
											<button
												type="button"
												class="link-xero text-[13px]"
												onclick={() =>
													(adjustmentsFor =
														adjustmentsFor === l.StatementLineID ? null : l.StatementLineID)}
											>
												Adjustments <span aria-hidden="true">▾</span>
											</button>
											{#if adjustmentsFor === l.StatementLineID}
												<div
													class="absolute left-0 top-full z-20 mt-1 w-44 card p-1"
													role="menu"
												>
													<button
														type="button"
														class="block w-full rounded px-3 py-2 text-left text-[13px] text-ink-700 hover:bg-ink-50"
														role="menuitem"
														onclick={() => addAdjustment('BANK_FEE')}
													>
														Bank fee
													</button>
													<button
														type="button"
														class="block w-full rounded px-3 py-2 text-left text-[13px] text-ink-700 hover:bg-ink-50"
														role="menuitem"
														onclick={() => addAdjustment('MINOR_ADJUSTMENT')}
													>
														Minor adjustment
													</button>
												</div>
											{/if}
										</span>
										<button
											type="button"
											class="link-xero text-[13px] {matchBalanced ? '' : 'disabled'}"
											disabled={!matchBalanced || panelBusy}
											onclick={() => reconcileSelection(l)}
										>
											Reconcile
										</button>
										<button type="button" class="link-xero text-[13px]" onclick={cancelMatch}>
											Cancel
										</button>
									</div>
								</div>
							{/if}
						</div>

					{/each}
				</div>
			</div>

			<footer
				class="px-4 py-2.5 bg-xero-band border-t border-xero-rule text-[13px] text-xero-muted flex flex-wrap items-center justify-between gap-4"
			>
				<span class="flex flex-wrap gap-4">
					<span>{inbox.length} statement line(s) waiting</span>
					{#if balance}
						<span>
							{balance.ReconciledCount} reconciled · {balance.UnreconciledCount} unreconciled
						</span>
					{/if}
				</span>
				<div class="flex flex-wrap items-center gap-x-5 gap-y-2">
					<div class="flex items-center gap-2">
						<button
							type="button"
							role="switch"
							aria-checked={compact}
							aria-label="Compact view"
							class="relative h-4 w-7 shrink-0 rounded-full transition {compact
								? 'bg-xero-blue'
								: 'bg-xero-border'}"
							onclick={toggleCompact}
						>
							<span
								class="absolute top-0.5 left-0.5 h-3 w-3 rounded-full bg-white transition {compact
									? 'translate-x-3'
									: ''}"
							></span>
						</button>
						<span class="text-xero-ink">Compact view</span>
						<span
							class="text-[13px] text-xero-muted"
							title="One line per statement line, without the reference, description and details button."
						>
							· less to read
						</span>
					</div>
					<label class="flex items-center gap-2 text-xero-ink cursor-pointer">
						<input type="checkbox" checked={suggestPrevious} onchange={toggleSuggestPrevious} />
						<span>Suggest previous entries</span>
						<span
							class="text-[13px] text-xero-muted"
							title="Fill in the account and tax rate this bank account used the last time it saw the same payee."
						>
							· what you coded last time
						</span>
					</label>
				</div>
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
							<th class="px-3 py-2 text-left">Particulars</th>
							<th class="px-3 py-2 text-left">Code</th>
							<th class="px-3 py-2 text-left">Reference</th>
							<th class="px-3 py-2 text-left">Analysis Code</th>
							<th class="px-3 py-2 text-right">Spent</th>
							<th class="px-3 py-2 text-right">Received</th>
							<th class="px-3 py-2 text-right">Balance</th>
							<th class="px-3 py-2 text-left">Source</th>
							<th class="px-3 py-2 text-left">Status</th>
							<th class="px-3 py-2 text-right"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-ink-100">
						{#each statementRows as row (row.line.StatementLineID)}
							{@const amount = Number(row.line.Amount ?? 0)}
							{@const code = row.line.CodedAccountCode ?? ''}
							<tr class="hover:bg-ink-50">
								<td class="px-3 py-2 tabular-nums">
									{#if row.line.BankTransactionID}
										<button
											type="button"
											class="text-brand-700 hover:underline"
											title="Show the transaction this line became"
											onclick={() => showTransaction(row.line.BankTransactionID as string)}
										>
											{formatDate(row.line.PostedAt)}
										</button>
									{:else}
										{formatDate(row.line.PostedAt)}
									{/if}
								</td>
								<td class="px-3 py-2">{amount < 0 ? 'Debit' : 'Credit'}</td>
								<td class="px-3 py-2">{row.line.Payee || row.line.Counterparty || '—'}</td>
								<td class="px-3 py-2">{row.line.Description || ''}</td>
								<td class="px-3 py-2">
									{#if code}
										<div>{code}</div>
										<div class="text-xs muted">{row.line.CodedAccountName ?? ''}</div>
									{:else}
										<span class="muted">—</span>
									{/if}
								</td>
								<td class="px-3 py-2">{row.line.Reference || ''}</td>
								<td
									class="px-3 py-2 muted"
									title="Tracking categories are not captured on statement lines"
								>
									—
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{amount < 0 ? formatCurrency(Math.abs(amount), row.line.CurrencyCode ?? currency) : ''}
								</td>
								<td class="px-3 py-2 text-right tabular-nums">
									{amount >= 0 ? formatCurrency(amount, row.line.CurrencyCode ?? currency) : ''}
								</td>
								<td
									class="px-3 py-2 text-right tabular-nums"
									title={row.priced
										? "Balance printed on the bank's statement"
										: 'Running total of the lines below'}
								>
									{formatCurrency(row.balance, row.line.CurrencyCode ?? currency)}
								</td>
								<td class="px-3 py-2 muted">
									{row.line.Source === 'FEED' ? 'Bank feed' : 'Imported'}
								</td>
								<td class="px-3 py-2">
									{#if row.line.Status === 'IMPORTED'}
										<span class="text-emerald-700">Reconciled</span>
										<!--
											Only a coded line can carry this: while a line is still in
											the inbox the feed is free to correct it, and does.
										-->
										{#if row.line.UpstreamChange}
											<div class="mt-1 flex items-center gap-2">
												<span
													class="rounded bg-amber-100 px-1.5 py-0.5 text-[11px] font-semibold text-amber-800"
													title={upstreamDetail(row.line)}
												>
													{upstreamLabel(row.line)}
												</span>
												{#if row.line.UpstreamChange === 'REMOVED'}
													<!--
														A withdrawal is a stored flag the sync preserves, so
														dismissing it lasts. A change is derived from the bank's
														current version and would return on the next sync, so it
														gets no button.
													-->
													<button
														type="button"
														class="btn-ghost-sm"
														title="Stop showing this withdrawal"
														onclick={() => dismissUpstream(row.line)}
													>
														Dismiss
													</button>
												{/if}
											</div>
										{/if}
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
								<td class="px-3 py-2 text-right whitespace-nowrap">
									<button
										type="button"
										class="btn-ghost-sm"
										title="Read the line exactly as the bank sent it"
										onclick={() => (details = row.line)}
									>
										More details
									</button>
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
						<th class="px-3 py-2 text-left">Statement line</th>
						<th class="px-3 py-2 text-right">Spent</th>
						<th class="px-3 py-2 text-right">Received</th>
						<th class="px-3 py-2 text-right">Balance</th>
						<th class="px-3 py-2 text-left">Analysis Code</th>
						<th class="px-3 py-2 text-left">Source</th>
						<th class="px-3 py-2 text-left">Status</th>
						<th class="px-3 py-2 text-right"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-ink-100">
					{#each transactions as t (t.BankTransactionID)}
						{@const line = lineByTransaction.get(t.BankTransactionID)}
						{@const accountCode = t.LineItems?.[0]?.AccountCode ?? ''}
						<tr
							class="hover:bg-ink-50 {focusTransaction === t.BankTransactionID
								? 'bg-brand-50'
								: ''}"
						>
							<td class="px-3 py-2 tabular-nums">{formatDate(t.Date)}</td>
							<td class="px-3 py-2">{t.Contact?.Name ?? t.Type}</td>
							<td class="px-3 py-2">{t.Reference ?? ''}</td>
							<td class="px-3 py-2">
								{#if accountCode}
									<div>{accountCode}</div>
									<div class="text-xs muted">{accountName(accountCode)}</div>
								{:else}
									<span class="muted">—</span>
								{/if}
							</td>
							<td class="px-3 py-2">
								{#if line}
									<button
										type="button"
										class="text-left text-brand-700 hover:underline"
										title="Read the statement line this transaction came from"
										onclick={() => (details = line)}
									>
										{formatDate(line.PostedAt)}
										<span class="text-ink-600">
											· {line.Payee || line.Counterparty || '—'}</span
										>
									</button>
								{:else}
									<span class="muted">—</span>
								{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'SPEND'}{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? currency)}{/if}
							</td>
							<td class="px-3 py-2 text-right tabular-nums">
								{#if t.Type === 'RECEIVE'}{formatCurrency(t.Total ?? 0, t.CurrencyCode ?? currency)}{/if}
							</td>
							<!--
								Xero carries the Balance column on this tab too, and it is
								the balance of the statement line the transaction came from
								rather than of the transaction: that is what the bank's own
								statement says at that point. A hand-entered transaction has
								no statement line behind it, so it shows an em dash exactly as
								the statement line column beside it does.
							-->
							<td
								class="px-3 py-2 text-right tabular-nums"
								title={line?.Balance != null
									? "Balance printed on the bank's statement"
									: 'No statement line behind this transaction'}
							>
								{#if line?.Balance != null}
									{formatCurrency(Number(line.Balance), t.CurrencyCode ?? currency)}
								{:else}
									<span class="muted">—</span>
								{/if}
							</td>
							<!--
								Xero's Analysis Code column. No tracking category is recorded on a
								bank transaction yet, so the column says so rather than being
								absent — the same placeholder the statements tab carries.
							-->
							<td
								class="px-3 py-2 muted"
								title="Analysis codes are not recorded on transactions yet"
							>
								—
							</td>
							<td class="px-3 py-2 muted" title="How the transaction got into Xero">
								{line ? 'Imported' : 'Manual'}
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

{#if details}
	<!--
		Xero's "Statement Details": the line as the bank sent it, with none of
		our interpretation on top. It is opened from the reconcile list, the
		statement list and the transactions list alike, so it lives once at the
		bottom of the page rather than inside any one tab.
	-->
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-ink-900/40 flex items-center justify-center z-50 p-4"
		role="presentation"
		onclick={() => (details = null)}
	>
		<div
			class="bg-white rounded-xl shadow-pop w-full max-w-md max-h-[90vh] flex flex-col"
			role="dialog"
			tabindex="-1"
			aria-modal="true"
			aria-labelledby="statement-details-title"
			onclick={(e) => e.stopPropagation()}
		>
			<div class="p-5 border-b border-ink-100 flex items-center justify-between">
				<h3 id="statement-details-title" class="font-semibold">Statement Details</h3>
				<button
					class="btn-ghost"
					aria-label="Close statement details"
					onclick={() => (details = null)}
				>
					Close
				</button>
			</div>
			<div class="p-5 overflow-y-auto flex-1">
				<dl class="text-sm divide-y divide-ink-100">
					{#each detailsRows(details) as row (row.label)}
						<div class="flex justify-between gap-6 py-2">
							<dt class="muted">{row.label}</dt>
							<dd class="text-right">{row.value}</dd>
						</div>
					{/each}
					{#if details.CodedAccountCode}
						<div class="flex justify-between gap-6 py-2">
							<dt class="muted">Coded to</dt>
							<dd class="text-right">
								{codingLabel(details.CodedAccountCode, details.CodedAccountName ?? '')}
							</dd>
						</div>
					{/if}
					{#if details.UpstreamChange}
						<div class="flex justify-between gap-6 py-2">
							<dt class="muted">Bank's version</dt>
							<dd class="text-right text-amber-800">{upstreamDetail(details)}</dd>
						</div>
					{/if}
				</dl>
			</div>
			<div class="px-5 py-3 border-t border-ink-100 text-xs muted">
				Esc to close
			</div>
		</div>
	</div>
{/if}

<svelte:window
	onclick={onDocumentClick}
	onkeydown={(e) => {
		// The details panel is the innermost thing on screen, so Escape closes it
		// first and only then reaches the panels and menus underneath.
		if (e.key === 'Escape' && details) {
			details = null;
			return;
		}
		onKeydown(e);
	}}
/>
