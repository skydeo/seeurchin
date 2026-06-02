<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import type { VotingMethod, CreatePollBody } from '$lib/types';
	import UrchinMark from '$lib/components/UrchinMark.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';

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
	let seerrEnabled = $state(false);
	let allowWriteins = $state(true);
	let autoRequestWinner = $state(true);
	let creating = $state(false);
	let error = $state('');

	onMount(async () => {
		try {
			methods = await api.methods();
			if (methods.length) selectMethod(method);
		} catch (e) {
			error = e instanceof Error ? e.message : 'could not load voting methods';
		}
		try {
			seerrEnabled = (await api.features()).seerr;
		} catch {
			seerrEnabled = false;
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
			genres: selectedGenres,
			allow_writeins: seerrEnabled && allowWriteins,
			auto_request_winner: seerrEnabled && allowWriteins && autoRequestWinner
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

	const scopeOpts: [string, string][] = [
		['both', 'Movies & Shows'],
		['movie', 'Movies'],
		['series', 'Shows']
	];
	const ruleOpts: [string, string][] = [
		['open', 'Any'],
		['range', 'A range'],
		['exact', 'Exactly']
	];
</script>

<svelte:head><title>seeurchin — group movie night picker</title></svelte:head>

<main class="mx-auto max-w-2xl px-4 py-8">
	<div class="relative">
		<div class="absolute right-0 top-0"><ThemeToggle /></div>
		<div class="pt-1 text-center">
			<div class="flex items-center justify-center gap-2.5">
				<UrchinMark size={40} />
				<span class="font-display text-3xl font-semibold tracking-tight text-ink">seeurchin</span>
			</div>
			<p class="mt-3 text-[15px] font-semibold text-muted text-pretty">
				Pick what to watch, together.<br />Nominate, then vote — no accounts needed.
			</p>
		</div>
	</div>

	<!-- Join -->
	<form onsubmit={join} class="mx-auto mt-7 flex max-w-sm gap-2">
		<input
			bind:value={joinCode}
			placeholder="POLL CODE"
			maxlength="6"
			autocomplete="off"
			class="input input-code flex-1"
		/>
		<button type="submit" class="btn btn-ghost px-5">Join</button>
	</form>

	<div class="my-7 flex items-center gap-3.5 text-[11px] font-bold uppercase tracking-[0.12em] text-faint font-title">
		<div class="h-px flex-1 bg-line2"></div>
		or start a new poll
		<div class="h-px flex-1 bg-line2"></div>
	</div>

	<!-- Create -->
	<form onsubmit={create} class="card space-y-5 p-5 sm:p-6">
		<div class="grid gap-4 sm:grid-cols-2">
			<label class="block">
				<span class="mb-1.5 block text-sm font-bold text-muted">Poll name</span>
				<input bind:value={title} required placeholder="Friday movie night" class="input" />
			</label>
			<label class="block">
				<span class="mb-1.5 block text-sm font-bold text-muted">Your name</span>
				<input bind:value={hostName} required placeholder="Alex" class="input" />
			</label>
		</div>

		<div>
			<span class="mb-1.5 block text-sm font-bold text-muted">What can people pick?</span>
			<div class="flex gap-2">
				{#each scopeOpts as [val, label] (val)}
					<button type="button" onclick={() => (scope = val)} class="opt flex-1" class:is-on={scope === val}>{label}</button>
				{/each}
			</div>
		</div>

		<!-- Genre restriction (optional) -->
		{#if allGenres.length > 0}
			<div>
				<span class="mb-1.5 block text-sm font-bold text-muted">Limit to genres <span class="text-faint">(optional)</span></span>
				<div class="flex flex-wrap gap-2">
					{#each allGenres as g (g)}
						<button type="button" onclick={() => toggleGenre(g)} class="chip" class:is-on={selectedGenres.includes(g)}>{g}</button>
					{/each}
				</div>
				{#if selectedGenres.length > 0}
					<p class="mt-2 text-xs font-semibold text-faint">Only {selectedGenres.join(', ')} can be nominated.</p>
				{/if}
			</div>
		{:else if genreError}
			<p class="text-xs font-semibold text-faint">Genres unavailable ({genreError}).</p>
		{/if}

		<!-- Submission rules -->
		<div>
			<span class="mb-1.5 block text-sm font-bold text-muted">How many can each person nominate?</span>
			<div class="flex gap-2">
				{#each ruleOpts as [val, label] (val)}
					<button type="button" onclick={() => (ruleMode = val as typeof ruleMode)} class="opt flex-1" class:is-on={ruleMode === val}>{label}</button>
				{/each}
			</div>
			{#if ruleMode === 'range'}
				<div class="mt-2.5 flex items-center gap-2 text-sm font-bold text-muted">
					<span>min</span>
					<input type="number" min="0" bind:value={ruleMin} class="input w-16 px-2 py-1.5 text-center" />
					<span>max</span>
					<input type="number" min="0" bind:value={ruleMax} class="input w-16 px-2 py-1.5 text-center" />
				</div>
			{:else if ruleMode === 'exact'}
				<div class="mt-2.5 flex items-center gap-2 text-sm font-bold text-muted">
					<span>exactly</span>
					<input type="number" min="1" bind:value={ruleRequired} class="input w-16 px-2 py-1.5 text-center" />
					<span>each</span>
				</div>
			{/if}
		</div>

		<!-- Voting method -->
		<div>
			<span class="mb-1.5 block text-sm font-bold text-muted">Voting method</span>
			<div class="grid grid-cols-2 gap-2">
				{#each methods as m (m.key)}
					<button type="button" onclick={() => selectMethod(m.key)} class="opt" class:is-on={method === m.key}>{m.label}</button>
				{/each}
			</div>

			<!-- Method-specific options -->
			<div class="panel mt-2.5 space-y-3 p-3 text-sm">
				{#if method === 'approval'}
					<label class="flex items-center justify-between gap-3">
						<span class="font-semibold text-ink">Votes per person</span>
						<input type="number" min="1" value={num(config.votes_per_user)} oninput={(e) => (config.votes_per_user = num(e.currentTarget.value))} class="input w-20 px-2 py-1.5 text-center" />
					</label>
					<label class="flex items-center justify-between gap-3">
						<span class="font-semibold text-ink">Max votes on one title <span class="text-faint">(0 = no limit)</span></span>
						<input type="number" min="0" value={num(config.max_votes_per_option)} oninput={(e) => (config.max_votes_per_option = num(e.currentTarget.value))} class="input w-20 px-2 py-1.5 text-center" />
					</label>
				{:else if method === 'ranked'}
					<label class="flex items-center justify-between gap-3">
						<span class="font-semibold text-ink">How many to rank <span class="text-faint">(0 = all)</span></span>
						<input type="number" min="0" value={num(config.max_ranked)} oninput={(e) => (config.max_ranked = num(e.currentTarget.value))} class="input w-20 px-2 py-1.5 text-center" />
					</label>
				{:else if method === 'score'}
					<label class="flex items-center justify-between gap-3">
						<span class="font-semibold text-ink">Max rating</span>
						<input type="number" min="2" value={num(config.max_score)} oninput={(e) => (config.max_score = num(e.currentTarget.value))} class="input w-20 px-2 py-1.5 text-center" />
					</label>
					<label class="flex items-center justify-between gap-3">
						<span class="font-semibold text-ink">Winner by</span>
						<select value={config.aggregate ?? 'total'} onchange={(e) => (config.aggregate = e.currentTarget.value)} class="select">
							<option value="total">Highest total</option>
							<option value="average">Highest average</option>
						</select>
					</label>
				{/if}
				{#if method === 'random'}
					<p class="font-semibold text-muted">A random nomination is drawn as the winner — there's no voting round.</p>
				{:else}
					<label class="flex items-center justify-between gap-3">
						<span class="font-semibold text-ink">Voting for your own picks</span>
						<select value={selfVoteMode} onchange={(e) => setSelfVote(e.currentTarget.value)} class="select">
							<option value="unlimited">Allowed</option>
							<option value="none">Not allowed</option>
							<option value="limited">Limited…</option>
						</select>
					</label>
					{#if selfVoteMode === 'limited'}
						<label class="flex items-center justify-between gap-3">
							<span class="font-semibold text-ink">Most you can give your own picks</span>
							<input type="number" min="1" value={num(config.max_self_votes)} oninput={(e) => (config.max_self_votes = Math.max(1, num(e.currentTarget.value)))} class="input w-20 px-2 py-1.5 text-center" />
						</label>
					{/if}
				{/if}
			</div>
		</div>

		<div class="space-y-1">
			<button type="button" onclick={() => (allowGuests = !allowGuests)} class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm">
				<span class="font-semibold text-ink">Allow guests <span class="text-faint">(no account needed)</span></span>
				<span class="switch" role="switch" aria-checked={allowGuests}></span>
			</button>
			{#if method !== 'random'}
				<button type="button" onclick={() => (resultsLive = !resultsLive)} class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm">
					<span class="font-semibold text-ink">Show live results during voting</span>
					<span class="switch" role="switch" aria-checked={resultsLive}></span>
				</button>
			{/if}
			<button type="button" onclick={() => (revealNominators = !revealNominators)} class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm">
				<span class="font-semibold text-ink">Reveal who nominated, on results</span>
				<span class="switch" role="switch" aria-checked={revealNominators}></span>
			</button>
			{#if revealNominators}
				<label class="flex items-center justify-between gap-3 pl-1 py-1 text-sm">
					<span class="font-semibold text-muted">Show nominators for</span>
					<select bind:value={revealScope} class="select">
						<option value="winner">The winner only</option>
						<option value="all">Every title</option>
					</select>
				</label>
			{/if}
			{#if seerrEnabled}
				<button type="button" onclick={() => (allowWriteins = !allowWriteins)} class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm">
					<span class="font-semibold text-ink">Allow titles not in your library <span class="text-faint">(via Seerr)</span></span>
					<span class="switch" role="switch" aria-checked={allowWriteins}></span>
				</button>
				{#if allowWriteins}
					<button type="button" onclick={() => (autoRequestWinner = !autoRequestWinner)} class="flex w-full items-center justify-between gap-3 pl-1 py-1 text-left text-sm">
						<span class="font-semibold text-muted">Auto-request the winner if it's a write-in</span>
						<span class="switch" role="switch" aria-checked={autoRequestWinner}></span>
					</button>
				{/if}
			{/if}
		</div>

		{#if error}<p class="text-sm font-semibold text-coral-ink">{error}</p>{/if}

		<button type="submit" disabled={creating} class="btn btn-primary w-full">
			{creating ? 'Creating…' : 'Create poll'}
		</button>
	</form>

	<p class="mt-6 text-center text-xs font-semibold text-faint">self-hosted movie voting for your Jellyfin library</p>
</main>
