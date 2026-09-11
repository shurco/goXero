<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<!-- This page used to render the Trial Balance endpoint under a General Ledger
     title; it now asks for the general ledger itself. -->
<ReportView
	title="General Ledger"
	endpoint="/api/v1/reports/general-ledger"
	fields={[
		{ name: 'fromDate', label: 'From', type: 'date' },
		{ name: 'toDate', label: 'To', type: 'date' }
	]}
/>
