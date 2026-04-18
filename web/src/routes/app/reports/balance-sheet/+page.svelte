<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Balance Sheet"
	endpoint="/api/v1/reports/balance-sheet"
	fields={[{ name: 'date', label: 'As of', type: 'date' }]}
/>
