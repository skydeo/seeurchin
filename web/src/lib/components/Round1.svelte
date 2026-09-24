<script lang="ts">
	import { api } from '$lib/api';
	import type { PollView, LibraryItem, ExternalResult } from '$lib/types';
	import PosterImage from './PosterImage.svelte';
	import { lockScroll } from '$lib/scrollLock';

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

	// Lock the page behind the full-screen browser so iOS can't scroll it
	// (that scroll-through is what made the whole page "slip").
	$effect(() => {
		if (browseOpen) return lockScroll();
	});

	function openBrowse() {
		browseTab = 'library';
		genre = '';
		browseOpen = true;
		loadGenres();
	}
</script>

<svelte:window onkeydown={(e) => browseOpen && e.key === 'Escape' && (browseOpen = false)} />

{#snippet checkBadge()}
	<span class="absolute top-1.5 right-1.5 z-[3] grid h-7 w-7 place-items-center rounded-full bg-accent text-on-accent shadow-md">
		<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12l5 5 9-10" /></svg>
	</span>
	<span class="pointer-events-none absolute inset-0 z-[2] rounded-[14px] shadow-[inset_0_0_0_3px_var(--color-accent)]"></span>
{/snippet}

<section>
	<div class="card flex items-center gap-3.5 p-4">
		<div class="min-w-0 flex-1">
			<p class="text-base font-extrabold text-ink">
				{#if (poll.me?.nomination_count ?? 0) === 0}
					Add your picks
				{:else}
					You’ve added {poll.me?.nomination_count} {poll.me?.nomination_count === 1 ? 'title' : 'titles'}
				{/if}
			</p>
			<p class="mt-0.5 text-sm font-semibold text-muted">
				{guidance}{#if poll.genres.length > 0} · {poll.genres.join(', ')} only{/if}
			</p>
		</div>
		<button onclick={openBrowse} class="btn btn-primary h-[46px] shrink-0 text-base">
			<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
			Add titles
		</button>
	</div>

	{#if actionError}<p class="mt-3 text-sm font-bold text-coral-ink" role="alert">{actionError}</p>{/if}

	{#if poll.nominations.length === 0}
		<button
			onclick={openBrowse}
			class="mt-5 block w-full rounded-[20px] border-2 border-dashed border-line2 bg-surface2 px-6 py-10 text-center text-[15px] font-semibold text-muted transition hover:border-accent hover:text-ink"
		>
			Nothing nominated yet. Be the first — tap to browse the library.
		</button>
	{:else}
		<div class="mt-6 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
			<h2 class="section-h">Everyone’s picks <span class="text-muted">· {poll.nominations.length}</span></h2>
			<p class="text-[13px] font-semibold text-faint">Tap one to nominate it too, or to remove yours</p>
		</div>
		<div class="mt-3 grid grid-cols-3 gap-x-3 gap-y-4 sm:grid-cols-4 md:grid-cols-5">
			{#each poll.nominations as n (n.id)}
				<button
					onclick={() => toggle(n.item_id)}
					aria-pressed={n.mine_nominated}
					aria-label="{n.title}{n.mine_nominated ? ', your nomination — tap to remove' : ' — tap to nominate too'}"
					class="group min-w-0 text-left"
				>
					<div class="relative transition group-active:scale-[0.97]">
						<PosterImage itemId={n.item_id} tag={n.image_tag} posterUrl={n.poster_url ?? ''} title={n.title} />
						{#if n.mine_nominated}{@render checkBadge()}{/if}
						{#if n.nominator_count > 1}
							<span class="badge badge-count">×{n.nominator_count}</span>
						{/if}
						{#if n.source === 'seerr'}
							<span class="badge badge-req">Request</span>
						{/if}
					</div>
					<p class="mt-1.5 truncate text-sm font-bold text-ink">{n.title}</p>
					<p class="text-[13px] font-semibold text-faint">{n.mine_nominated ? 'Yours' : n.year || ''}</p>
				</button>
			{/each}
		</div>
	{/if}

	{#if isHost && !poll.timer}
		<div class="action-bar flex items-center gap-3">
			<div class="min-w-0 flex-1">
				<p class="text-[13px] font-extrabold text-muted">You’re hosting</p>
				<p class="truncate text-[15px] font-bold text-ink">
					{#if poll.nominations.length < 2}
						Need 2+ titles to {isRandom ? 'draw' : 'vote'}
					{:else}
						{poll.nominations.length} titles from {poll.participant_count} {poll.participant_count === 1 ? 'person' : 'people'}
					{/if}
				</p>
			</div>
			<button
				onclick={async () => update(await api.advance(code))}
				disabled={poll.nominations.length < 2}
				class="btn btn-coral h-[50px] shrink-0 text-base"
			>
				{isRandom ? 'Draw the winner' : 'Start voting'}
				<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12h14M13 6l6 6-6 6" /></svg>
			</button>
		</div>
	{/if}
</section>

{#if browseOpen}
	<div class="fixed inset-0 z-50 flex h-dvh flex-col bg-bg" role="dialog" aria-modal="true" aria-labelledby="browse-title">
		<div class="mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col">
			<div class="px-4 pt-2">
				<div class="-mr-2 flex items-center justify-between">
					<h2 id="browse-title" class="font-display text-2xl font-bold text-ink">Add titles</h2>
					<button onclick={() => (browseOpen = false)} aria-label="Close" class="grid h-11 w-11 place-items-center rounded-full text-muted hover:text-ink">
						<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12" /></svg>
					</button>
				</div>

				{#if canWriteIn}
					<div class="seg mt-2" role="group" aria-label="Where to search">
						<button aria-pressed={browseTab === 'library'} onclick={() => (browseTab = 'library')}>Your library</button>
						<button aria-pressed={browseTab === 'external'} onclick={() => (browseTab = 'external')}>Request something</button>
					</div>
				{/if}

				<div class="relative mt-2.5">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" class="pointer-events-none absolute top-1/2 left-3.5 -translate-y-1/2 text-faint" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="M20 20l-3.5-3.5" /></svg>
					<input
						bind:value={query}
						type="search"
						aria-label={browseTab === 'external' ? 'Search for any movie or show' : 'Search the library'}
						placeholder={browseTab === 'external' ? 'Search any movie or show' : 'Search the library'}
						autocomplete="off"
						class="input h-12 pl-10"
					/>
				</div>

				{#if browseTab === 'library' && (poll.library_scope === 'both' || genres.length > 1)}
					<div class="genre-scroll -mx-4 mt-2.5 flex gap-2 overflow-x-auto px-4 pb-1">
						{#if poll.library_scope === 'both'}
							{#each [['', 'All'], ['movie', 'Movies'], ['series', 'Shows']] as [val, label] (val)}
								<button onclick={() => (typeFilter = val)} class="chip shrink-0" aria-pressed={typeFilter === val}>{label}</button>
							{/each}
							{#if genres.length > 1}<span class="my-1.5 w-px shrink-0 bg-line2" aria-hidden="true"></span>{/if}
						{/if}
						{#if genres.length > 1}
							{#each genres as g (g)}
								<button onclick={() => (genre = genre === g ? '' : g)} class="chip chip-genre shrink-0" aria-pressed={genre === g}>{g}</button>
							{/each}
						{/if}
					</div>
				{/if}
			</div>

			<div class="mt-3 min-h-0 flex-1 overflow-y-auto overscroll-contain px-4">
				{#if searching}
					<p class="py-10 text-center font-semibold text-muted">Searching…</p>
				{:else if searchError}
					<p class="py-10 text-center font-semibold text-coral-ink">{searchError}</p>
				{:else if browseTab === 'external'}
					{#if query.trim().length < 2}
						<p class="py-10 text-center font-semibold text-muted">Type a title to find something to request.</p>
					{:else if externalItems.length === 0}
						<p class="py-10 text-center font-semibold text-muted">No titles found.</p>
					{:else}
						<div class="grid grid-cols-3 gap-x-3 gap-y-4 pb-6 sm:grid-cols-4 md:grid-cols-5">
							{#each externalItems as r (r.media_type + r.tmdb_id)}
								{@const key = writeInKey(r)}
								{@const picked = nominatedIds.has(key)}
								{@const blocked = r.in_library || (atMax && !picked)}
								<button
									onclick={() => !blocked && toggleExternal(r)}
									aria-pressed={picked}
									class="group min-w-0 text-left {blocked && !picked ? 'opacity-40' : ''}"
									disabled={blocked}
								>
									<div class="relative overflow-hidden rounded-[14px]">
										<PosterImage itemId={key} posterUrl={r.poster_url} title={r.title} />
										{#if picked}
											{@render checkBadge()}
										{:else if r.in_library}
											<div class="poster-pick poster-pick-lib"><span class="tag">In library</span></div>
										{/if}
									</div>
									<p class="mt-1.5 truncate text-sm font-bold text-ink">{r.title}</p>
									<p class="text-[13px] font-semibold text-faint">{r.year || ''}</p>
								</button>
							{/each}
						</div>
					{/if}
				{:else if items.length === 0}
					<p class="py-10 text-center font-semibold text-muted">No titles found.</p>
				{:else}
					<div class="grid grid-cols-3 gap-x-3 gap-y-4 pb-6 sm:grid-cols-4 md:grid-cols-5">
						{#each items as item (item.id)}
							{@const picked = nominatedIds.has(item.id)}
							{@const blocked = atMax && !picked}
							<button
								onclick={() => !blocked && toggle(item.id)}
								aria-pressed={picked}
								class="group min-w-0 text-left {blocked ? 'opacity-40' : ''}"
								disabled={blocked}
							>
								<div class="relative overflow-hidden rounded-[14px]">
									<PosterImage itemId={item.id} tag={item.image_tag} title={item.title} />
									{#if picked}{@render checkBadge()}{/if}
								</div>
								<p class="mt-1.5 truncate text-sm font-bold text-ink">{item.title}</p>
								<p class="text-[13px] font-semibold text-faint">{item.year || ''}</p>
							</button>
						{/each}
					</div>
				{/if}
			</div>

			<div class="border-t border-line px-4 pt-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))]">
				{#if actionError}<p class="mb-2 text-sm font-bold text-coral-ink" role="alert">{actionError}</p>{/if}
				<button onclick={() => (browseOpen = false)} class="btn btn-primary h-[52px] w-full text-[17px]">
					{#if mine.length > 0}Done · {mine.length} added{:else}Done{/if}
				</button>
			</div>
		</div>
	</div>
{/if}
