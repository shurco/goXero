<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Income Statement (Profit and Loss)"
	endpoint="/api/v1/reports/profit-and-loss"
	fields={[
		{ name: 'fromDate', label: 'From', type: 'date' },
		{ name: 'toDate', label: 'To', type: 'date' }
	]}
/>
