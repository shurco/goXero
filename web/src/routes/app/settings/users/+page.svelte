<script lang="ts">
	import { onMount } from 'svelte';
	import { page as pageStore } from '$app/stores';
	import { goto } from '$app/navigation';
	import { userApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { formatDate } from '$lib/utils/format';
	import SettingsHeader from '$lib/components/SettingsHeader.svelte';
	import type { OrgUser } from '$lib/types';

	type Tab = 'current' | 'history';

	let tab = $state<Tab>('current');
	let users = $state<OrgUser[]>([]);
	let loading = $state(true);

	function readUrl() {
		const t = $pageStore.url.searchParams.get('tab');
		tab = t === 'history' ? 'history' : 'current';
	}

	async function reload() {
		loading = true;
		try {
			users = (await userApi.list()) ?? [];
		} catch {
			users = [];
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		readUrl();
		reload();
	});

	let lastSearch = '';
	$effect(() => {
		const cur = $pageStore.url.search;
		if (cur !== lastSearch) {
			lastSearch = cur;
			readUrl();
		}
		if ($session.tenantId) void reload();
	});

	function setTab(t: Tab) {
		void goto(`/app/settings/users${t === 'history' ? '?tab=history' : ''}`, {
			replaceState: true,
			noScroll: true,
			keepFocus: true
		});
	}

	function fullName(u: OrgUser) {
		return `${u.FirstName ?? ''} ${u.LastName ?? ''}`.trim() || u.EmailAddress;
	}

	function initials(name: string) {
		return name
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((p) => p[0]?.toUpperCase())
			.join('');
	}

	function humanRole(role: string) {
		// Translate the Xero-style role into a friendly summary line.
		switch (role.toUpperCase()) {
			case 'ADMIN':
				return 'Adviser · Contact bank account admin, Payroll admin, Expenses (Admin)';
			case 'STANDARD':
				return 'Standard · Can view reports, approve invoices and bills';
			case 'READONLY':
				return 'Read-only · View-only access to the organisation';
			case 'INVOICEONLY':
				return 'Invoice only · Draft & approve invoices';
			default:
				return role;
		}
	}

	function timeAgo(iso?: string) {
		if (!iso) return '';
		const then = new Date(iso).getTime();
		if (!Number.isFinite(then)) return '';
		const diff = Date.now() - then;
		const min = Math.round(diff / 60000);
		if (min < 1) return 'just now';
		if (min < 60) return `${min} minute${min === 1 ? '' : 's'} ago`;
		const hr = Math.round(min / 60);
		if (hr < 24) return `${hr} hour${hr === 1 ? '' : 's'} ago`;
		const day = Math.round(hr / 24);
		return `${day} day${day === 1 ? '' : 's'} ago`;
	}
</script>

<SettingsHeader title="Users">
	<button class="btn-primary" type="button" disabled title="Invite user API is coming soon">
		+ Invite a user
	</button>
</SettingsHeader>

<div class="subnav">
	<button
		type="button"
		class="subnav-item {tab === 'current' ? 'subnav-item-active' : ''}"
		onclick={() => setTab('current')}
	>
		Current users
	</button>
	<button
		type="button"
		class="subnav-item {tab === 'history' ? 'subnav-item-active' : ''}"
		onclick={() => setTab('history')}
	>
		Login history
	</button>
</div>

<div class="mt-6">
	{#if tab === 'current'}
		<div class="card">
			<div class="px-6 py-4 border-b border-ink-100 flex items-center gap-2">
				<h2 class="text-sm font-semibold text-ink-900">Current users</h2>
				<span class="muted text-sm tabular-nums">{users.length}</span>
			</div>

			{#if loading}
				<div class="p-6 text-center muted text-sm">Loading…</div>
			{:else if users.length === 0}
				<div class="p-6 text-center muted text-sm">No users yet.</div>
			{:else}
				<ul class="divide-y divide-ink-100">
					{#each users as u (u.UserID)}
						{@const name = fullName(u)}
						<li class="flex items-start justify-between gap-4 px-6 py-4">
							<div class="flex items-start gap-3">
								<div
									class="h-10 w-10 rounded-full bg-brand-100 text-brand-700 flex items-center justify-center font-semibold text-sm"
								>
									{initials(name)}
								</div>
								<div>
									<div class="flex flex-wrap items-baseline gap-x-2">
										<span class="font-semibold text-ink-900">{name}</span>
										<span class="muted text-sm">{u.EmailAddress}</span>
										{#if u.IsSubscriber}
											<span class="badge badge-active">Subscriber</span>
										{/if}
									</div>
									<div class="muted text-xs mt-0.5">{humanRole(u.OrganisationRole)}</div>
								</div>
							</div>
							<div class="muted text-sm text-right whitespace-nowrap">
								{#if u.CreatedDateUTC}
									Added {timeAgo(u.CreatedDateUTC)}
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{:else}
		<div class="card">
			<div class="px-6 py-4 border-b border-ink-100">
				<h2 class="text-sm font-semibold text-ink-900">Login history</h2>
			</div>
			<div class="overflow-x-auto">
				<table class="table-auto-xero">
					<thead>
						<tr>
							<th>Name</th>
							<th>Login date</th>
						</tr>
					</thead>
					<tbody>
						{#if loading}
							<tr>
								<td colspan="2" class="text-center py-10 muted">Loading…</td>
							</tr>
						{:else if users.length === 0}
							<tr>
								<td colspan="2" class="text-center py-10 muted">No login history yet.</td>
							</tr>
						{:else}
							<!-- Until a dedicated audit-log endpoint exists we approximate with the
							     user's account-creation timestamp, which already ships in /Users. -->
							{#each users as u (u.UserID)}
								<tr>
									<td class="font-medium text-ink-900">{fullName(u)}</td>
									<td>{formatDate(u.CreatedDateUTC, 'DD MMM YYYY [at] h:mm A')}</td>
								</tr>
							{/each}
						{/if}
					</tbody>
				</table>
			</div>
			<div class="px-6 py-3 border-t border-ink-100 muted text-xs">
				Full login history requires an audit-log endpoint — this view currently lists each user's
				registration timestamp as a placeholder.
			</div>
		</div>
	{/if}
</div>
