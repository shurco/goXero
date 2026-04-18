<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	onMount(() => { goto('/app/contacts?new=1', { replaceState: true }); });
</script>
