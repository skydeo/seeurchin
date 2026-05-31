<script lang="ts">
	import type { PollView } from '$lib/types';

	let { poll }: { poll: PollView } = $props();

	const r = $derived(poll.results);
	const max = $derived(Math.max(1, ...(r?.ranked.map((x) => x.score) ?? [1])));
	const winners = $derived(new Set(r?.winners.map((w) => w.nomination_id) ?? []));
</script>

{#if r}
	<div class="rounded-2xl bg-slate-900/70 p-4 ring-1 ring-white/10">
		<h3 class="text-sm font-semibold text-slate-300">Live tally</h3>
		<div class="mt-3 space-y-2">
			{#each r.ranked as e (e.nomination_id)}
				<div>
					<div class="flex items-baseline justify-between gap-2 text-xs">
						<span class="truncate {winners.has(e.nomination_id) ? 'font-semibold text-brand-300' : 'text-slate-300'}">{e.title}</span>
						<span class="tabular-nums text-slate-500">{e.score}</span>
					</div>
					<div class="mt-1 h-2 overflow-hidden rounded-full bg-slate-800">
						<div
							class="h-full rounded-full {winners.has(e.nomination_id) ? 'bg-brand-400' : 'bg-slate-600'} transition-all"
							style="width: {(e.score / max) * 100}%"
						></div>
					</div>
				</div>
			{/each}
		</div>
	</div>
{/if}
