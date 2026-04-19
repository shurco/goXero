<script lang="ts">
	interface Props {
		/** Page title shown as the H1 (e.g. "Financial settings"). */
		title: string;
		/** Short description shown below the title. */
		description?: string;
		/** Breadcrumb root label — default "Settings". */
		rootLabel?: string;
		/** Root href for the crumb link. */
		rootHref?: string;
		children?: import('svelte').Snippet;
	}
	let {
		title,
		description,
		rootLabel = 'Settings',
		rootHref = '/app/settings',
		children
	}: Props = $props();
</script>

<div class="flex items-start justify-between flex-wrap gap-4 mb-6">
	<div>
		<div class="text-sm">
			<a href={rootHref} class="text-brand-600 hover:underline">{rootLabel}</a>
			<span class="muted">›</span>
		</div>
		<h1 class="section-title mt-1">{title}</h1>
		{#if description}
			<p class="muted mt-1 text-sm max-w-2xl">{description}</p>
		{/if}
	</div>
	{#if children}
		<div class="flex items-center gap-2">
			{@render children()}
		</div>
	{/if}
</div>
