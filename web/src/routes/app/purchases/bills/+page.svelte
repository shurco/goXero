<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { onMount } from 'svelte';

	onMount(() => {
		const params = new URLSearchParams($page.url.searchParams);
		params.set('type', 'ACCPAY');
		goto('/app/invoices?' + params.toString(), { replaceState: true });
	});
</script>
