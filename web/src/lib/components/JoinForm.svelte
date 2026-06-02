<script lang="ts">
	import { api } from '$lib/api';
	import type { PollView } from '$lib/types';
	import UrchinMark from './UrchinMark.svelte';

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

<div class="card mx-auto max-w-sm p-6">
	{#if poll.allow_guests}
		<div class="mb-3.5 flex justify-center"><UrchinMark size={46} /></div>
		<h2 class="text-center font-title text-lg font-bold text-ink">Join “{poll.title}”</h2>
		<p class="mt-1.5 text-center text-sm font-semibold text-muted">Pick a name so others know who's voting.</p>
		<form onsubmit={join} class="mt-4 space-y-3">
			<input
				bind:value={name}
				maxlength="40"
				placeholder="Your name"
				autocomplete="off"
				class="input text-center text-lg"
			/>
			{#if error}<p class="text-sm font-semibold text-coral-ink">{error}</p>{/if}
			<button type="submit" disabled={busy || !name.trim()} class="btn btn-primary w-full">
				{busy ? 'Joining…' : 'Join'}
			</button>
		</form>
	{:else}
		<h2 class="font-title text-lg font-bold text-ink">This poll is closed to guests</h2>
		<p class="mt-1.5 text-sm font-semibold text-muted">
			The host disabled guest access, so you'll need an account to take part. (Account login is
			coming soon.)
		</p>
	{/if}
</div>
