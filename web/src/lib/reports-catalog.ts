/** Shared definitions for the Reports hub and nav — links map to existing routes where available. */
export const FAVOURITE_REPORTS: { label: string; href: string }[] = [
	{ label: 'Account Transactions', href: '/app/reports/account-transactions' },
	{ label: 'Accounts Payable Aging Summary', href: '/app/reports/aged-payables' },
	{ label: 'Accounts Receivable Aging Summary', href: '/app/reports/aged-receivables' },
	{ label: 'Balance Sheet', href: '/app/reports/balance-sheet' },
	{ label: 'Income Statement (Profit and Loss)', href: '/app/reports/profit-and-loss' },
	{ label: 'Sales Tax Report', href: '/app/reports/sales-tax' }
];

export interface ReportEntry {
	label: string;
	/** `null` = listed for UI parity; not wired yet */
	href: string | null;
	favourite?: boolean;
	/** Shown when "Show descriptions" is on (Xero-style copy). */
	description: string;
}

export interface ReportCategory {
	id: string;
	title: string;
	/** Grey subtitle under the category title (Xero home reports). */
	subtitle: string;
	reports: ReportEntry[];
}

export const REPORT_CATEGORIES: ReportCategory[] = [
	{
		id: 'financial-performance',
		title: 'Financial performance',
		subtitle: "Summarise your business's financial position at a glance.",
		reports: [
			{
				label: 'Budget Manager',
				href: '/app/reports/budget-summary',
				description:
					'Create budgets to monitor business performance against your goals.'
			},
			{
				label: 'Budget Summary',
				href: '/app/reports/budget-summary',
				description: "View the budgets you've created in Budget Manager."
			},
			{
				label: 'Budget Variance',
				href: null,
				description:
					'Compare your actual figures with budgeted figures to track business performance.'
			},
			{
				label: 'Business Cash Flow Summary',
				href: '/app/reports/cash-flow',
				description:
					'See how your business has received and used cash within a certain timeframe.'
			},
			{
				label: 'Cash Summary',
				href: '/app/reports/cash-summary',
				description: 'See how your business has received and used cash.'
			},
			{
				label: 'Executive Summary',
				href: '/app/reports/executive-summary',
				description:
					"Get an overview of key cash, profitability, balance sheet, income, performance, and position items."
			},
			{
				label: "Owner's Equity Summary",
				href: null,
				description:
					"View and compare changes to your organization's net worth."
			},
			{
				label: 'Tracking Summary',
				href: null,
				description:
					'See a summary of your business activities broken down by tracking options.'
			}
		]
	},
	{
		id: 'financial-statements',
		title: 'Financial statements',
		subtitle:
			"Examine your business's performance with reports that cover costs, liabilities, revenue, assets, and equity.",
		reports: [
			{
				label: 'Balance Sheet',
				href: '/app/reports/balance-sheet',
				favourite: true,
				description:
					"See your organization's financial position, and what you own and owe at a particular time."
			},
			{
				label: 'Blank Report',
				href: null,
				description: 'Create your own report with this empty template.'
			},
			{
				label: 'Depreciation Schedule',
				href: null,
				description:
					'See a list of your fixed assets, additions & disposals, and depreciation values.'
			},
			{
				label: 'Disposal Schedule',
				href: null,
				description: 'See details of fixed assets that were sold or disposed of.'
			},
			{
				label: 'Fixed Asset Reconciliation',
				href: null,
				description:
					'Compare fixed asset balances in your Balance Sheet and Fixed Assets register and check for differences.'
			},
			{
				label: 'Income Statement (Profit and Loss)',
				href: '/app/reports/profit-and-loss',
				favourite: true,
				description:
					"See a snapshot of your organization's income, expenses, and profit. Some non-operating expenses may be included in the 'Operating Expenses' section. To make changes, select 'Edit layout' in the report."
			},
			{
				label: 'Management Report',
				href: null,
				description:
					'A reporting package that includes 6 management style reports.'
			}
		]
	},
	{
		id: 'payables-receivables',
		title: 'Payables and receivables',
		subtitle:
			"Get reports of the transactions you've had between your business and your customers, as well as supplier relations.",
		reports: [
			{
				label: 'Accounts Payable Aging Detail',
				href: '/app/reports/aged-payables',
				description:
					'See individual bills, credit notes, and overpayments you owe, based on the age of the transactions.'
			},
			{
				label: 'Accounts Payable Aging Summary',
				href: '/app/reports/aged-payables',
				favourite: true,
				description:
					'See a summary of the amount you owe to each contact, based on the age of the transactions.'
			},
			{
				label: 'Accounts Receivable Aging Detail',
				href: '/app/reports/aged-receivables',
				description:
					'See individual invoices, credit notes, and overpayments owed to you, based on the age of the transactions.'
			},
			{
				label: 'Accounts Receivable Aging Summary',
				href: '/app/reports/aged-receivables',
				favourite: true,
				description:
					'See a summary of the amount each contact owes you, based on the age of the transactions.'
			},
			{
				label: 'Billable Expenses - Outstanding',
				href: null,
				description:
					"See billable items that haven't been allocated to customers yet."
			},
			{
				label: 'Contact Transactions - Summary',
				href: null,
				description:
					'See a summary of your receivable, payable, and cash transactions for a contact.'
			},
			{
				label: 'Expense Claim Detail',
				href: null,
				description:
					'See outstanding expense claims based on the age of the transaction.'
			},
			{
				label: 'Income and Expenses by Contact',
				href: null,
				description:
					'See a summary of your contacts based on incoming and outgoing transactions.'
			},
			{
				label: 'Payable Invoice Detail',
				href: null,
				description:
					'See line-by-line details of bills, credit notes, and overpayments from your suppliers, with the option to add prepayments.'
			},
			{
				label: 'Payable Invoice Summary',
				href: null,
				description:
					'See a list of bills, credit notes, and overpayments from your suppliers, with the option to add prepayments.'
			},
			{
				label: 'Receivable Invoice Detail',
				href: null,
				description:
					'See line-by-line details of sales invoices, credit notes, and overpayments for your customers, with the option to add prepayments.'
			},
			{
				label: 'Receivable Invoice Summary',
				href: null,
				description:
					'See a list of sales invoices, credit notes, and overpayments for your customers, with the option to add prepayments.'
			}
		]
	},
	{
		id: 'payroll',
		title: 'Payroll',
		subtitle:
			'Get valuable information on payroll activity, transactions, leave balances, timesheets, superannuation, and more.',
		reports: [
			{
				label: 'Pay Run by Employee',
				href: null,
				description: 'See amounts paid to either individual or all employees.'
			},
			{
				label: 'Pay Run by Pay Item',
				href: null,
				description:
					'See amounts paid by pay type and item, including the accounts these amounts are recorded in.'
			},
			{
				label: 'Pay Run by Pay Type',
				href: null,
				description:
					'See the amounts paid or the individual pay runs by pay type.'
			},
			{
				label: 'Pay Run Summary',
				href: null,
				description: 'Review each pay type processed per pay run.'
			}
		]
	},
	{
		id: 'projects',
		title: 'Projects',
		subtitle:
			"Get details on your project's financial activity, chargeable time, and how much of the year was spent on a project.",
		reports: [
			{
				label: 'Detailed Time',
				href: null,
				description: 'A detailed report of staff time entries across all projects.'
			},
			{
				label: 'Project Details',
				href: null,
				description:
					'Get an in-depth view of your projects with a comprehensive look at all estimates, expenses and invoiced amounts to help you manage all aspects of your work.'
			},
			{
				label: 'Project Financials',
				href: null,
				description:
					'Get insight into how your tasks and expenses are tracking against budget, helping highlight potential issues or overruns, enabling you to adjust work.'
			},
			{
				label: 'Project Summary',
				href: null,
				description:
					'Get a summary of how your project costs, estimates and profitability are tracking with this high-level view.'
			}
		]
	},
	{
		id: 'reconciliations',
		title: 'Reconciliation',
		subtitle:
			'Compare bank and ledger bank balances, and verify records of transactions and records to help with reconciling.',
		reports: [
			{
				label: 'Account Summary',
				href: '/app/reports/bank-summary',
				description: 'See a monthly summary for a specific account.'
			},
			{
				label: 'Bank Reconciliation',
				href: null,
				description:
					'Compare your balance in Xero with your bank balance, and check for missing, deleted or duplicated transactions.'
			},
			{
				label: 'Bank Reconciliation Detail',
				href: null,
				description:
					'Compare your balance in Xero with your bank balance or bank statement, and check for missing, deleted or duplicated transactions.'
			},
			{
				label: 'Bank Summary',
				href: '/app/reports/bank-summary',
				description:
					'See your opening and closing bank balances, and the activity during a selected period.'
			},
			{
				label: 'Cash Validation Customer Report',
				href: null,
				description:
					"Shows how your business's accounting data relates to your linked bank statement lines."
			},
			{
				label: 'Inventory Item List',
				href: null,
				description:
					'See a list of all your tracked and untracked inventory items, including the item details.'
			},
			{
				label: 'Reconciliation Reports',
				href: null,
				description:
					'A reporting package that includes multiple reports that help reconcile your accounts at the end of a period.'
			},
			{
				label: 'Uncoded Statement Lines',
				href: null,
				description:
					'A list of unreconciled statement lines that can be shared with clients, who can add comments and help with reconciliation.'
			}
		]
	},
	{
		id: 'taxes-balances',
		title: 'Taxes and Balances',
		subtitle: 'Check sales tax and trial reports related to your general ledger.',
		reports: [
			{
				label: '1099 Report',
				href: null,
				description:
					'Prepare your NEC and MISC forms by setting up rules to generate your 1099 report ready to file with the IRS.'
			},
			{
				label: 'Custom Sales Tax Report',
				href: null,
				description: 'Review sales tax details.'
			},
			{
				label: 'Foreign Currency Gains and Losses',
				href: null,
				description:
					'See revalued balances for foreign currency accounts and a summary of your currency-related gains or losses.'
			},
			{
				label: 'General Ledger Detail',
				href: '/app/reports/general-ledger-detail',
				description:
					'See a detailed view of the activity and balances for all your accounts.'
			},
			{
				label: 'General Ledger Exceptions',
				href: null,
				description:
					'Review transactions identified as out of the ordinary compared to others in the same account.'
			},
			{
				label: 'General Ledger Summary',
				href: '/app/reports/general-ledger',
				description:
					'See a summary of the activity and balances for all your accounts.'
			},
			{
				label: 'Journal Report',
				href: '/app/reports/journal-report',
				description:
					'See all journal entries made in your general ledger (chart of accounts).'
			},
			{
				label: 'Sales Tax Report',
				href: '/app/reports/sales-tax',
				favourite: true,
				description: 'Review sales tax details.'
			},
			{
				label: 'Tax Reconciliation',
				href: null,
				description: 'Reconcile the amount of tax recorded with the tax filed.'
			},
			{
				label: 'Trial Balance',
				href: null,
				description: 'View account balances on a year-to-date basis.'
			},
			{
				label: 'Trial Balance by Date Range Beta',
				href: null,
				description: 'View account balances based on a selected date range.'
			}
		]
	},
	{
		id: 'transactions',
		title: 'Transactions',
		subtitle: 'View transaction details and search for duplicate transactions.',
		reports: [
			{
				label: 'Account Transactions',
				href: '/app/reports/account-transactions',
				favourite: true,
				description: 'See a detailed view of transactions in selected accounts.'
			},
			{
				label: 'Duplicate Statement Lines',
				href: null,
				description:
					'Check your Xero bank account to see if any statement lines are duplicated.'
			},
			{
				label: 'Inventory Item Details',
				href: null,
				description:
					'See line-by-line sales and purchase details of your inventory items.'
			},
			{
				label: 'Inventory Item Summary',
				href: null,
				description:
					'See a summary of your purchases and sales for each inventory item.'
			},
			{
				label: 'Sales By Item',
				href: null,
				description:
					'See a summary of sales transactions for your untracked inventory items.'
			}
		]
	}
];

/** Stable id for a report row (category + label; unique in catalog). */
export function reportRowKey(categoryId: string, label: string): string {
	return `${categoryId}::${label}`;
}

export interface FlatReportRow {
	key: string;
	categoryId: string;
	label: string;
	href: string | null;
}

/** All report rows from categories — for favourites lookup and keys. */
export function flattenReportCatalog(): FlatReportRow[] {
	const out: FlatReportRow[] = [];
	for (const cat of REPORT_CATEGORIES) {
		for (const r of cat.reports) {
			out.push({
				key: reportRowKey(cat.id, r.label),
				categoryId: cat.id,
				label: r.label,
				href: r.href
			});
		}
	}
	return out;
}

/** Default favourite row keys matching {@link FAVOURITE_REPORTS} by label + href. */
export function defaultFavouriteKeys(): string[] {
	const flat = flattenReportCatalog();
	const keys: string[] = [];
	for (const f of FAVOURITE_REPORTS) {
		const hit = flat.find((r) => r.label === f.label && r.href === f.href);
		if (hit) keys.push(hit.key);
	}
	return keys;
}
