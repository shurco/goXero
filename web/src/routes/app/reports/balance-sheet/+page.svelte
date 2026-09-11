<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<!--
	The comparative column is the beginning-of-year balance, which is the pair of
	figures IRS Schedule L asks for (beginning and end of the tax year). It is on
	by default because that is what the report is for here; `compareDate` pins it
	to a specific date when the financial year does not start where you expect.
-->
<ReportView
	title="Balance Sheet"
	endpoint="/api/v1/reports/balance-sheet"
	defaults={{ compare: 'true' }}
	fields={[
		{ name: 'date', label: 'As of', type: 'date' },
		{ name: 'compare', label: 'Compare with beginning of year', type: 'checkbox' },
		{ name: 'compareDate', label: 'Compare date', type: 'date' }
	]}
/>
