<script lang="ts">
	import type { PollView } from '$lib/types';
	import PosterImage from './PosterImage.svelte';

	let { poll }: { poll: PollView } = $props();

	const r = $derived(poll.results);
	const nomById = $derived(new Map(poll.nominations.map((n) => [n.id, n])));
	const max = $derived(Math.max(1, ...(r?.ranked.map((x) => x.score) ?? [1])));
	const winnerIds = $derived(new Set(r?.winners.map((w) => w.nomination_id) ?? []));
	const isTie = $derived((r?.winners.length ?? 0) > 1);
	const hasWinner = $derived((r?.winners.length ?? 0) > 0);
</script>

<section class="space-y-6">
	{#if !r || !hasWinner}
		<div class="rounded-2xl bg-slate-900/70 p-8 text-center text-slate-400 ring-1 ring-white/10">
			No votes were cast.
		</div>
	{:else}
		<div class="text-center">
			<p class="text-sm font-semibold uppercase tracking-wide text-emerald-400">
				{isTie ? "It's a tie!" : 'The winner is'}
			</p>
			<div class="mt-4 flex flex-wrap justify-center gap-4">
				{#each r.winners as w (w.nomination_id)}
					{@const n = nomById.get(w.nomination_id)}
					<div class="w-36">
						{#if n}
							<div class="ring-2 ring-emerald-400 rounded-xl overflow-hidden shadow-lg shadow-emerald-500/20">
								<PosterImage itemId={n.item_id} tag={n.image_tag} title={n.title} />
							</div>
							<p class="mt-2 font-semibold">{n.title}</p>
							<p class="text-xs text-slate-500">{n.year || ''}</p>
						{:else}
							<p class="font-semibold">{w.title}</p>
						{/if}
					</div>
				{/each}
			</div>
		</div>

		<div class="rounded-2xl bg-slate-900/70 p-4 ring-1 ring-white/10">
			<h3 class="text-sm font-semibold text-slate-300">Full results</h3>
			<div class="mt-3 space-y-2.5">
				{#each r.ranked as e, i (e.nomination_id)}
					<div>
						<div class="flex items-baseline justify-between gap-2 text-sm">
							<span class="truncate {winnerIds.has(e.nomination_id) ? 'font-semibold text-emerald-300' : 'text-slate-200'}">
								{i + 1}. {e.title}
							</span>
							<span class="tabular-nums text-xs text-slate-500">{e.score}</span>
						</div>
						<div class="mt-1 h-2 overflow-hidden rounded-full bg-slate-800">
							<div
								class="h-full rounded-full {winnerIds.has(e.nomination_id) ? 'bg-emerald-400' : 'bg-slate-600'}"
								style="width: {(e.score / max) * 100}%"
							></div>
						</div>
					</div>
				{/each}
			</div>
			{#if r.method === 'ranked' && r.rounds && r.rounds.length > 1}
				<p class="mt-3 text-xs text-slate-500">
					Decided by instant-runoff over {r.rounds.length} rounds. Bars show each title's final-round support.
				</p>
			{/if}
		</div>
	{/if}
</section>
