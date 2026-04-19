<script lang="ts">
	import { page } from '$app/stores';
	import { dev } from '$app/environment';

	const status = $derived($page.status);
	const err = $derived($page.error);
	const is404 = $derived(status === 404);
	const title = $derived(is404 ? 'Page not found' : 'Something went wrong');
	const detail = $derived(
		is404
			? 'This address doesn’t exist or was moved.'
			: (typeof err === 'object' && err && 'message' in err && typeof (err as { message: string }).message === 'string'
					? (err as { message: string }).message
					: 'An unexpected error occurred. Please try again later.')
	);
</script>

<svelte:head>
	<title>{status} · {title} · goXero</title>
</svelte:head>

<div class="flex min-h-[50vh] w-full flex-col items-center justify-center px-4 py-16">
	<div class="card max-w-md w-full p-8 text-center">
		<p class="text-sm font-semibold uppercase tracking-wide text-ink-500">{status}</p>
		<h1 class="mt-2 text-2xl font-semibold text-ink-900">{title}</h1>
		<p class="mt-3 text-sm text-ink-600 leading-relaxed">{detail}</p>
		{#if dev && err}
			<pre class="mt-4 max-h-48 overflow-auto rounded-md bg-ink-100 p-3 text-left text-xs text-ink-800 whitespace-pre-wrap">{err instanceof Error ? err.stack ?? err.message : JSON.stringify(err, null, 2)}</pre>
		{/if}
		<div class="mt-8 flex flex-col gap-2 sm:flex-row sm:justify-center">
			<a href="/app" class="btn-primary">Go to dashboard</a>
			<a href="/" class="btn-secondary">Home</a>
		</div>
	</div>
</div>
