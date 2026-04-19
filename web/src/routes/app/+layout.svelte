<script lang="ts">
	import { page } from '$app/stores';
	import { session } from '$lib/stores/session';
	import { goto } from '$app/navigation';
	import TopNav from '$lib/components/TopNav.svelte';

	let { children } = $props();

	$effect(() => {
		if ($page.error) return;
		if (!$session.token) {
			void goto('/login');
		}
	});
</script>

<div class="min-h-screen flex flex-col bg-ink-50">
	<TopNav />
	<main class="flex-1">
		<div class="mx-auto w-full max-w-[1360px] px-4 lg:px-6 py-6 lg:py-8">
			{@render children()}
		</div>
	</main>
</div>
