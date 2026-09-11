<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Aged Payables by Contact"
	endpoint="/api/v1/reports/aged-payables-by-contact"
	fields={[{ name: 'date', label: 'As of', type: 'date' }]}
/>
