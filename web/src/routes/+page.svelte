<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import type { VotingMethod, CreatePollBody } from '$lib/types';

	// --- join by code ---
	let joinCode = $state('');
	function join(e: Event) {
		e.preventDefault();
		const c = joinCode.trim();
		if (c) goto(`/p/${encodeURIComponent(c)}`);
	}

	// --- create a poll ---
	let methods = $state<VotingMethod[]>([]);
	let title = $state('');
	let hostName = $state('');
	let scope = $state('both');
	let method = $state('approval');
	let config = $state<Record<string, number | boolean | string>>({});
	let ruleMode = $state<'open' | 'range' | 'exact'>('open');
	let ruleMin = $state(1);
	let ruleMax = $state(3);
	let ruleRequired = $state(2);
	let allowGuests = $state(true);
	let resultsLive = $state(false);
	let revealNominators = $state(false);
	let revealScope = $state('winner');
	let allGenres = $state<string[]>([]);
	let selectedGenres = $state<string[]>([]);
	let genreError = $state('');
	let creating = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			methods = await api.methods();
			if (methods.length) selectMethod(method);
		} catch (e) {
			error = e instanceof Error ? e.message : 'could not load voting methods';
		}
	});

	function selectMethod(key: string) {
		method = key;
		const m = methods.find((x) => x.key === key);
		config = { ...(m?.default_config ?? {}) } as Record<string, number | boolean | string>;
	}

	// Load the genre list for the chosen scope. Changing scope resets the
	// selection, since movie and show genres differ.
	$effect(() => {
		const s = scope;
		let cancelled = false;
		genreError = '';
		selectedGenres = [];
		api
			.genres(s)
			.then((res) => {
				if (!cancelled) allGenres = res.genres;
			})
			.catch((err) => {
				if (!cancelled) genreError = err instanceof Error ? err.message : 'could not load genres';
			});
		return () => {
			cancelled = true;
		};
	});
	function toggleGenre(g: string) {
		selectedGenres = selectedGenres.includes(g)
			? selectedGenres.filter((x) => x !== g)
			: [...selectedGenres, g];
	}

	async function create(e: Event) {
		e.preventDefault();
		if (creating) return;
		error = '';
		const rules =
			ruleMode === 'exact'
				? { min: 0, max: 0, required: Math.max(1, ruleRequired) }
				: ruleMode === 'range'
					? { min: Math.max(0, ruleMin), max: Math.max(0, ruleMax), required: 0 }
					: { min: 0, max: 0, required: 0 };
		const body: CreatePollBody = {
			title: title.trim(),
			host_name: hostName.trim(),
			library_scope: scope,
			voting_method: method,
			voting_config: config,
			submission_rules: rules,
			allow_guests: allowGuests,
			results_live: resultsLive,
			reveal_nominators: revealNominators,
			reveal_scope: revealScope,
			genres: selectedGenres
		};
		creating = true;
		try {
			const poll = await api.createPoll(body);
			goto(`/p/${poll.code}`);
		} catch (err) {
			error = err instanceof Error ? err.message : 'could not create poll';
			creating = false;
		}
	}

	const num = (v: unknown) => Number(v ?? 0);

	// Self-vote control maps to the method config's max_self_votes:
	//   unlimited -> -1, none -> 0, limited -> N. Falls back to the legacy
	//   allow_self_vote bool when max_self_votes is unset.
	const selfVoteMode = $derived.by(() => {
		const m = config.max_self_votes;
		if (m === undefined || m === null) return config.allow_self_vote === false ? 'none' : 'unlimited';
		const n = Number(m);
		if (n < 0) return 'unlimited';
		if (n === 0) return 'none';
		return 'limited';
	});
	function setSelfVote(mode: string) {
		if (mode === 'unlimited') config.max_self_votes = -1;
		else if (mode === 'none') config.max_self_votes = 0;
		else config.max_self_votes = Math.max(1, num(config.max_self_votes) || 1);
	}
</script>

<svelte:head><title>seeurchin — group movie night picker</title></svelte:head>

