/** Small curated catalogue for the "Add bank account" picker screen. */
export interface BankBrand {
	id: string;
	name: string;
	initials: string;
	color: string; // hex / css color for the tile badge
	country: string; // ISO alpha-2
	popular?: boolean;
}

export const BANK_BRANDS: BankBrand[] = [
	{ id: 'amex-ca', name: 'American Express Cards (Canada)', initials: 'AM/EX', color: '#0057B7', country: 'CA', popular: true },
	{ id: 'bmo-ca', name: 'BMO — Online Banking (CA)', initials: 'BMO', color: '#F7F2EA', country: 'CA', popular: true },
	{ id: 'bmo-comm', name: 'BMO Commercial Bank', initials: 'BMO', color: '#FFFFFF', country: 'CA', popular: true },
	{ id: 'cibc', name: 'CIBC (CA)', initials: 'CIBC', color: '#F3F4F6', country: 'CA', popular: true },
	{ id: 'rbc-express', name: 'RBC Express Online Banking (Canada)', initials: 'RBC', color: '#F3F4F6', country: 'CA', popular: true },
	{ id: 'rbc', name: 'RBC Royal Bank (CA)', initials: 'RBC', color: '#F3F4F6', country: 'CA', popular: true },
	{ id: 'scotia', name: 'Scotiabank (Personal & Small Business)', initials: 'SB', color: '#F3F4F6', country: 'CA', popular: true },

	{ id: 'chase', name: 'Chase Bank', initials: 'CH', color: '#117ACA', country: 'US', popular: true },
	{ id: 'boa', name: 'Bank of America', initials: 'BOA', color: '#B11F33', country: 'US', popular: true },
	{ id: 'wf', name: 'Wells Fargo', initials: 'WF', color: '#D71E28', country: 'US', popular: true },
	{ id: 'citi', name: 'Citi', initials: 'CITI', color: '#056DAE', country: 'US', popular: true },
	{ id: 'usbank', name: 'U.S. Bank', initials: 'USB', color: '#0C2074', country: 'US', popular: true },
	{ id: 'capone', name: 'Capital One', initials: 'C1', color: '#D22E1E', country: 'US', popular: true },
	{ id: 'amex-us', name: 'American Express Cards (US)', initials: 'AM/EX', color: '#0057B7', country: 'US', popular: true },

	{ id: 'barclays', name: 'Barclays', initials: 'BRC', color: '#00AEEF', country: 'GB', popular: true },
	{ id: 'hsbc', name: 'HSBC UK', initials: 'HSBC', color: '#DB0011', country: 'GB', popular: true },
	{ id: 'lloyds', name: 'Lloyds Bank', initials: 'LB', color: '#024731', country: 'GB', popular: true },
	{ id: 'natwest', name: 'NatWest', initials: 'NW', color: '#5A287D', country: 'GB', popular: true },
	{ id: 'starling', name: 'Starling Bank', initials: 'SB', color: '#7A3FEA', country: 'GB', popular: true },
	{ id: 'monzo', name: 'Monzo', initials: 'MZ', color: '#FF4E44', country: 'GB', popular: true },

	{ id: 'pkobp', name: 'PKO Bank Polski', initials: 'PKO', color: '#0A4DA2', country: 'PL' },
	{ id: 'mbank', name: 'mBank', initials: 'MB', color: '#000000', country: 'PL' },
	{ id: 'santander-pl', name: 'Santander Bank Polska', initials: 'SNT', color: '#EC0000', country: 'PL' },
	{ id: 'ing-pl', name: 'ING Bank Śląski', initials: 'ING', color: '#FF6200', country: 'PL' }
];

export const BANK_COUNTRIES: { code: string; name: string }[] = [
	{ code: 'CA', name: 'Canada' },
	{ code: 'US', name: 'United States' },
	{ code: 'GB', name: 'United Kingdom' },
	{ code: 'PL', name: 'Poland' }
];

/** Common currencies for bank account creation. */
export const BANK_CURRENCIES = [
	'USD',
	'CAD',
	'EUR',
	'GBP',
	'PLN',
	'AUD',
	'NZD',
	'CHF',
	'JPY',
	'SGD'
];
