<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Sales Tax Report"
	endpoint="/api/v1/reports/bas"
	fields={[
		{ name: 'fromDate', label: 'From', type: 'date' },
		{ name: 'toDate', label: 'To', type: 'date' }
	]}
/>
