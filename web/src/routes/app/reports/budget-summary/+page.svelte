<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView title="Budget Summary" endpoint="/api/v1/reports/budget-summary" fields={[{ name: 'date', label: 'As of', type: 'date' }]} />
