<script lang="ts">
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
</script>

<ModuleHeader title="Tax" subtitle="Sales tax and 1099 reporting." />

<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
	<a href="/app/tax/sales-tax" class="card p-6 block hover:shadow-pop transition">
		<h2 class="content-section-title">Sales tax</h2>
		<p class="muted text-sm mt-1">Track GST, VAT and sales-tax collected and paid.</p>
	</a>
	<a href="/app/tax/1099" class="card p-6 block hover:shadow-pop transition">
		<h2 class="content-section-title">1099 report</h2>
		<p class="muted text-sm mt-1">US 1099-NEC/1099-MISC summary by contact.</p>
	</a>
</div>
