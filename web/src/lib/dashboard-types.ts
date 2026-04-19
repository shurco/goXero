import type { Account } from './types';

/** Bank tile row on the home dashboard (shared with dashboard-layout). */
export interface BankTile {
	account: Account;
	statementBalance: number;
	xeroBalance: number;
	lastStatementDate?: string;
	unreconciled: number;
}
