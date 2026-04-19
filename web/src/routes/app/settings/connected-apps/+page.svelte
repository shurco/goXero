<script lang="ts">
	import SettingsHeader from '$lib/components/SettingsHeader.svelte';

	let search = $state('');

	interface ConnectedApp {
		id: string;
		name: string;
		connectedAt: string;
	}

	const apps = $state<ConnectedApp[]>([]);
</script>

<SettingsHeader title="Connected apps">
	<span class="muted text-sm">Marketplace directory is not connected yet.</span>
</SettingsHeader>

<div class="card">
	<div class="px-6 py-4 border-b border-ink-100">
		<h2 class="text-sm font-semibold text-ink-900">Connected apps</h2>
	</div>
	<div class="px-6 py-10 text-center">
		{#if apps.length === 0}
			<p class="muted text-sm">No connected apps.</p>
		{:else}
			<ul class="space-y-3 text-left">
				{#each apps as app (app.id)}
					<li class="flex items-center justify-between gap-4 border-b border-ink-100 pb-3">
						<div>
							<div class="font-medium text-ink-900">{app.name}</div>
							<div class="muted text-xs">Connected {app.connectedAt}</div>
						</div>
						<button class="btn-secondary" type="button">Disconnect</button>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</div>

<div class="mt-10 text-center">
	<h3 class="text-base font-semibold text-ink-900">
		Discover and connect more apps for your business
	</h3>
	<div class="relative mx-auto mt-4 max-w-2xl">
		<svg
			class="absolute left-3 top-1/2 -translate-y-1/2 text-ink-400"
			width="16"
			height="16"
			viewBox="0 0 20 20"
			fill="currentColor"
			aria-hidden="true"
		>
			<path
				fill-rule="evenodd"
				d="M9 3a6 6 0 104.47 10.03l3.25 3.25a1 1 0 001.41-1.41l-3.25-3.25A6 6 0 009 3zm-4 6a4 4 0 118 0 4 4 0 01-8 0z"
				clip-rule="evenodd"
			/>
		</svg>
		<input
			class="input pl-9"
			placeholder="Search apps, industries, tasks and more…"
			bind:value={search}
			disabled
		/>
	</div>
	<p class="muted text-xs mt-3">
		App marketplace integration is coming soon — connect Slack, Stripe, HubSpot and more.
	</p>
</div>
