<script lang="ts">
	import ReportView from '$lib/components/ReportView.svelte';
</script>
<ReportView
	title="Account Transactions"
	endpoint="/api/v1/reports/account-transactions"
	fields={[
		{ name: 'fromDate', label: 'From', type: 'date' },
		{ name: 'toDate', label: 'To', type: 'date' }
	]}
/>
