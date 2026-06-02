<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { PollView } from '$lib/types';
	import PosterImage from './PosterImage.svelte';
	import { launchConfetti } from '$lib/confetti';

	let {
		poll,
		code = '',
		update
	}: { poll: PollView; code?: string; update?: (p: PollView) => void } = $props();

	const r = $derived(poll.results);
	const isHost = $derived(poll.me?.is_host ?? false);

	let reqError = $state('');
	async function requestWinner(id: string) {
		reqError = '';
		try {
			const p = await api.requestWinner(code, id);
			update?.(p);
		} catch (e) {
			reqError = e instanceof Error ? e.message : 'request failed';
		}
	}
	const nomById = $derived(new Map(poll.nominations.map((n) => [n.id, n])));
	const max = $derived(Math.max(1, ...(r?.ranked.map((x) => x.score) ?? [1])));
	const winnerIds = $derived(new Set(r?.winners.map((w) => w.nomination_id) ?? []));
	const isTie = $derived((r?.winners.length ?? 0) > 1);
	const hasWinner = $derived((r?.winners.length ?? 0) > 0);
	const isRandom = $derived(r?.method === 'random');
	const others = $derived((r?.ranked ?? []).filter((e) => !winnerIds.has(e.nomination_id)));

	// celebratory ring around the winning poster (theme-aware via tokens)
	const ringStyle =
		'box-shadow: 0 8px 24px -10px rgba(0,0,0,.32), 0 0 0 4px color-mix(in srgb, var(--color-sun) 22%, transparent), 0 0 0 2px var(--color-sun);';

	let confettiHost: HTMLElement;
	onMount(() => {
		if (hasWinner) requestAnimationFrame(() => launchConfetti(confettiHost));
	});
</script>

<!-- full-viewport overlay the confetti canvas mounts into -->
<div bind:this={confettiHost} class="pointer-events-none fixed inset-0 z-[60]"></div>

<section class="space-y-6">
	{#if !r || !hasWinner}
		<div class="rounded-[20px] border border-line bg-surface2 p-8 text-center font-semibold text-muted">
			No votes were cast.
		</div>
	{:else}
		<div class="text-center">
			<div class="mb-2 flex justify-center gap-1.5">
				{#each ['#ff6f5e', '#11b3aa', '#ffa23a', '#ffce5c', '#0e5a7d'] as c (c)}
					<i class="inline-block h-2 w-2 rounded-[2px]" style="background:{c}"></i>
				{/each}
			</div>
			<p class="font-title text-xs font-extrabold uppercase tracking-[0.12em]" style="color: color-mix(in srgb, var(--color-sun) 60%, var(--color-ink));">
				{isRandom ? '🎲 Randomly selected' : isTie ? "It's a tie!" : 'The winner is'}
			</p>
			<div class="mt-4 flex flex-wrap justify-center gap-4">
				{#each r.winners as w (w.nomination_id)}
					{@const n = nomById.get(w.nomination_id)}
					<div class="winner-pop w-40">
						{#if n}
							<button type="button" onclick={() => launchConfetti(confettiHost)} class="block w-full cursor-pointer overflow-hidden rounded-[20px]" style={ringStyle} title="Tap to celebrate again">
								<PosterImage itemId={n.item_id} tag={n.image_tag} posterUrl={n.poster_url ?? ''} title={n.title} />
							</button>
							<p class="mt-3 font-display text-lg font-semibold text-ink">{n.title}</p>
							<p class="text-xs font-semibold text-faint">{n.year || ''}</p>
						{:else}
							<p class="font-display text-lg font-semibold text-ink">{w.title}</p>
						{/if}
						{#if w.nominators && w.nominators.length > 0}
							<p class="mt-1 text-xs font-semibold text-muted">Nominated by {w.nominators.join(', ')}</p>
						{/if}
						{#if w.request_status}
							<p class="mt-1 text-xs font-bold text-mango">Requested via Seerr · {w.request_status}</p>
						{:else if n?.source === 'seerr' && poll.seerr_enabled && isHost}
							<button onclick={() => requestWinner(w.nomination_id)} class="mt-2 w-full rounded-lg bg-mango px-2 py-1.5 text-xs font-bold text-[#3a230a] hover:brightness-95">
								Request via Seerr
							</button>
						{/if}
					</div>
				{/each}
			</div>
		</div>

		{#if reqError}<p class="mt-2 text-center text-xs font-semibold text-coral-ink">{reqError}</p>{/if}

		{#if isRandom}
			{#if others.length > 0}
				<div class="rounded-[20px] border border-line bg-surface2 p-4">
					<h3 class="font-title text-xs font-bold uppercase tracking-[0.12em] text-faint">The other nominations</h3>
					<ul class="mt-3 space-y-1.5 text-sm font-semibold text-muted">
						{#each others as e (e.nomination_id)}
							<li>
								{e.title}{#if e.nominators && e.nominators.length > 0}<span class="text-faint"> · by {e.nominators.join(', ')}</span>{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		{:else}
			<div class="card p-4">
				<h3 class="font-title text-xs font-bold uppercase tracking-[0.12em] text-faint">Full results</h3>
				<div class="mt-3 space-y-3">
					{#each r.ranked as e, i (e.nomination_id)}
						<div>
							<div class="flex items-baseline justify-between gap-2">
								<span class="truncate text-sm font-bold {winnerIds.has(e.nomination_id) ? 'text-accent-ink' : 'text-ink'}">
									{i + 1}. {e.title}
								</span>
								<span class="text-xs font-extrabold tabular-nums text-faint">{e.score}</span>
							</div>
							<div class="bar mt-1.5 {winnerIds.has(e.nomination_id) ? 'bar-win' : ''}">
								<i style="width: {(e.score / max) * 100}%"></i>
							</div>
							{#if e.nominators && e.nominators.length > 0}
								<p class="mt-1 text-[11px] font-semibold text-faint">by {e.nominators.join(', ')}</p>
							{/if}
						</div>
					{/each}
				</div>
				{#if r.method === 'ranked' && r.rounds && r.rounds.length > 1}
					<p class="mt-3 text-xs font-semibold text-faint">
						Decided by instant-runoff over {r.rounds.length} rounds. Bars show each title's final-round support.
					</p>
				{/if}
			</div>
		{/if}
	{/if}
</section>
