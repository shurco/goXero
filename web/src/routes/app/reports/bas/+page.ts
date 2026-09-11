import { redirect } from '@sveltejs/kit';

// GET /api/v1/reports/bas serves this report, and the API index
// (internal/handlers/report.go's ReportsList) advertises it at the
// collection-relative path /reports/bas -- so this address is reachable by
// anyone who follows the index's Path field literally. The report has exactly
// one page: the catalogue calls it "Sales Tax Report" and links
// /app/reports/sales-tax, which fetches this same endpoint. Redirect there
// rather than rendering one report twice under two names.
export function load() {
	redirect(308, '/app/reports/sales-tax');
}
