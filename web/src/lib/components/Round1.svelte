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
	let items = $state<LibraryItem[]>([]);
	let externalItems = $state<ExternalResult[]>([]);
	let searching = $state(false);
	let searchError = $state('');
	let timer: ReturnType<typeof setTimeout>;

	async function runSearch() {
		searching = true;
		searchError = '';
		try {
			items = (await api.library(code, query, typeFilter)).items;
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

	function openBrowse() {
		browseTab = 'library';
		browseOpen = true;
	}
</script>

<section>
	<div class="flex items-end justify-between gap-3">
		<div>
			<h2 class="text-lg font-semibold">Nominations</h2>
			<p class="text-sm text-slate-400">{guidance} · you've added {poll.me?.nomination_count ?? 0}</p>
			{#if poll.genres.length > 0}
				<p class="mt-0.5 text-xs text-brand-300">Limited to {poll.genres.join(', ')}</p>
			{/if}
		</div>
		<button
			onclick={openBrowse}
			class="shrink-0 rounded-xl bg-brand-500 px-4 py-2.5 text-sm font-semibold text-white hover:bg-brand-600"
		>
			＋ Add titles
		</button>
	</div>

	{#if actionError}<p class="mt-2 text-sm text-rose-400">{actionError}</p>{/if}

	{#if poll.nominations.length === 0}
		<button
			onclick={openBrowse}
			class="mt-6 block w-full rounded-2xl border border-dashed border-white/10 p-10 text-center text-slate-400 hover:border-brand-500/50 hover:text-slate-200"
		>
			Nothing nominated yet. Tap <span class="font-semibold text-slate-200">Add titles</span> to browse the library.
		</button>
	{:else}
		{#if (poll.me?.nomination_count ?? 0) === 0}
			<button
				onclick={openBrowse}
				class="mt-4 flex w-full items-center justify-between gap-3 rounded-2xl bg-brand-500/10 p-4 text-left ring-1 ring-brand-500/30 hover:bg-brand-500/15"
			>
				<span class="text-sm text-slate-200">
					You haven't added anything yet — tap to pick titles from the library.
				</span>
				<span class="shrink-0 rounded-lg bg-brand-500 px-3 py-1.5 text-sm font-semibold text-white">＋ Add titles</span>
			</button>
		{/if}
		<h3 class="mt-6 text-xs font-semibold uppercase tracking-wide text-slate-500">
			Everyone's picks so far ({poll.nominations.length}){#if mine.length > 0} · {mine.length} yours{/if}
		</h3>
		<div class="mt-3 grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-5">
			{#each poll.nominations as n (n.id)}
				<button onclick={() => toggle(n.item_id)} class="group text-left">
					<div class="relative">
						<PosterImage itemId={n.item_id} tag={n.image_tag} posterUrl={n.poster_url ?? ''} title={n.title} />
						{#if n.mine_nominated}
							<span class="absolute right-1.5 top-1.5 rounded-full bg-brand-500 px-2 py-0.5 text-[10px] font-bold text-white shadow">YOURS</span>
						{/if}
						{#if n.nominator_count > 1}
							<span class="absolute left-1.5 top-1.5 rounded-full bg-black/60 px-2 py-0.5 text-[10px] font-semibold text-white">×{n.nominator_count}</span>
						{/if}
						{#if n.source === 'seerr'}
							<span class="absolute bottom-1.5 left-1.5 rounded-full bg-amber-500/90 px-2 py-0.5 text-[10px] font-bold text-white shadow">REQUEST</span>
						{/if}
					</div>
					<p class="mt-1 truncate text-xs font-medium">{n.title}</p>
					<p class="text-[11px] text-slate-500">
						{n.mine_nominated ? 'Tap to remove' : 'Tap to also nominate'}
					</p>
				</button>
			{/each}
		</div>
	{/if}

	{#if mine.length > 0 && poll.me}
		<p class="mt-4 text-sm text-slate-400">Your picks: {mine.map((n) => n.title).join(', ')}</p>
	{/if}

	{#if isHost}
		<div class="mt-8 rounded-2xl bg-slate-900/70 p-4 ring-1 ring-white/10">
			<p class="text-sm text-slate-400">
				{#if isRandom}
					When everyone's done nominating, draw the winner. You need at least 2 nominations.
				{:else}
					When everyone's done nominating, start the vote. You need at least 2 nominations.
				{/if}
			</p>
			<button
				onclick={async () => update(await api.advance(code))}
				disabled={poll.nominations.length < 2}
				class="mt-3 w-full rounded-xl bg-emerald-500 px-4 py-3 font-semibold text-white hover:bg-emerald-600 disabled:opacity-40"
			>
				{isRandom ? '🎲 Pick the winner' : 'Start voting →'}
			</button>
		</div>
	{/if}
</section>

{#if browseOpen}
	<div class="fixed inset-0 z-50 flex flex-col bg-slate-950/95 backdrop-blur">
		<div class="mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col p-4">
			{#if canWriteIn}
				<div class="mb-3 flex gap-2 text-sm">
					{#each [ ['library', 'In your library'], ['external', 'Request something new'] ] as [val, label] (val)}
						<button
							onclick={() => (browseTab = val as typeof browseTab)}
							class="flex-1 rounded-xl px-3 py-2 font-medium {browseTab === val ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-300'}"
						>{label}</button>
					{/each}
				</div>
			{/if}

			<div class="flex items-center gap-3">
				<input
					bind:value={query}
					placeholder={browseTab === 'external' ? 'Search for any movie or show…' : 'Search the library…'}
					autocomplete="off"
					class="w-full rounded-xl bg-slate-800 px-4 py-3 ring-1 ring-white/10 outline-none focus:ring-2 focus:ring-brand-500"
				/>
				<button onclick={() => (browseOpen = false)} class="rounded-xl px-3 py-3 text-slate-400 hover:text-white">Done</button>
			</div>

			{#if poll.library_scope === 'both' && browseTab === 'library'}
				<div class="mt-3 flex gap-2 text-sm">
					{#each [ ['', 'All'], ['movie', 'Movies'], ['series', 'Shows'] ] as [val, label] (val)}
						<button
							onclick={() => (typeFilter = val)}
							class="rounded-full px-3 py-1 {typeFilter === val ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-300'}"
						>{label}</button>
					{/each}
				</div>
			{/if}

			<div class="mt-4 min-h-0 flex-1 overflow-y-auto overscroll-contain">
				{#if searching}
					<p class="py-10 text-center text-slate-400">Searching…</p>
				{:else if searchError}
					<p class="py-10 text-center text-rose-400">{searchError}</p>
				{:else if browseTab === 'external'}
					{#if query.trim().length < 2}
						<p class="py-10 text-center text-slate-400">Type a title to search for something to request.</p>
					{:else if externalItems.length === 0}
						<p class="py-10 text-center text-slate-400">No titles found.</p>
					{:else}
						<div class="grid grid-cols-3 gap-3 pb-6 sm:grid-cols-4 md:grid-cols-5">
							{#each externalItems as r (r.media_type + r.tmdb_id)}
								{@const key = writeInKey(r)}
								{@const picked = nominatedIds.has(key)}
								{@const blocked = r.in_library || (atMax && !picked)}
								<button
									onclick={() => !blocked && toggleExternal(r)}
									class="group text-left {blocked && !picked ? 'opacity-40' : ''}"
									disabled={blocked}
								>
									<div class="relative">
										<PosterImage itemId={key} posterUrl={r.poster_url} title={r.title} />
										{#if picked}
											<div class="absolute inset-0 flex items-center justify-center rounded-xl bg-brand-500/40 ring-2 ring-brand-400">
												<span class="rounded-full bg-brand-500 px-2 py-1 text-xs font-bold text-white">✓ Picked</span>
											</div>
										{:else if r.in_library}
											<div class="absolute inset-0 flex items-center justify-center rounded-xl bg-black/50">
												<span class="rounded-full bg-slate-700 px-2 py-1 text-xs font-semibold text-white">In library</span>
											</div>
										{/if}
									</div>
									<p class="mt-1 truncate text-xs font-medium">{r.title}</p>
									<p class="text-[11px] text-slate-500">{r.year || ''}</p>
								</button>
							{/each}
						</div>
					{/if}
				{:else if items.length === 0}
					<p class="py-10 text-center text-slate-400">No titles found.</p>
				{:else}
					<div class="grid grid-cols-3 gap-3 pb-6 sm:grid-cols-4 md:grid-cols-5">
						{#each items as item (item.id)}
							{@const picked = nominatedIds.has(item.id)}
							{@const blocked = atMax && !picked}
							<button
								onclick={() => !blocked && toggle(item.id)}
								class="group text-left {blocked ? 'opacity-40' : ''}"
								disabled={blocked}
							>
								<div class="relative">
									<PosterImage itemId={item.id} tag={item.image_tag} title={item.title} />
									{#if picked}
										<div class="absolute inset-0 flex items-center justify-center rounded-xl bg-brand-500/40 ring-2 ring-brand-400">
											<span class="rounded-full bg-brand-500 px-2 py-1 text-xs font-bold text-white">✓ Picked</span>
										</div>
									{/if}
								</div>
								<p class="mt-1 truncate text-xs font-medium">{item.title}</p>
								<p class="text-[11px] text-slate-500">{item.year || ''}</p>
							</button>
						{/each}
					</div>
				{/if}
			</div>
		</div>
	</div>
{/if}
