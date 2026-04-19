<script lang="ts">
	interface Props {
		saving?: boolean;
		saveLabel?: string;
		cancelLabel?: string;
		disabled?: boolean;
		onSave: () => void | Promise<void>;
		onCancel?: () => void | Promise<void>;
	}

	let {
		saving = false,
		saveLabel = 'Save',
		cancelLabel = 'Cancel',
		disabled = false,
		onSave,
		onCancel
	}: Props = $props();
</script>

<div class="form-section flex justify-end gap-2">
	{#if onCancel}
		<button class="btn-secondary" type="button" onclick={() => onCancel?.()} disabled={saving}>
			{cancelLabel}
		</button>
	{/if}
	<button
		class="btn-primary bg-emerald-500 hover:bg-emerald-600"
		type="button"
		onclick={() => onSave()}
		disabled={saving || disabled}
	>
		{saving ? 'Saving…' : saveLabel}
	</button>
</div>
