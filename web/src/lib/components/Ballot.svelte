<script lang="ts">
	import { api } from '$lib/api';
	import type { PollView } from '$lib/types';
	import PosterImage from './PosterImage.svelte';
	import LiveResults from './LiveResults.svelte';

	let {
		poll,
		code,
		update
	}: { poll: PollView; code: string; update: (p: PollView) => void } = $props();

	const method = poll.voting_method;
	const cfg = poll.voting_config as Record<string, number | boolean>;
	const allowSelf = cfg.allow_self_vote !== false;
	const noms = $derived(poll.nominations);
	const isHost = $derived(poll.me?.is_host ?? false);

	// approval / score selections; initialized once from any existing ballot.
	let selections = $state<Record<string, number>>({ ...(poll.me?.my_selections ?? {}) });
	// ranked uses an ordered list as the source of truth.
	let ranking = $state<string[]>(
		Object.entries(poll.me?.my_selections ?? {})
			.sort((a, b) => a[1] - b[1])
			.map(([id]) => id)
	);

	let busy = $state(false);
	let error = $state('');

	// --- approval ---
	const votesPerUser = Number(cfg.votes_per_user ?? 3);
	const maxPer = Number(cfg.max_votes_per_option ?? 1);
	const used = $derived(Object.values(selections).reduce((a, b) => a + (b > 0 ? b : 0), 0));
	const remaining = $derived(votesPerUser - used);

	function selfBlocked(mine: boolean) {
		return !allowSelf && mine;
	}
	function setApproval(id: string, v: number) {
		if (v <= 0) delete selections[id];
		else selections[id] = v;
	}
	function bumpApproval(id: string, delta: number, mine: boolean) {
		if (selfBlocked(mine)) return;
		const cur = selections[id] ?? 0;
		let next = cur + delta;
		if (next < 0) next = 0;
		if (maxPer > 0 && next > maxPer) next = maxPer;
		if (delta > 0 && remaining <= 0) return;
		setApproval(id, next);
	}

	// --- score ---
	const maxScore = Number(cfg.max_score ?? 5);
	function setScore(id: string, s: number, mine: boolean) {
		if (selfBlocked(mine)) return;
		if ((selections[id] ?? 0) === s) delete selections[id];
		else selections[id] = s;
	}

	// --- ranked ---
	const maxRanked = Number(cfg.max_ranked ?? 0);
	const unranked = $derived(
		noms.filter((n) => !ranking.includes(n.id) && !selfBlocked(n.mine_nominated))
	);
	function addRank(id: string) {
		if (maxRanked > 0 && ranking.length >= maxRanked) return;
		ranking = [...ranking, id];
	}
	function removeRank(id: string) {
		ranking = ranking.filter((x) => x !== id);
	}
	function moveRank(i: number, dir: number) {
		const j = i + dir;
		if (j < 0 || j >= ranking.length) return;
		const next = [...ranking];
		[next[i], next[j]] = [next[j], next[i]];
		ranking = next;
	}
	function titleOf(id: string) {
		return noms.find((n) => n.id === id)?.title ?? '';
	}

	async function submit() {
		busy = true;
		error = '';
		try {
			const sel: Record<string, number> = {};
			if (method === 'ranked') {
				ranking.forEach((id, i) => (sel[id] = i + 1));
			} else {
				for (const [id, v] of Object.entries(selections)) if (v > 0) sel[id] = v;
			}
			update(await api.vote(code, sel));
		} catch (err) {
			error = err instanceof Error ? err.message : 'could not submit vote';
		} finally {
			busy = false;
		}
	}
</script>

