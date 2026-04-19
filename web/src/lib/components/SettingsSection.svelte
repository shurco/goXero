<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		hint?: string;
		children: Snippet;
	}

	let { title, description = '', hint = '', children }: Props = $props();
</script>

<div class="form-section">
	<div class="form-section-title">
		{title}
		{#if hint}
			<span
				class="ml-1 inline-flex h-4 w-4 items-center justify-center rounded-full bg-ink-200 text-ink-700 text-[10px]"
				title={hint}
			>
				?
			</span>
		{/if}
	</div>
	{#if description}
		<p class="text-sm muted mb-3">{description}</p>
	{/if}
	{@render children()}
</div>
