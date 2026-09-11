<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	// Cash flow is served by /reports/cash-summary; this route predates that
	// page and is kept so old links land on the real report.
	onMount(() => {
		goto('/app/reports/cash-summary', { replaceState: true });
	});
</script>
