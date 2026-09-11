<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Aged Receivables by Contact"
	endpoint="/api/v1/reports/aged-receivables-by-contact"
	fields={[{ name: 'date', label: 'As of', type: 'date' }]}
/>
