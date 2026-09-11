<script lang="ts">
	import { browser } from '$app/environment';
	import ReportView from '$lib/components/ReportView.svelte';

	// The workpaper is a document, not a screen: the browser's own print dialog
	// is how it gets to PDF, so the button just opens that.
	function printPage() {
		if (browser) window.print();
	}
</script>

<div class="form-1120-page space-y-3">
	<div class="flex flex-wrap items-start justify-between gap-2">
		<p class="muted max-w-3xl text-sm">
			IRS Form 1120, Page 1, in the form's own order, followed by Schedule L and
			Schedule M-1. The last section lists the accounts that carry a figure but reach
			no line on the form — while it has rows, this workpaper is not ready to be
			transferred to the return.
		</p>
		<button type="button" class="btn-ghost text-sm print:hidden" onclick={printPage}>
			Print
		</button>
	</div>

	<ReportView
		title="Form 1120 Workpaper"
		endpoint="/api/v1/reports/form-1120"
		fields={[
			{ name: 'fromDate', label: 'From', type: 'date' },
			{ name: 'toDate', label: 'To', type: 'date' }
		]}
	/>
</div>

<style>
	/*
	 * On paper the report is the whole page: the date controls, the Run and
	 * Print buttons and the card's chrome are screen furniture, and the 44rem
	 * minimum width that keeps the columns aligned on a narrow screen would
	 * clip the right-hand amount column on A4.
	 */
	@media print {
		.form-1120-page :global(button),
		.form-1120-page :global(input),
		.form-1120-page :global(select),
		.form-1120-page :global(span.label) {
			display: none;
		}
		.form-1120-page :global(div.card) {
			border: 0;
			box-shadow: none;
			padding: 0;
		}
		.form-1120-page :global(.overflow-x-auto) {
			overflow: visible;
		}
		.form-1120-page :global(.overflow-x-auto > div) {
			min-width: 0;
		}
	}
</style>
