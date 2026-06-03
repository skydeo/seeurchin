<script lang="ts">
	import { api } from '$lib/api';
	import type { PollView, LibraryItem, ExternalResult } from '$lib/types';
	import PosterImage from './PosterImage.svelte';

	let {
		poll,
		code,
		update
	}: { poll: PollView; code: string; update: (p: PollView) => void } = $props();

	const nominatedIds = $derived(new Set(poll.nominations.map((n) => n.item_id)));
	const mine = $derived(poll.nominations.filter((n) => n.mine_nominated));
	const isHost = $derived(poll.me?.is_host ?? false);
	const isRandom = $derived(poll.voting_method === 'random');
	const canWriteIn = $derived(poll.seerr_enabled && poll.allow_writeins);

	const guidance = $derived.by(() => {
		const r = poll.submission_rules;
		if (r.required > 0) return `Pick exactly ${r.required}`;
		if (r.min > 0 && r.max > 0) return `Pick ${r.min}–${r.max}`;
		if (r.max > 0) return `Pick up to ${r.max}`;
		if (r.min > 0) return `Pick at least ${r.min}`;
		return 'Pick as many as you like';
	});
	const atMax = $derived.by(() => {
		const max = poll.submission_rules.required || poll.submission_rules.max;
		return max > 0 && (poll.me?.nomination_count ?? 0) >= max;
	});

	let actionError = $state('');
	async function toggle(itemId: string) {
		actionError = '';
		try {
			if (nominatedIds.has(itemId)) {
				const nom = poll.nominations.find((n) => n.item_id === itemId);
				if (nom) update(await api.withdraw(code, nom.id));
			} else {
				update(await api.nominate(code, itemId));
			}
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'something went wrong';
		}
	}

	// --- browse modal ---
	let browseOpen = $state(false);
	let browseTab = $state<'library' | 'external'>('library');
	let query = $state('');
	let typeFilter = $state(''); // '', 'movie', 'series'
	let genre = $state(''); // '' = all genres
	let genres = $state<string[]>([]);
	let items = $state<LibraryItem[]>([]);
	let externalItems = $state<ExternalResult[]>([]);
	let searching = $state(false);
	let searchError = $state('');
	let timer: ReturnType<typeof setTimeout>;

	async function runSearch() {
		searching = true;
		searchError = '';
		try {
			items = (await api.library(code, query, typeFilter, genre)).items;
		} catch (err) {
			searchError = err instanceof Error ? err.message : 'search failed';
		} finally {
			searching = false;
		}
	}

	async function runExternalSearch() {
		if (query.trim().length < 2) {
			externalItems = [];
			searching = false;
			return;
		}
		searching = true;
		searchError = '';
		try {
			externalItems = (await api.searchExternal(code, query)).results;
		} catch (err) {
			searchError = err instanceof Error ? err.message : 'search failed';
		} finally {
			searching = false;
		}
	}

	$effect(() => {
		// Track deps; debounce while the modal is open.
		query;
		typeFilter;
		genre;
		browseTab;
		if (!browseOpen) return;
		clearTimeout(timer);
		timer = setTimeout(browseTab === 'external' ? runExternalSearch : runSearch, 250);
		return () => clearTimeout(timer);
	});

	// The "seerr:<type>:<tmdb>" surrogate key matches how the backend stores
	// write-ins, so we can tell which external results are already nominated.
	function writeInKey(r: ExternalResult) {
		return `seerr:${r.media_type}:${r.tmdb_id}`;
	}

	async function toggleExternal(r: ExternalResult) {
		actionError = '';
		const key = writeInKey(r);
		try {
			if (nominatedIds.has(key)) {
				const nom = poll.nominations.find((n) => n.item_id === key);
				if (nom) update(await api.withdraw(code, nom.id));
			} else {
				update(await api.nominateExternal(code, r.tmdb_id, r.media_type));
			}
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'something went wrong';
		}
	}

	// Genre chips filter the library tab. When the poll already restricts to a
	// set of genres the library is limited to those, so offer them directly;
	// otherwise list everything available for the poll's scope.
	async function loadGenres() {
		if (poll.genres.length > 0) {
			genres = poll.genres;
			return;
		}
		try {
			genres = (await api.genres(poll.library_scope)).genres;
		} catch {
			genres = [];
		}
	}

	function openBrowse() {
		browseTab = 'library';
		genre = '';
		browseOpen = true;
		loadGenres();
	}
