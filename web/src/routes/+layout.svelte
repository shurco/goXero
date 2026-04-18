<script lang="ts">
	import '../app.css';
	import { page } from '$app/stores';
	import { session } from '$lib/stores/session';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	let { children } = $props();

	const publicRoutes = ['/login', '/register', '/'];

	onMount(() => {
		session.subscribe((s) => {
			const currentPath = $page.url.pathname;
			const isPublic = publicRoutes.includes(currentPath);
			if (!s.token && !isPublic) {
				goto('/login');
			}
		});
	});
</script>

{@render children()}
