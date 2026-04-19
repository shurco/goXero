<script lang="ts">
	import { onMount } from 'svelte';
	import { orgApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';
	import {
		EMAIL_PLACEHOLDER_OPTIONS,
		EMAIL_TEMPLATE_TYPE_OPTIONS,
		seedEmailTemplates
	} from '$lib/email-settings-defaults';
	import type { Organisation, OrgEmailTemplate, ReplyEmailAddress } from '$lib/types';

	let loading = $state(true);
	let saving = $state(false);
	let org = $state<Organisation | null>(null);
	let successMsg = $state('');
	let errorMsg = $state('');

	let userName = $state('User');
	let userEmail = $state('user@example.com');

	let templates = $state<OrgEmailTemplate[]>([]);
	let replyAddresses = $state<ReplyEmailAddress[]>([]);

	let showAddress = $state(true);
	let showTemplates = $state(true);

	let sortKey = $state<'Type' | 'Name'>('Name');
	let sortDir = $state<'asc' | 'desc'>('asc');

	let showTemplateModal = $state(false);
	let templateModalAdd = $state(true);
	let templateDraft = $state<OrgEmailTemplate>(blankTemplate());
	let subjectEl = $state<HTMLInputElement | null>(null);
	let bodyEl = $state<HTMLTextAreaElement | null>(null);
	let placeholderTarget = $state<'Subject' | 'Body'>('Body');

	let showReplyModal = $state(false);
	let replyDraft = $state<{ Email: string; Name: string }>({ Email: '', Name: '' });
	let replyEditId = $state<string | null>(null);

	function blankTemplate(): OrgEmailTemplate {
		return {
			ID: '',
			Type: 'Quote',
			Name: '',
			IsDefault: false,
			Subject: '',
			Body: ''
		};
	}

	function newId() {
		return typeof crypto !== 'undefined' && crypto.randomUUID
			? crypto.randomUUID()
			: `id-${Date.now()}-${Math.random().toString(36).slice(2)}`;
	}

	function applyOrg(o: Organisation | null) {
		if (!o) {
			templates = [];
			replyAddresses = [];
			return;
		}
		const raw = o.Profile?.EmailTemplates;
		templates = raw === undefined ? seedEmailTemplates() : [...(raw ?? [])];
		replyAddresses = [...(o.Profile?.ReplyAddresses ?? [])];
	}

	async function reload() {
		loading = true;
		errorMsg = '';
		try {
			org = (await orgApi.current()) ?? null;
			applyOrg(org);
			const s = $session;
			if (s.email) {
				userName = [s.firstName, s.lastName].filter(Boolean).join(' ') || s.email;
				userEmail = s.email;
			}
		} catch (e) {
			errorMsg = e instanceof Error ? e.message : 'Failed to load settings';
		} finally {
			loading = false;
		}
	}

	async function persist(profilePatch: { EmailTemplates?: OrgEmailTemplate[]; ReplyAddresses?: ReplyEmailAddress[] }) {
		if (!org) return;
		saving = true;
		errorMsg = '';
		successMsg = '';
		try {
			const updated = await orgApi.update({
				Name: org.Name,
				LegalName: org.LegalName ?? '',
				OrganisationType: org.OrganisationType ?? 'COMPANY',
				CountryCode: org.CountryCode ?? '',
				LineOfBusiness: org.LineOfBusiness ?? '',
				RegistrationNumber: org.RegistrationNumber ?? '',
				Description: org.Description ?? '',
				Timezone: org.Timezone || 'UTC',
				TaxNumber: org.TaxNumber ?? '',
				Profile: {
					...(org.Profile ?? {}),
					...profilePatch
				}
			});
			org = updated;
			applyOrg(updated);
			successMsg = 'Saved.';
			window.setTimeout(() => {
				successMsg = '';
			}, 4000);
		} catch (e) {
			errorMsg = e instanceof Error ? e.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	function openAddTemplate() {
		errorMsg = '';
		templateModalAdd = true;
		templateDraft = { ...blankTemplate(), ID: newId() };
		placeholderTarget = 'Body';
		showTemplateModal = true;
	}

	function openEditTemplate(t: OrgEmailTemplate) {
		errorMsg = '';
		templateModalAdd = false;
		templateDraft = { ...t };
		placeholderTarget = 'Body';
		showTemplateModal = true;
	}

	function applyDefaultUniqueness(list: OrgEmailTemplate[], row: OrgEmailTemplate): OrgEmailTemplate[] {
		if (!row.IsDefault) {
			return list.map((x) => (x.ID === row.ID ? row : x));
		}
		return list.map((x) => {
			if (x.ID === row.ID) return { ...row, IsDefault: true };
			if (x.Type === row.Type && x.ID !== row.ID) return { ...x, IsDefault: false };
			return x;
		});
	}

	async function saveTemplateModal() {
		const name = templateDraft.Name.trim();
		const type = templateDraft.Type.trim();
		if (!name || !type) {
			errorMsg = 'Type and name are required.';
			return;
		}
		let next = [...templates];
		const row = {
			...templateDraft,
			ID: templateDraft.ID || newId(),
			Name: name,
			Type: type,
			Subject: templateDraft.Subject?.trim() ?? '',
			Body: templateDraft.Body ?? ''
		};
		if (templateModalAdd) {
			next.push(row);
		} else {
			next = next.map((x) => (x.ID === row.ID ? row : x));
		}
		next = applyDefaultUniqueness(next, row);
		await persist({ EmailTemplates: next });
		if (!errorMsg) showTemplateModal = false;
	}

	function insertPlaceholder(token: string) {
		const el =
			placeholderTarget === 'Subject'
				? subjectEl
				: bodyEl;
		if (!el) {
			if (placeholderTarget === 'Subject') {
				templateDraft = { ...templateDraft, Subject: (templateDraft.Subject ?? '') + token };
			} else {
				templateDraft = { ...templateDraft, Body: (templateDraft.Body ?? '') + token };
			}
			return;
		}
		const start = el.selectionStart ?? 0;
		const end = el.selectionEnd ?? 0;
		const val = el.value;
		const next = val.slice(0, start) + token + val.slice(end);
		if (placeholderTarget === 'Subject') {
			templateDraft = { ...templateDraft, Subject: next };
		} else {
			templateDraft = { ...templateDraft, Body: next };
		}
		queueMicrotask(() => {
			const pos = start + token.length;
			el.setSelectionRange(pos, pos);
			el.focus();
		});
	}

	function openAddReply() {
		errorMsg = '';
		replyEditId = null;
		replyDraft = { Email: '', Name: '' };
		showReplyModal = true;
	}

	function openEditReply(r: ReplyEmailAddress) {
		errorMsg = '';
		replyEditId = r.ID ?? null;
		replyDraft = { Email: r.Email, Name: r.Name ?? '' };
		showReplyModal = true;
	}

	const replyFormValid = $derived(
		replyDraft.Email.trim().length > 0 &&
			/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(replyDraft.Email.trim()) &&
			replyDraft.Name.trim().length > 0
	);

	async function saveReplyModal() {
		if (!replyFormValid) return;
		const email = replyDraft.Email.trim();
		const name = replyDraft.Name.trim();
		let next = [...replyAddresses];
		if (replyEditId) {
			next = next.map((x) =>
				x.ID === replyEditId ? { ...x, Email: email, Name: name } : x
			);
		} else {
			next.push({ ID: newId(), Email: email, Name: name });
		}
		await persist({ ReplyAddresses: next });
		if (!errorMsg) showReplyModal = false;
	}

	async function removeReply(id: string | undefined) {
		if (!id) return;
		const next = replyAddresses.filter((x) => x.ID !== id);
		await persist({ ReplyAddresses: next });
	}

	const sortedTemplates = $derived(
		[...templates].sort((a, b) => {
			const ak = String(a[sortKey] ?? '');
			const bk = String(b[sortKey] ?? '');
			return sortDir === 'asc' ? ak.localeCompare(bk) : bk.localeCompare(ak);
		})
	);

	const standardCount = $derived(templates.filter((t) => t.IsDefault).length);
	const customCount = $derived(templates.filter((t) => !t.IsDefault).length);

	function toggleSort(k: 'Type' | 'Name') {
		if (sortKey === k) {
			sortDir = sortDir === 'asc' ? 'desc' : 'asc';
		} else {
			sortKey = k;
			sortDir = 'asc';
		}
	}

	onMount(reload);
	$effect(() => {
		if ($session.tenantId) void reload();
	});
</script>

<div class="w-full space-y-6">
	<p class="text-sm mb-1">
		<a href="/app/settings" class="text-brand-600 hover:underline">Settings</a>
	</p>

	<ModuleHeader
		title="Email settings"
		subtitle="Control the display name, reply addresses, and default wording for emails sent from this organisation."
	/>

	{#if successMsg}
		<p class="text-sm text-emerald-800" role="status">{successMsg}</p>
	{/if}
	{#if errorMsg && !showTemplateModal && !showReplyModal}
		<p class="text-sm text-red-700" role="alert">{errorMsg}</p>
	{/if}

	{#if loading}
		<div class="card p-6 muted">Loading…</div>
	{:else}
		<div class="max-w-5xl space-y-4">
			<p class="muted text-sm max-w-2xl">
				Display names and reply addresses control how outgoing mail appears to recipients. Use the sections
				below to align branding with your organisation.
			</p>

			<div class="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">
				<!-- Email address -->
				<div class="card overflow-hidden">
					<button
						type="button"
						class="w-full flex items-center justify-between px-5 py-4 text-left hover:bg-ink-50/60 transition-colors"
						onclick={() => (showAddress = !showAddress)}
						aria-expanded={showAddress}
					>
						<div>
							<h2 class="text-sm font-semibold text-ink-900">Email address</h2>
							{#if !showAddress}
								<p class="muted text-sm mt-1">
									Sent as <span class="font-medium text-ink-800">{userName}</span>, replies to
									<span class="font-medium text-ink-800">&lt;{userEmail}&gt;</span>
									{#if replyAddresses.length}
										· {replyAddresses.length} extra reply {replyAddresses.length === 1 ? 'line' : 'lines'}
									{/if}
								</p>
							{/if}
						</div>
						<span class="text-brand-600 text-sm shrink-0">{showAddress ? 'Hide' : 'Show'}</span>
					</button>

					{#if showAddress}
						<div class="px-5 pb-5 border-t border-ink-100 pt-4 space-y-4">
							<div class="rounded-lg border border-ink-100 bg-ink-50/40 p-4">
								<p class="text-xs font-semibold uppercase tracking-wide text-ink-500 mb-1">From</p>
								<p class="text-sm text-ink-900">
									<span class="font-semibold">{userName}</span>
									<span class="muted"> — replies go to </span>
									<span class="font-mono text-ink-800">&lt;{userEmail}&gt;</span>
								</p>
								<p class="muted text-xs mt-2">
									Emails sent from this organisation use the logged-in user as the sender unless you add
									extra reply addresses below.
								</p>
							</div>

							{#if replyAddresses.length > 0}
								<ul class="space-y-2">
									{#each replyAddresses as r (r.ID)}
										<li
											class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-ink-100 px-3 py-2 text-sm"
										>
											<div>
												<span class="font-medium text-ink-900">{r.Name || '—'}</span>
												<span class="muted"> · </span>
												<span class="font-mono text-ink-800">{r.Email}</span>
											</div>
											<div class="flex gap-2">
												<button
													type="button"
													class="text-brand-600 text-xs font-medium hover:underline"
													onclick={() => openEditReply(r)}>Edit</button
												>
												<button
													type="button"
													class="text-red-600 text-xs font-medium hover:underline"
													onclick={() => removeReply(r.ID)}>Remove</button
												>
											</div>
										</li>
									{/each}
								</ul>
							{/if}

							<button
								type="button"
								class="text-brand-600 text-sm font-medium hover:underline"
								onclick={openAddReply}
								disabled={saving}
							>
								+ Add reply email address
							</button>
						</div>
					{/if}
				</div>

				<!-- Templates summary when address collapsed: show hint -->
				<div class="card overflow-hidden lg:min-h-[120px]">
					<div class="px-5 py-4">
						<h2 class="text-sm font-semibold text-ink-900">Quick tips</h2>
						<p class="muted text-sm mt-2 leading-relaxed">
							Use <span class="font-medium text-ink-800">templates</span> to customise subjects and bodies. Placeholders
							like <span class="font-mono text-xs">[Quote Number]</span> are replaced when the email is sent.
						</p>
					</div>
				</div>
			</div>

			<!-- Templates full width -->
			<div class="card overflow-hidden">
				<button
					type="button"
					class="w-full flex items-center justify-between px-5 py-4 text-left hover:bg-ink-50/60 transition-colors"
					onclick={() => (showTemplates = !showTemplates)}
					aria-expanded={showTemplates}
				>
					<div>
						<h2 class="text-sm font-semibold text-ink-900">Templates</h2>
						{#if !showTemplates}
							<p class="muted text-sm mt-1">
								<span class="font-medium text-ink-800">{standardCount}</span> default ·
								<span class="font-medium text-ink-800">{customCount}</span> custom ·
								<span class="font-medium text-ink-800">{templates.length}</span> total
							</p>
						{/if}
					</div>
					<span class="text-brand-600 text-sm shrink-0">{showTemplates ? 'Hide' : 'Show'}</span>
				</button>

				{#if showTemplates}
					<div class="border-t border-ink-100">
						<table class="table-auto-xero">
							<thead>
								<tr>
									<th>
										<button
											type="button"
											class="th-sort {sortKey === 'Type'
												? sortDir === 'asc'
													? 'th-sort-asc'
													: 'th-sort-desc'
												: ''}"
											onclick={() => toggleSort('Type')}
										>
											Type
										</button>
									</th>
									<th>
										<button
											type="button"
											class="th-sort {sortKey === 'Name'
												? sortDir === 'asc'
													? 'th-sort-asc'
													: 'th-sort-desc'
												: ''}"
											onclick={() => toggleSort('Name')}
										>
											Name
										</button>
									</th>
								</tr>
							</thead>
							<tbody>
								{#each sortedTemplates as t, ti ((t.ID ?? '') + '-' + ti)}
									<tr>
										<td>
											{t.Type}
											{#if t.IsDefault}
												<span class="muted uppercase text-[10px] font-semibold ml-1">Default</span>
											{/if}
										</td>
										<td>
											<button
												type="button"
												class="text-brand-600 hover:underline text-left"
												onclick={() => openEditTemplate(t)}
											>
												{t.Name}
											</button>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
						<div class="px-5 py-3 border-t border-ink-100 flex flex-wrap gap-3">
							<button
								type="button"
								class="text-brand-600 text-sm font-medium hover:underline"
								onclick={openAddTemplate}
								disabled={saving}
							>
								+ Add email template
							</button>
						</div>
					</div>
				{/if}
			</div>
		</div>
	{/if}
</div>

{#if showTemplateModal}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-ink-900/40 flex items-center justify-center z-[60] p-4"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) showTemplateModal = false;
		}}
	>
		<div
			class="bg-white rounded-xl shadow-pop w-full max-w-lg max-h-[90vh] flex flex-col border border-ink-100 outline-none"
			role="dialog"
			aria-modal="true"
			aria-labelledby="tpl-modal-title"
			tabindex="-1"
		>
			<div class="p-5 border-b border-ink-100 flex items-center justify-between shrink-0">
				<h3 id="tpl-modal-title" class="font-semibold text-lg text-ink-900">
					{templateModalAdd ? 'Add an email template' : `Edit ${templateDraft.Type} template`}
				</h3>
				<button
					type="button"
					class="btn-ghost"
					onclick={() => (showTemplateModal = false)}
					aria-label="Close">✕</button
				>
			</div>
			<div class="p-5 space-y-4 overflow-y-auto flex-1 min-h-0">
				{#if errorMsg}
					<p class="text-sm text-red-700" role="alert">{errorMsg}</p>
				{/if}
				<div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
					<label class="block" for="tpl-type">
						<span class="label">Type</span>
						<select id="tpl-type" class="input w-full" bind:value={templateDraft.Type}>
							{#each EMAIL_TEMPLATE_TYPE_OPTIONS as opt}
								<option value={opt}>{opt}</option>
							{/each}
						</select>
					</label>
					<label class="block" for="tpl-name">
						<span class="label">Name</span>
						<input
							id="tpl-name"
							class="input w-full"
							bind:value={templateDraft.Name}
							placeholder="e.g. Basic"
						/>
					</label>
				</div>
				<label class="flex items-center gap-2 cursor-pointer" for="tpl-default">
					<input
						id="tpl-default"
						type="checkbox"
						class="accent-brand-500"
						bind:checked={templateDraft.IsDefault}
					/>
					<span class="text-sm text-ink-800">Default for this type</span>
				</label>

				<div>
					<div class="flex items-center justify-between gap-2 mb-2">
						<span class="label mb-0">Message</span>
						<div class="flex items-center gap-2 text-sm">
							<span class="muted">Insert into</span>
							<select
								class="input py-1 text-xs max-w-[140px]"
								bind:value={placeholderTarget}
							>
								<option value="Subject">Subject</option>
								<option value="Body">Body</option>
							</select>
						</div>
					</div>
					<div class="flex flex-wrap gap-2 mb-2">
						<select
							class="input py-1.5 text-sm flex-1 min-w-[200px]"
							aria-label="Insert placeholder"
							onchange={(e) => {
								const v = (e.currentTarget as HTMLSelectElement).value;
								if (v) {
									insertPlaceholder(v);
									(e.currentTarget as HTMLSelectElement).selectedIndex = 0;
								}
							}}
						>
							<option value="">Insert placeholder…</option>
							{#each EMAIL_PLACEHOLDER_OPTIONS as ph}
								<option value={ph}>{ph}</option>
							{/each}
						</select>
					</div>
					<label class="block mb-3" for="tpl-subject">
						<span class="label">Subject</span>
						<input
							id="tpl-subject"
							class="input w-full"
							bind:this={subjectEl}
							bind:value={templateDraft.Subject}
							onfocus={() => (placeholderTarget = 'Subject')}
						/>
					</label>
					<label class="block" for="tpl-body">
						<span class="label">Body</span>
						<textarea
							id="tpl-body"
							class="input w-full min-h-[200px] font-mono text-sm leading-relaxed"
							bind:this={bodyEl}
							bind:value={templateDraft.Body}
							onfocus={() => (placeholderTarget = 'Body')}
						></textarea>
					</label>
				</div>
			</div>
			<div class="p-5 border-t border-ink-100 flex justify-end gap-2 shrink-0">
				<button type="button" class="btn-secondary" onclick={() => (showTemplateModal = false)}
					>Cancel</button
				>
				<button type="button" class="btn-primary" onclick={() => void saveTemplateModal()} disabled={saving}>
					{saving ? 'Saving…' : 'Save'}
				</button>
			</div>
		</div>
	</div>
{/if}

{#if showReplyModal}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-ink-900/40 flex items-center justify-center z-[60] p-4"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) showReplyModal = false;
		}}
	>
		<div
			class="bg-white rounded-xl shadow-pop w-full max-w-md border border-ink-100 outline-none"
			role="dialog"
			aria-modal="true"
			aria-labelledby="reply-modal-title"
			tabindex="-1"
		>
			<div class="p-5 border-b border-ink-100 flex items-center justify-between">
				<h3 id="reply-modal-title" class="font-semibold text-lg text-ink-900">
					{replyEditId ? 'Edit reply email address' : 'Add a new reply email address'}
				</h3>
				<button type="button" class="btn-ghost" onclick={() => (showReplyModal = false)} aria-label="Close"
					>✕</button
				>
			</div>
			<div class="p-5 space-y-4">
				{#if errorMsg}
					<p class="text-sm text-red-700" role="alert">{errorMsg}</p>
				{/if}
				<label class="block" for="reply-email">
					<span class="label">‘Reply to’ email address</span>
					<input
						id="reply-email"
						type="email"
						class="input w-full"
						autocomplete="email"
						bind:value={replyDraft.Email}
					/>
				</label>
				<label class="block" for="reply-name">
					<span class="label">Email name</span>
					<input
						id="reply-name"
						type="text"
						class="input w-full"
						placeholder="e.g. Hornblower Enterprises"
						bind:value={replyDraft.Name}
					/>
				</label>
				<p class="text-sm text-ink-600">
					Emails sent from this organisation can use this name and address for replies.
				</p>
			</div>
			<div class="p-5 border-t border-ink-100 flex justify-end gap-2">
				<button type="button" class="btn-secondary" onclick={() => (showReplyModal = false)}>Cancel</button>
				<button
					type="button"
					class="btn-primary"
					onclick={() => void saveReplyModal()}
					disabled={saving || !replyFormValid}
				>
					{saving ? 'Saving…' : replyEditId ? 'Save' : 'Add email'}
				</button>
			</div>
		</div>
	</div>
{/if}
