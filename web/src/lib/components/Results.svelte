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
		'box-shadow: 0 16px 32px -14px rgba(0,0,0,.45), 0 0 0 8px color-mix(in srgb, var(--color-sun) 24%, transparent), 0 0 0 3px var(--color-sun);';
	// Approval/ranked scores are vote counts; score-method totals are stars.
	const fmt = (n: number) => (r?.method === 'score' ? `${n} ★` : `${n} ${n === 1 ? 'vote' : 'votes'}`);

	let confettiHost: HTMLElement;
	onMount(() => {
		if (hasWinner) requestAnimationFrame(() => launchConfetti(confettiHost));
	});
</script>

<!-- full-viewport overlay the confetti canvas mounts into -->
<div bind:this={confettiHost} class="pointer-events-none fixed inset-0 z-[60]"></div>

<section>
	{#if !r || !hasWinner}
		<div class="rounded-[20px] border border-line bg-surface2 p-8 text-center text-[15px] font-semibold text-muted">
			No votes were cast, so there’s no winner.
		</div>
	{:else}
		<div class="text-center">
			<p class="text-sm font-extrabold tracking-[0.06em] text-sun-ink uppercase">
				{isRandom ? 'Picked at random' : isTie ? 'It’s a tie!' : 'Tonight you’re watching'}
			</p>
			<div class="mt-5 flex flex-wrap justify-center gap-x-6 gap-y-8">
				{#each r.winners as w (w.nomination_id)}
					{@const n = nomById.get(w.nomination_id)}
					<div class="winner-pop w-44">
						{#if n}
							<button type="button" onclick={() => launchConfetti(confettiHost)} class="block w-full cursor-pointer overflow-hidden rounded-[18px] [&_.poster]:rounded-[18px]" style={ringStyle} aria-label="Celebrate {n.title} again">
								<PosterImage itemId={n.item_id} tag={n.image_tag} posterUrl={n.poster_url ?? ''} title={n.title} />
							</button>
							<h2 class="mt-5 font-display text-[28px] leading-tight font-bold text-ink [overflow-wrap:anywhere]">{n.title}</h2>
						{:else}
							<h2 class="font-display text-[28px] leading-tight font-bold text-ink">{w.title}</h2>
						{/if}
						<p class="mt-1 text-[15px] font-semibold text-muted">
							{[n?.year || '', !isRandom ? fmt(w.score) : ''].filter(Boolean).join(' · ')}
						</p>
						{#if w.nominators && w.nominators.length > 0}
							<p class="mt-0.5 text-sm font-semibold text-faint">Nominated by {w.nominators.join(', ')}</p>
						{/if}
						{#if w.request_status}
							<p class="mt-2 text-sm font-bold text-mango-ink">Requested via Seerr · {w.request_status}</p>
						{:else if n?.source === 'seerr' && poll.seerr_enabled && isHost}
							<button onclick={() => requestWinner(w.nomination_id)} class="mt-3 h-10 w-full rounded-xl bg-mango px-3 text-sm font-extrabold text-[#3a230a] hover:brightness-95">
								Request via Seerr
							</button>
						{/if}
					</div>
				{/each}
			</div>
		</div>

		{#if reqError}<p class="mt-3 text-center text-sm font-bold text-coral-ink" role="alert">{reqError}</p>{/if}

		{#if isRandom}
			{#if others.length > 0}
				<div class="card mt-8 p-4">
					<h3 class="section-h">The other nominations</h3>
					<ul class="mt-3 space-y-2 text-[15px] font-semibold text-ink">
						{#each others as e (e.nomination_id)}
							<li>
								{e.title}{#if e.nominators && e.nominators.length > 0}<span class="text-faint"> · {e.nominators.join(', ')}</span>{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		{:else}
			<div class="card mt-8 p-4">
				<h3 class="section-h">How everyone voted</h3>
				<div class="mt-4 space-y-4">
					{#each r.ranked as e (e.nomination_id)}
						{@const win = winnerIds.has(e.nomination_id)}
						<div>
							<div class="flex items-baseline justify-between gap-2">
								<span class="min-w-0 truncate text-[15px] font-bold {win ? 'text-accent-ink' : 'text-ink'}">{e.title}</span>
								<span class="shrink-0 text-sm font-extrabold tabular-nums text-muted">{fmt(e.score)}</span>
							</div>
							<div class="bar mt-1.5 h-2.5 {win ? 'bar-win' : ''}">
								<i style="width: {Math.max(2, (e.score / max) * 100)}%"></i>
							</div>
							{#if e.nominators && e.nominators.length > 0}
								<p class="mt-1 text-[13px] font-semibold text-faint">by {e.nominators.join(', ')}</p>
							{/if}
						</div>
					{/each}
				</div>
				{#if r.method === 'ranked' && r.rounds && r.rounds.length > 1}
					<p class="mt-4 text-sm font-semibold text-faint">
						Decided by instant runoff over {r.rounds.length} rounds. Bars show each title’s final-round support.
					</p>
				{/if}
			</div>
		{/if}
	{/if}

	<div class="action-bar">
		<a href="/" class="btn btn-ghost h-[52px] w-full border-[1.5px] border-line2 bg-transparent text-base text-ink">Start another poll</a>
	</div>
</section>
