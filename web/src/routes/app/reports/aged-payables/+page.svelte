<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView title="Aged Payables" endpoint="/api/v1/reports/aged-payables" fields={[{ name: 'date', label: 'As of', type: 'date' }]} />
