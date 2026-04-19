import { browser } from '$app/environment';
import { defaultFavouriteKeys, flattenReportCatalog, type FlatReportRow } from './reports-catalog';

export const REPORT_FAVOURITES_STORAGE_KEY = 'goxero.reports.favouriteKeys.v1';

const byKey = new Map(flattenReportCatalog().map((r) => [r.key, r]));

export function getReportRowByKey(key: string): FlatReportRow | undefined {
	return byKey.get(key);
}

function readKeysFromStorage(): string[] {
	if (!browser) return defaultFavouriteKeys();
	try {
		const raw = localStorage.getItem(REPORT_FAVOURITES_STORAGE_KEY);
		if (raw) {
			const parsed = JSON.parse(raw) as unknown;
			if (Array.isArray(parsed) && parsed.every((x) => typeof x === 'string')) {
				return parsed as string[];
			}
		}
	} catch {
		/* ignore */
	}
	return defaultFavouriteKeys();
}

/** Shared favourite report row keys — same source as nav dropdown and /app/reports. */
export const reportFavourites = $state<{ keys: string[] }>({
	keys: readKeysFromStorage()
});

export function setFavouriteReportKeys(keys: string[]) {
	reportFavourites.keys = keys;
	if (!browser) return;
	try {
		localStorage.setItem(REPORT_FAVOURITES_STORAGE_KEY, JSON.stringify(keys));
	} catch {
		/* ignore */
	}
}

export function toggleFavouriteReportKey(key: string) {
	const cur = reportFavourites.keys;
	const next = cur.includes(key) ? cur.filter((k) => k !== key) : [...cur, key];
	setFavouriteReportKeys(next);
}

export function isFavouriteReportKey(key: string): boolean {
	return reportFavourites.keys.includes(key);
}
