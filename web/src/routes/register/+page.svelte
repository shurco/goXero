<script lang="ts">
	import { authApi } from '$lib/api';
	import { session } from '$lib/stores/session';
	import { goto } from '$app/navigation';

	let email = $state('');
	let password = $state('');
	let firstName = $state('');
	let lastName = $state('');
	let loading = $state(false);
	let error = $state<string | null>(null);

	async function submit(e: Event) {
		e.preventDefault();
		loading = true;
		error = null;
		try {
			const res = await authApi.register({ email, password, firstName, lastName });
			session.login({
				token: res.token,
				refreshToken: res.refreshToken,
				expiresAt: res.expiresAt,
				refreshTokenExpiresAt: res.refreshTokenExpiresAt,
				email: res.email,
				firstName: res.user?.firstName,
				lastName: res.user?.lastName,
				tenants: res.organisations || []
			});
			goto('/app');
		} catch (err) {
			error = (err as Error).message || 'Registration failed';
		} finally {
			loading = false;
		}
	}
</script>

<div class="min-h-screen bg-gradient-to-br from-brand-500 via-brand-700 to-ink-900 flex flex-col">
	<header class="px-8 py-6 flex items-center justify-between text-white">
		<a href="/" class="flex items-center gap-2 font-bold text-xl tracking-tight">
			<span class="inline-flex h-9 w-9 items-center justify-center rounded-full bg-white text-brand-700 font-black">X</span>
			goXero
		</a>
		<a href="/login" class="text-sm text-white/90 hover:text-white underline-offset-4 hover:underline">Sign in</a>
	</header>

	<main class="flex-1 flex items-center justify-center px-4 pb-12">
		<div class="w-full max-w-md">
			<div class="bg-white rounded-2xl shadow-pop p-8">
				<h1 class="text-2xl font-semibold text-ink-900">Create your account</h1>
				<p class="muted text-sm mt-1">Start running your business beautifully.</p>

				<form class="mt-6 space-y-4" onsubmit={submit}>
					<div class="grid grid-cols-2 gap-4">
						<div>
							<label class="label" for="first">First name</label>
							<input id="first" type="text" class="input" bind:value={firstName} />
						</div>
						<div>
							<label class="label" for="last">Last name</label>
							<input id="last" type="text" class="input" bind:value={lastName} />
						</div>
					</div>
					<div>
						<label class="label" for="email">Email</label>
						<input id="email" type="email" class="input" bind:value={email} required />
					</div>
					<div>
						<label class="label" for="password">Password</label>
						<input id="password" type="password" class="input" bind:value={password} required minlength="6" />
					</div>
					{#if error}
						<div class="rounded-lg bg-red-50 text-red-700 text-sm px-3 py-2 border border-red-100">{error}</div>
					{/if}
					<button class="btn-primary w-full" disabled={loading}>
						{loading ? 'Creating…' : 'Create account'}
					</button>
				</form>
			</div>
		</div>
	</main>
</div>
