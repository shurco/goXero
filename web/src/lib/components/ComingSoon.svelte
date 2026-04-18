<script lang="ts">
	interface Props {
		title: string;
		description?: string;
		backHref?: string;
	}
	let { title, description, backHref }: Props = $props();
</script>

<div class="space-y-4">
	<div class="rounded-md border border-amber-300 bg-amber-50 text-amber-900 px-4 py-3 flex items-center gap-3">
		<svg viewBox="0 0 24 24" class="h-5 w-5 fill-current shrink-0">
			<path d="M12 2 1 21h22L12 2zm0 5 7.5 12.9H4.5L12 7zm-1 4v4h2v-4h-2zm0 6v2h2v-2h-2z" />
		</svg>
		<div class="text-sm">
			<span class="font-semibold uppercase tracking-wider text-xs mr-2">Under development</span>
			This section is not yet wired to the API.
		</div>
	</div>

	<div class="max-w-xl mx-auto mt-4 card p-10 text-center">
		<div class="mx-auto mb-5 h-14 w-14 rounded-full bg-brand-100 text-brand-700 flex items-center justify-center">
			<svg viewBox="0 0 24 24" class="h-7 w-7 fill-current"><path d="M12 8v5l4 2 .7-1.2-3.2-1.9V8H12zm0-6a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm0 18a8 8 0 1 1 0-16 8 8 0 0 1 0 16z" /></svg>
		</div>
		<h1 class="text-xl font-semibold text-ink-900">{title}</h1>
		{#if description}
			<p class="muted mt-2">{description}</p>
		{/if}
		{#if backHref}
			<a href={backHref} class="btn-secondary mt-6 inline-flex">Go back</a>
		{/if}
	</div>
</div>
