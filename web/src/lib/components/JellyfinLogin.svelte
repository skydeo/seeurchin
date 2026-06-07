<script lang="ts">
	import { api } from '$lib/api';
	import type { UserSession } from '$lib/types';
	import UrchinMark from './UrchinMark.svelte';

	let {
		onsuccess,
		heading = 'Sign in with Jellyfin',
		sub = 'Use your Jellyfin account to continue.'
	}: {
		onsuccess: (s: UserSession) => void;
		heading?: string;
		sub?: string;
	} = $props();

	let username = $state('');
	let password = $state('');
	let busy = $state(false);
	let error = $state('');

	async function submit(e: Event) {
		e.preventDefault();
		if (busy || !username.trim() || !password) return;
		busy = true;
		error = '';
		try {
			const s = await api.userLogin(username.trim(), password);
			password = '';
			onsuccess(s);
		} catch (err) {
			error = err instanceof Error ? err.message : 'login failed';
		} finally {
			busy = false;
		}
	}
</script>

<form onsubmit={submit} class="card mx-auto max-w-sm space-y-4 p-6">
	<div class="text-center">
		<div class="mb-3 flex justify-center"><UrchinMark size={44} /></div>
		<h2 class="font-title text-lg font-bold text-ink">{heading}</h2>
		<p class="mt-1.5 text-sm font-semibold text-muted">{sub}</p>
	</div>
	<label class="block">
		<span class="mb-1.5 block text-sm font-bold text-muted">Jellyfin username</span>
		<input bind:value={username} autocomplete="username" required class="input" />
	</label>
	<label class="block">
		<span class="mb-1.5 block text-sm font-bold text-muted">Password</span>
		<input
			bind:value={password}
			type="password"
			autocomplete="current-password"
			required
			class="input"
		/>
	</label>
	{#if error}<p class="text-sm font-semibold text-coral-ink">{error}</p>{/if}
	<button type="submit" disabled={busy || !username.trim() || !password} class="btn btn-primary w-full">
		{busy ? 'Signing in…' : 'Sign in'}
	</button>
</form>
