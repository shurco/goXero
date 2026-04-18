<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Executive Summary"
	endpoint="/api/v1/reports/executive-summary"
	fields={[{ name: 'date', label: 'As of', type: 'date' }]}
/>