</script>

<section>
	<div class="flex items-end justify-between gap-3">
		<div>
			<h2 class="font-title text-lg font-bold text-ink">Nominations</h2>
			<p class="mt-0.5 text-[13px] font-semibold text-muted">{guidance} · you've added {poll.me?.nomination_count ?? 0}</p>
			{#if poll.genres.length > 0}
				<p class="mt-0.5 text-xs font-bold text-accent">Limited to {poll.genres.join(', ')}</p>
			{/if}
		</div>
		<button onclick={openBrowse} class="btn btn-primary btn-sm shrink-0">＋ Add titles</button>
	</div>

	{#if actionError}<p class="mt-2 text-sm font-semibold text-coral-ink">{actionError}</p>{/if}

	{#if poll.nominations.length === 0}
		<button
			onclick={openBrowse}
			class="mt-6 block w-full rounded-[20px] border-2 border-dashed border-line2 bg-surface2 p-10 text-center text-sm font-semibold text-muted transition hover:border-accent hover:text-ink"
		>
			Nothing nominated yet. Tap <span class="font-bold text-ink">Add titles</span> to browse the library.
		</button>
	{:else}
		{#if (poll.me?.nomination_count ?? 0) === 0}
			<button
				onclick={openBrowse}
				class="mt-4 flex w-full items-center justify-between gap-3 rounded-[20px] border-[1.5px] border-accent bg-accent/10 p-3.5 text-left"
			>
				<span class="text-sm font-semibold text-ink">You haven't added anything yet — tap to pick titles.</span>
				<span class="btn btn-primary btn-sm pointer-events-none">＋ Add</span>
			</button>
		{/if}
		<h3 class="mt-6 font-title text-xs font-bold uppercase tracking-[0.12em] text-faint">
			Everyone's picks so far ({poll.nominations.length}){#if mine.length > 0} · {mine.length} yours{/if}
		</h3>
		<div class="mt-3 grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5">
			{#each poll.nominations as n (n.id)}
				<button onclick={() => toggle(n.item_id)} class="group min-w-0 text-left">
					<div class="relative">
						<PosterImage itemId={n.item_id} tag={n.image_tag} posterUrl={n.poster_url ?? ''} title={n.title} />
						{#if n.mine_nominated}
							<span class="badge badge-yours">YOURS</span>
						{/if}
						{#if n.nominator_count > 1}
							<span class="badge badge-count">×{n.nominator_count}</span>
						{/if}
						{#if n.source === 'seerr'}
							<span class="badge badge-req">REQUEST</span>
						{/if}
					</div>
					<p class="mt-1.5 truncate text-[13px] font-bold text-ink">{n.title}</p>
					<p class="text-[11px] font-semibold text-faint">
						{n.mine_nominated ? 'Tap to remove' : 'Tap to also nominate'}
					</p>
				</button>
			{/each}
		</div>
	{/if}

	{#if mine.length > 0 && poll.me}
		<p class="mt-4 text-sm font-semibold text-muted">Your picks: {mine.map((n) => n.title).join(', ')}</p>
	{/if}

	{#if isHost && !poll.timer}
		<div class="mt-8 rounded-[20px] border border-line bg-surface2 p-4">
			<p class="text-[13px] font-semibold text-muted">
				{#if isRandom}
					When everyone's done nominating, draw the winner. You need at least 2 nominations.
				{:else}
					When everyone's done nominating, start the vote. You need at least 2 nominations.
				{/if}
			</p>
			<button
				onclick={async () => update(await api.advance(code))}
				disabled={poll.nominations.length < 2}
				class="btn btn-coral mt-3 w-full"
			>
				{isRandom ? '🎲 Pick the winner' : 'Start voting →'}
			</button>
		</div>
	{/if}
</section>

{#if browseOpen}
	<div class="fixed inset-0 z-50 flex flex-col bg-bg">
		<div class="mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col p-4">
			{#if canWriteIn}
				<div class="mb-3 flex gap-2">
					{#each [['library', 'In your library'], ['external', 'Request something new']] as [val, label] (val)}
						<button onclick={() => (browseTab = val as typeof browseTab)} class="opt flex-1" class:is-on={browseTab === val}>{label}</button>
					{/each}
				</div>
			{/if}

			<div class="flex items-center gap-2">
				<input
					bind:value={query}
					placeholder={browseTab === 'external' ? 'Search for any movie or show…' : 'Search the library…'}
					autocomplete="off"
					class="input flex-1"
				/>
				<button onclick={() => (browseOpen = false)} class="btn btn-ghost">Done</button>
			</div>

			{#if poll.library_scope === 'both' && browseTab === 'library'}
				<div class="mt-3 flex gap-2">
					{#each [['', 'All'], ['movie', 'Movies'], ['series', 'Shows']] as [val, label] (val)}
						<button onclick={() => (typeFilter = val)} class="chip" class:is-on={typeFilter === val}>{label}</button>
					{/each}
				</div>
			{/if}

			{#if browseTab === 'library' && genres.length > 1}
				<div class="genre-scroll mt-3 flex gap-2 overflow-x-auto pb-1">
					<button onclick={() => (genre = '')} class="chip chip-genre shrink-0" class:is-on={genre === ''}>All genres</button>
					{#each genres as g (g)}
						<button onclick={() => (genre = g)} class="chip chip-genre shrink-0" class:is-on={genre === g}>{g}</button>
					{/each}
				</div>
			{/if}

			<div class="mt-4 min-h-0 flex-1 overflow-y-auto overscroll-contain">
				{#if searching}
					<p class="py-10 text-center font-semibold text-muted">Searching…</p>
				{:else if searchError}
					<p class="py-10 text-center font-semibold text-coral-ink">{searchError}</p>
				{:else if browseTab === 'external'}
					{#if query.trim().length < 2}
						<p class="py-10 text-center font-semibold text-muted">Type a title to search for something to request.</p>
					{:else if externalItems.length === 0}
						<p class="py-10 text-center font-semibold text-muted">No titles found.</p>
					{:else}
						<div class="grid grid-cols-3 gap-3 pb-6 sm:grid-cols-4 md:grid-cols-5">
							{#each externalItems as r (r.media_type + r.tmdb_id)}
								{@const key = writeInKey(r)}
								{@const picked = nominatedIds.has(key)}
								{@const blocked = r.in_library || (atMax && !picked)}
								<button
									onclick={() => !blocked && toggleExternal(r)}
									class="group min-w-0 text-left {blocked && !picked ? 'opacity-40' : ''}"
									disabled={blocked}
								>
									<div class="relative overflow-hidden rounded-[14px]">
										<PosterImage itemId={key} posterUrl={r.poster_url} title={r.title} />
										{#if picked}
											<div class="poster-pick poster-pick-on"><span class="tag">✓ Picked</span></div>
										{:else if r.in_library}
											<div class="poster-pick poster-pick-lib"><span class="tag">In library</span></div>
										{/if}
									</div>
									<p class="mt-1.5 truncate text-[13px] font-bold text-ink">{r.title}</p>
									<p class="text-[11px] font-semibold text-faint">{r.year || ''}</p>
								</button>
							{/each}
						</div>
					{/if}
				{:else if items.length === 0}
					<p class="py-10 text-center font-semibold text-muted">No titles found.</p>
				{:else}
					<div class="grid grid-cols-3 gap-3 pb-6 sm:grid-cols-4 md:grid-cols-5">
						{#each items as item (item.id)}
							{@const picked = nominatedIds.has(item.id)}
							{@const blocked = atMax && !picked}
							<button
								onclick={() => !blocked && toggle(item.id)}
								class="group min-w-0 text-left {blocked ? 'opacity-40' : ''}"
								disabled={blocked}
							>
								<div class="relative overflow-hidden rounded-[14px]">
									<PosterImage itemId={item.id} tag={item.image_tag} title={item.title} />
									{#if picked}
										<div class="poster-pick poster-pick-on"><span class="tag">✓ Picked</span></div>
									{/if}
								</div>
								<p class="mt-1.5 truncate text-[13px] font-bold text-ink">{item.title}</p>
								<p class="text-[11px] font-semibold text-faint">{item.year || ''}</p>
							</button>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