<section class="space-y-5">
	<div class="flex items-start justify-between gap-3">
		<div>
			<h2 class="font-title text-lg font-bold text-ink">Cast your vote</h2>
			{#if poll.me?.has_voted}
				<p class="mt-0.5 text-[13px] font-bold text-accent-ink">✓ You've voted — change it anytime before it closes.</p>
			{:else if method === 'approval'}
				<p class="mt-0.5 text-[13px] font-semibold text-muted">
					{maxPer === 1 ? `Choose up to ${votesPerUser}` : `You have ${votesPerUser} votes`} ·
					<span class="font-extrabold" class:text-mango={remaining === 0}>{remaining} left</span>
				</p>
			{:else if method === 'ranked'}
				<p class="mt-0.5 text-[13px] font-semibold text-muted">Tap titles to rank them, best first.</p>
			{:else}
				<p class="mt-0.5 text-[13px] font-semibold text-muted">Rate each title up to {maxScore}.</p>
			{/if}
		</div>
	</div>

	{#if error}<p class="text-sm font-semibold text-coral-ink">{error}</p>{/if}

	{#if method === 'ranked'}
		<!-- Ranked-choice: ordered picks then the rest. -->
		{#if ranking.length > 0}
			<ol class="space-y-2">
				{#each ranking as id, i (id)}
					<li class="flex items-center gap-3 rounded-[14px] border border-line bg-surface p-2 shadow-sm">
						<span class="grid h-7 w-7 shrink-0 place-items-center rounded-full bg-primary text-sm font-bold text-on-primary">{i + 1}</span>
						<span class="flex-1 truncate text-sm font-bold text-ink">{titleOf(id)}</span>
						<div class="flex items-center gap-1">
							<button onclick={() => moveRank(i, -1)} disabled={i === 0} class="grid h-[30px] w-[30px] place-items-center rounded-lg border-[1.5px] border-line bg-surface3 font-bold text-muted disabled:opacity-30">↑</button>
							<button onclick={() => moveRank(i, 1)} disabled={i === ranking.length - 1} class="grid h-[30px] w-[30px] place-items-center rounded-lg border-[1.5px] border-line bg-surface3 font-bold text-muted disabled:opacity-30">↓</button>
							<button onclick={() => removeRank(id)} class="grid h-[30px] w-[30px] place-items-center rounded-lg border-[1.5px] border-line bg-surface3 font-bold text-coral-ink">✕</button>
						</div>
					</li>
				{/each}
			</ol>
		{/if}
		{#if maxRanked > 0}
			<p class="text-xs font-semibold text-faint">Rank up to {maxRanked}.</p>
		{/if}
		{#if unranked.length > 0}
			<div>
				<h3 class="mb-2 font-title text-xs font-bold uppercase tracking-[0.12em] text-faint">Tap to add</h3>
				<div class="grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5">
					{#each unranked as n (n.id)}
						<button onclick={() => addRank(n.id)} class="min-w-0 text-left">
							<PosterImage itemId={n.item_id} tag={n.image_tag} title={n.title} />
							<p class="mt-1.5 truncate text-[13px] font-bold text-ink">{n.title}</p>
						</button>
					{/each}
				</div>
			</div>
		{/if}
	{:else}
		<!-- approval / score: one row per nomination. -->
		<div class="space-y-3">
			{#each noms as n (n.id)}
				{@const mine = n.mine_nominated}
				{@const blocked = selfBlocked(mine)}
				<div class="flex items-center gap-3 rounded-[14px] border border-line bg-surface p-2.5 shadow-sm {blocked ? 'opacity-50' : ''}">
					<div class="h-16 w-11 shrink-0 overflow-hidden rounded-md">
						<PosterImage itemId={n.item_id} tag={n.image_tag} title={n.title} />
					</div>
					<div class="min-w-0 flex-1">
						<p class="truncate text-sm font-bold text-ink">{n.title}</p>
						<p class="text-xs font-semibold text-faint">
							{n.year || ''}{mine ? ' · your nomination' : ''}
						</p>
					</div>

					{#if blocked}
						<span class="text-xs font-semibold text-faint">can't self-vote</span>
					{:else if method === 'approval'}
						{#if maxPer === 1}
							<button
								onclick={() => bumpApproval(n.id, (selections[n.id] ?? 0) > 0 ? -1 : 1, mine)}
								class="rounded-[10px] border-[1.5px] px-3.5 py-2.5 text-[13px] font-extrabold transition {(selections[n.id] ?? 0) > 0 ? 'border-accent bg-accent text-white' : 'border-line bg-surface3 text-muted'}"
							>
								{(selections[n.id] ?? 0) > 0 ? '✓ Picked' : 'Pick'}
							</button>
						{:else}
							<div class="flex items-center gap-2">
								<button onclick={() => bumpApproval(n.id, -1, mine)} class="grid h-8 w-8 place-items-center rounded-lg border-[1.5px] border-line bg-surface3 text-lg leading-none font-bold text-ink">−</button>
								<span class="w-5 text-center text-sm font-extrabold text-ink">{selections[n.id] ?? 0}</span>
								<button onclick={() => bumpApproval(n.id, 1, mine)} class="grid h-8 w-8 place-items-center rounded-lg border-[1.5px] border-line bg-surface3 text-lg leading-none font-bold text-ink">+</button>
							</div>
						{/if}
					{:else}
						<!-- score: stars -->
						<div class="flex items-center gap-0.5">
							{#each Array(maxScore) as _, i (i)}
								<button onclick={() => setScore(n.id, i + 1, mine)} class="p-0.5 text-[22px] leading-none {(selections[n.id] ?? 0) >= i + 1 ? 'text-mango' : 'text-line2'}">★</button>
							{/each}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}

	<button onclick={submit} disabled={busy} class="btn btn-primary w-full">
		{busy ? 'Saving…' : poll.me?.has_voted ? 'Update my vote' : 'Submit vote'}
	</button>

	{#if poll.results_live && poll.results}
		<LiveResults {poll} />
	{/if}

	{#if isHost}
		<div class="rounded-[20px] border border-line bg-surface2 p-4">
			<p class="text-[13px] font-semibold text-muted">{poll.voter_count} of {poll.participant_count} have voted.</p>
			<button onclick={async () => update(await api.advance(code))} class="btn btn-coral mt-3 w-full">
				Reveal results & close →
			</button>
		</div>
	{/if}
</section>
