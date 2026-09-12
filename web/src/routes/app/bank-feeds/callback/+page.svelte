<script lang="ts">
	import { goto } from '$app/navigation';
	import { bankFeedApi, type ApiError, type BankFeedConnection } from '$lib/api';
	import { session } from '$lib/stores/session';
	import ModuleHeader from '$lib/components/ModuleHeader.svelte';

	let status = $state('Finishing your bank connection…');
	let error = $state('');
	/**
	 * Banks whose consent is still open because the user has not approved access
	 * there. Reported apart from `error`: nothing has gone wrong, and the fix is
	 * at the bank, not here.
	 */
	let waiting = $state<string[]>([]);
	let busy = $state(false);
	let started = false;

	/**
	 * A connection waiting on a consent session: a brand-new link (PENDING), or
	 * a repair of one the bank has broken (ERROR) — the difference is invisible
	 * here, and does not need to be visible, because finalize finishes whichever
	 * session the connection is holding.
	 */
	function awaitingConsent(c: BankFeedConnection) {
		return (c.Status === 'PENDING' || c.Status === 'ERROR') && Boolean(c.AuthURL);
	}

	/**
	 * Where an aggregator sends the browser once the user has consented (see
	 * BANKFEED_REDIRECT_URL). The client never sees the consent result itself —
	 * for Plaid the public token is only reachable server-side — so this page
	 * just asks the API to finalize every connection still waiting on one.
	 */
	async function finish() {
		busy = true;
		error = '';
		waiting = [];
		try {
			const res = await bankFeedApi.listConnections();
			const pending = (res.Connections ?? []).filter(awaitingConsent);
			if (pending.length === 0) {
				status = 'Nothing is waiting for consent — the connection may already be linked.';
				return;
			}
			// Finalize one at a time: an aggregator that refuses (the user closed
			// the consent screen, say) must not stop the others.
			let linked = 0;
			const stillWaiting: string[] = [];
			const failures: string[] = [];
			for (const c of pending) {
				const who = c.InstitutionName || c.Provider;
				try {
					await bankFeedApi.finalize(c.ConnectionID);
					linked++;
				} catch (e) {
					// 409 is the bank saying the consent has not been finished there.
					// That is not a broken connection and not something to retry: the
					// user still has to approve access, so it is shown as waiting.
					if ((e as ApiError).status === 409) stillWaiting.push(who);
					else failures.push(`${who}: ${(e as Error).message}`);
				}
			}
			if (failures.length > 0) {
				status = `${linked} of ${pending.length} connection(s) linked.`;
				error = failures.join('\n');
				return;
			}
			if (stillWaiting.length > 0) {
				waiting = stillWaiting;
				status =
					linked > 0
						? `${linked} of ${pending.length} connection(s) linked.`
						: 'Waiting for the bank to confirm access.';
				return;
			}
			await goto('/app/bank-feeds');
		} catch (e) {
			error = (e as Error).message;
			status = 'Could not finish the connection.';
		} finally {
			busy = false;
		}
	}

	// The tenant is resolved asynchronously on load, so wait for it before
	// asking the API anything.
	$effect(() => {
		if (!started && $session.tenantId) {
			started = true;
			void finish();
		}
	});
</script>

<ModuleHeader
	title="Completing your bank connection"
	subtitle="Hang on while we pull the accounts you just authorised."
/>

<div class="card p-6 max-w-xl">
	{#if error}
		<p class="text-sm text-red-700 whitespace-pre-line">{error}</p>
	{/if}
	<p class="muted">{status}</p>
	{#if waiting.length > 0}
		<p class="text-sm mt-3">
			{waiting.join(', ')} {waiting.length === 1 ? 'is' : 'are'} still waiting on you at the bank —
			open the consent link and approve access there. Nothing else is needed from this page: the
			connection is finished the moment the bank reports it, webhook or no webhook.
		</p>
	{/if}
	<!--
		The way back is always here, error or not: it is the only link this page
		has, and a landing that arrives with nothing waiting — the session was
		completed by the provider's webhook first, most often — is otherwise a dead
		end.
	-->
	<div class="flex gap-3 mt-4">
		{#if error || waiting.length > 0}
			<button class="btn-secondary" disabled={busy} onclick={() => finish()}>
				{error ? 'Try again' : 'Check again'}
			</button>
		{/if}
		<a class="btn-primary" href="/app/bank-feeds">Back to bank feeds</a>
	</div>
</div>
