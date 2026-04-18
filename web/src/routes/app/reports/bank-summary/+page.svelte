<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Bank Summary"
	endpoint="/api/v1/reports/bank-summary"
	fields={[
		{ name: 'fromDate', label: 'From', type: 'date' },
		{ name: 'toDate', label: 'To', type: 'date' }
	]}
/>
