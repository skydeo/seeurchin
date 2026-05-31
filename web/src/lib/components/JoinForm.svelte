<script lang="ts">
	import { api } from '$lib/api';
	import type { PollView } from '$lib/types';

	let {
		poll,
		code,
		update
	}: { poll: PollView; code: string; update: (p: PollView) => void } = $props();

	let name = $state('');
	let busy = $state(false);
	let error = $state('');

	async function join(e: Event) {
		e.preventDefault();
		if (!name.trim() || busy) return;
		busy = true;
		error = '';
		try {
			update(await api.join(code, name.trim()));
		} catch (err) {
			error = err instanceof Error ? err.message : 'could not join';
		} finally {
			busy = false;
		}
	}
</script>

<div class="mx-auto max-w-sm rounded-2xl bg-slate-900/70 p-6 ring-1 ring-white/10">
	{#if poll.allow_guests}
		<h2 class="text-lg font-semibold">Join “{poll.title}”</h2>
		<p class="mt-1 text-sm text-slate-400">Pick a name so others know who's voting.</p>
		<form onsubmit={join} class="mt-4 space-y-3">
			<input
				bind:value={name}
				maxlength="40"
				placeholder="Your name"
				autocomplete="off"
				class="w-full rounded-xl bg-slate-800 px-4 py-3 text-lg ring-1 ring-white/10 outline-none focus:ring-2 focus:ring-brand-500"
			/>
			{#if error}<p class="text-sm text-rose-400">{error}</p>{/if}
			<button
				type="submit"
				disabled={busy || !name.trim()}
				class="w-full rounded-xl bg-brand-500 px-4 py-3 font-semibold text-white transition hover:bg-brand-600 disabled:opacity-40"
			>
				{busy ? 'Joining…' : 'Join'}
			</button>
		</form>
	{:else}
		<h2 class="text-lg font-semibold">This poll is closed to guests</h2>
		<p class="mt-1 text-sm text-slate-400">
			The host disabled guest access, so you'll need an account to take part. (Account login is
			coming soon.)
		</p>
	{/if}
</div>
