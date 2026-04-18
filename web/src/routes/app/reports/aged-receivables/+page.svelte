<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView title="Aged Receivables" endpoint="/api/v1/reports/aged-receivables" fields={[{ name: 'date', label: 'As of', type: 'date' }]} />
