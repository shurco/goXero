<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Trial Balance"
	endpoint="/api/v1/reports/trial-balance"
	fields={[
		{ name: 'date', label: 'As of', type: 'date' },
		{ name: 'fromDate', label: 'From', type: 'date' }
	]}
/>
