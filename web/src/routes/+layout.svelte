<script lang="ts">
	import '../app.css';
	import { page } from '$app/stores';
	import { session } from '$lib/stores/session';
	import { goto } from '$app/navigation';

	let { children } = $props();

	const publicRoutes = ['/login', '/register', '/'];

	/** Don’t send users to login while the error page (404/500) is shown. */
	$effect(() => {
		if ($page.error) return;
		const path = $page.url.pathname;
		if (publicRoutes.includes(path) || $session.token) return;
		void goto('/login');
	});
</script>

{@render children()}
