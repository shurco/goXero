<script lang="ts">
	import { session } from '$lib/stores/session';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	onMount(() => {
		const unsub = session.subscribe((s) => {
			goto(s.token ? '/app' : '/login');
		});
		return unsub;
	});
</script>

<div class="flex min-h-screen items-center justify-center">
	<div class="text-center">
		<div class="mx-auto h-12 w-12 animate-spin rounded-full border-4 border-brand-200 border-t-brand-500"></div>
		<p class="mt-4 muted">Loading goXero…</p>
	</div>
</div>