<main class="mx-auto max-w-2xl px-4 py-10">
	<div class="text-center">
		<h1 class="text-4xl font-bold tracking-tight">🌊🦔 seeurchin</h1>
		<p class="mt-2 text-slate-400">Pick what to watch, together. Nominate, then vote.</p>
	</div>

	<!-- Join -->
	<form onsubmit={join} class="mx-auto mt-8 flex max-w-sm gap-2">
		<input
			bind:value={joinCode}
			placeholder="Enter a poll code"
			autocomplete="off"
			class="w-full rounded-xl bg-slate-800 px-4 py-3 text-center text-lg uppercase tracking-widest ring-1 ring-white/10 outline-none focus:ring-2 focus:ring-brand-500"
		/>
		<button type="submit" class="rounded-xl bg-slate-700 px-5 py-3 font-semibold hover:bg-slate-600">Join</button>
	</form>

	<div class="my-8 flex items-center gap-4 text-xs uppercase tracking-wide text-slate-600">
		<div class="h-px flex-1 bg-white/10"></div>
		or start a new poll
		<div class="h-px flex-1 bg-white/10"></div>
	</div>

	<!-- Create -->
	<form onsubmit={create} class="space-y-5 rounded-2xl bg-slate-900/70 p-5 ring-1 ring-white/10 sm:p-6">
		<div class="grid gap-4 sm:grid-cols-2">
			<label class="block">
				<span class="text-sm text-slate-300">Poll name</span>
				<input bind:value={title} required placeholder="Friday movie night" class="mt-1 w-full rounded-xl bg-slate-800 px-3 py-2.5 ring-1 ring-white/10 outline-none focus:ring-2 focus:ring-brand-500" />
			</label>
			<label class="block">
				<span class="text-sm text-slate-300">Your name</span>
				<input bind:value={hostName} required placeholder="Alex" class="mt-1 w-full rounded-xl bg-slate-800 px-3 py-2.5 ring-1 ring-white/10 outline-none focus:ring-2 focus:ring-brand-500" />
			</label>
		</div>

		<div>
			<span class="text-sm text-slate-300">What can people pick?</span>
			<div class="mt-1 flex gap-2">
				{#each [ ['both', 'Movies & Shows'], ['movie', 'Movies'], ['series', 'Shows'] ] as [val, label] (val)}
					<button type="button" onclick={() => (scope = val)} class="flex-1 rounded-xl px-3 py-2.5 text-sm font-medium {scope === val ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-300'}">{label}</button>
				{/each}
			</div>
		</div>

		<!-- Genre restriction (optional) -->
		{#if allGenres.length > 0}
			<div>
				<span class="text-sm text-slate-300">Limit to genres <span class="text-slate-500">(optional)</span></span>
				<div class="mt-1 flex flex-wrap gap-2">
					{#each allGenres as g (g)}
						<button type="button" onclick={() => toggleGenre(g)} class="rounded-full px-3 py-1 text-sm {selectedGenres.includes(g) ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-300'}">{g}</button>
					{/each}
				</div>
				{#if selectedGenres.length > 0}
					<p class="mt-1 text-xs text-slate-500">Only {selectedGenres.join(', ')} can be nominated.</p>
				{/if}
			</div>
		{:else if genreError}
			<p class="text-xs text-slate-500">Genres unavailable ({genreError}).</p>
		{/if}

		<!-- Submission rules -->
		<div>
			<span class="text-sm text-slate-300">How many can each person nominate?</span>
			<div class="mt-1 flex gap-2">
				{#each [ ['open', 'Any'], ['range', 'A range'], ['exact', 'Exactly'] ] as [val, label] (val)}
					<button type="button" onclick={() => (ruleMode = val as typeof ruleMode)} class="flex-1 rounded-xl px-3 py-2.5 text-sm font-medium {ruleMode === val ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-300'}">{label}</button>
				{/each}
			</div>
			{#if ruleMode === 'range'}
				<div class="mt-2 flex items-center gap-2 text-sm text-slate-400">
					<span>min</span>
					<input type="number" min="0" bind:value={ruleMin} class="w-16 rounded-lg bg-slate-800 px-2 py-1.5 text-center ring-1 ring-white/10" />
					<span>max</span>
					<input type="number" min="0" bind:value={ruleMax} class="w-16 rounded-lg bg-slate-800 px-2 py-1.5 text-center ring-1 ring-white/10" />
				</div>
			{:else if ruleMode === 'exact'}
				<div class="mt-2 flex items-center gap-2 text-sm text-slate-400">
					<span>exactly</span>
					<input type="number" min="1" bind:value={ruleRequired} class="w-16 rounded-lg bg-slate-800 px-2 py-1.5 text-center ring-1 ring-white/10" />
					<span>each</span>
				</div>
			{/if}
		</div>

		<!-- Voting method -->
		<div>
			<span class="text-sm text-slate-300">Voting method</span>
			<div class="mt-1 grid grid-cols-1 gap-2 sm:grid-cols-3">
				{#each methods as m (m.key)}
					<button type="button" onclick={() => selectMethod(m.key)} class="rounded-xl px-3 py-2.5 text-sm font-medium {method === m.key ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-300'}">{m.label}</button>
				{/each}
			</div>

			<!-- Method-specific options -->
			<div class="mt-3 space-y-3 rounded-xl bg-slate-800/60 p-3 text-sm">
				{#if method === 'approval'}
					<label class="flex items-center justify-between gap-3">
						<span class="text-slate-300">Votes per person</span>
						<input type="number" min="1" value={num(config.votes_per_user)} oninput={(e) => (config.votes_per_user = num(e.currentTarget.value))} class="w-20 rounded-lg bg-slate-900 px-2 py-1.5 text-center ring-1 ring-white/10" />
					</label>
					<label class="flex items-center justify-between gap-3">
						<span class="text-slate-300">Max votes on one title <span class="text-slate-500">(0 = no limit)</span></span>
						<input type="number" min="0" value={num(config.max_votes_per_option)} oninput={(e) => (config.max_votes_per_option = num(e.currentTarget.value))} class="w-20 rounded-lg bg-slate-900 px-2 py-1.5 text-center ring-1 ring-white/10" />
					</label>
				{:else if method === 'ranked'}
					<label class="flex items-center justify-between gap-3">
						<span class="text-slate-300">How many to rank <span class="text-slate-500">(0 = all)</span></span>
						<input type="number" min="0" value={num(config.max_ranked)} oninput={(e) => (config.max_ranked = num(e.currentTarget.value))} class="w-20 rounded-lg bg-slate-900 px-2 py-1.5 text-center ring-1 ring-white/10" />
					</label>
				{:else if method === 'score'}
					<label class="flex items-center justify-between gap-3">
						<span class="text-slate-300">Max rating</span>
						<input type="number" min="2" value={num(config.max_score)} oninput={(e) => (config.max_score = num(e.currentTarget.value))} class="w-20 rounded-lg bg-slate-900 px-2 py-1.5 text-center ring-1 ring-white/10" />
					</label>
					<label class="flex items-center justify-between gap-3">
						<span class="text-slate-300">Winner by</span>
						<select value={config.aggregate ?? 'total'} onchange={(e) => (config.aggregate = e.currentTarget.value)} class="rounded-lg bg-slate-900 px-2 py-1.5 ring-1 ring-white/10">
							<option value="total">Highest total</option>
							<option value="average">Highest average</option>
						</select>
					</label>
				{/if}
				{#if method === 'random'}
					<p class="text-slate-400">A random nomination is drawn as the winner — there's no voting round.</p>
				{:else}
					<label class="flex items-center justify-between gap-3">
						<span class="text-slate-300">Voting for your own picks</span>
						<select value={selfVoteMode} onchange={(e) => setSelfVote(e.currentTarget.value)} class="rounded-lg bg-slate-900 px-2 py-1.5 ring-1 ring-white/10">
							<option value="unlimited">Allowed</option>
							<option value="none">Not allowed</option>
							<option value="limited">Limited…</option>
						</select>
					</label>
					{#if selfVoteMode === 'limited'}
						<label class="flex items-center justify-between gap-3">
							<span class="text-slate-300">Most you can give your own picks</span>
							<input type="number" min="1" value={num(config.max_self_votes)} oninput={(e) => (config.max_self_votes = Math.max(1, num(e.currentTarget.value)))} class="w-20 rounded-lg bg-slate-900 px-2 py-1.5 text-center ring-1 ring-white/10" />
						</label>
					{/if}
				{/if}
			</div>
		</div>

		<div class="space-y-2">
			<label class="flex items-center justify-between gap-3 text-sm">
				<span class="text-slate-300">Allow guests (no account needed)</span>
				<input type="checkbox" bind:checked={allowGuests} class="h-5 w-5 accent-brand-500" />
			</label>
			{#if method !== 'random'}
				<label class="flex items-center justify-between gap-3 text-sm">
					<span class="text-slate-300">Show live results during voting</span>
					<input type="checkbox" bind:checked={resultsLive} class="h-5 w-5 accent-brand-500" />
				</label>
			{/if}
			<label class="flex items-center justify-between gap-3 text-sm">
				<span class="text-slate-300">Reveal who nominated, on the results screen</span>
				<input type="checkbox" bind:checked={revealNominators} class="h-5 w-5 accent-brand-500" />
			</label>
			{#if revealNominators}
				<label class="flex items-center justify-between gap-3 pl-1 text-sm">
					<span class="text-slate-400">Show nominators for</span>
					<select bind:value={revealScope} class="rounded-lg bg-slate-800 px-2 py-1.5 ring-1 ring-white/10">
						<option value="winner">The winner only</option>
						<option value="all">Every title</option>
					</select>
				</label>
			{/if}
		</div>

		{#if error}<p class="text-sm text-rose-400">{error}</p>{/if}

		<button type="submit" disabled={creating} class="w-full rounded-xl bg-brand-500 px-4 py-3 font-semibold text-white hover:bg-brand-600 disabled:opacity-40">
			{creating ? 'Creating…' : 'Create poll'}
		</button>
	</form>
</main>
