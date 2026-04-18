<script lang="ts">
	interface Props {
		title: string;
		subtitle?: string;
		primary?: { label: string; href: string } | undefined;
		secondary?: { label: string; href: string } | undefined;
	}
	let { title, subtitle, primary, secondary }: Props = $props();
</script>

<div class="flex items-start justify-between flex-wrap gap-4 mb-6">
	<div>
		<h1 class="section-title">{title}</h1>
		{#if subtitle}
			<p class="muted mt-1 text-sm">{subtitle}</p>
		{/if}
	</div>
	<div class="flex gap-2">
		{#if secondary}
			<a href={secondary.href} class="btn-secondary">{secondary.label}</a>
		{/if}
		{#if primary}
			<a href={primary.href} class="btn-primary">{primary.label}</a>
		{/if}
	</div>
</div>
