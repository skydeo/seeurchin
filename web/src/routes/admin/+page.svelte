<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { AdminPollSummary, PollView } from '$lib/types';
	import UrchinMark from '$lib/components/UrchinMark.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';

	type Phase = 'loading' | 'disabled' | 'login' | 'ready';
	type StatusFilter = 'all' | 'round1' | 'round2' | 'closed';

	let phase = $state<Phase>('loading');

	// login
	let token = $state('');
	let loginError = $state('');
	let loggingIn = $state(false);

	// history
	let polls = $state<AdminPollSummary[]>([]);
	let loadError = $state('');
	let busy = $state(''); // code of the row currently mutating

	// filters
	let statusFilter = $state<StatusFilter>('all');
	let search = $state('');

	// inline detail
	let openCode = $state<string | null>(null);
	let detail = $state<PollView | null>(null);
	let detailLoading = $state(false);

	const statusLabel: Record<string, string> = {
		draft: 'Draft',
		round1: 'Nominating',
		round2: 'Voting',
		closed: 'Results'
	};
	const statusClass: Record<string, string> = {
		draft: 'pill-draft',
		round1: 'pill-round1',
		round2: 'pill-round2',
		closed: 'pill-closed'
	};
	const filterOpts: [StatusFilter, string][] = [
		['all', 'All'],
		['round1', 'Nominating'],
		['round2', 'Voting'],
		['closed', 'Results']
	];

	onMount(async () => {
		try {
			const sess = await api.adminSession();
			phase = sess.authenticated ? 'ready' : 'login';
			if (sess.authenticated) await load();
		} catch {
			phase = 'disabled'; // 404 — no admin token configured on the server
		}
	});

	async function load() {
		loadError = '';
		try {
			polls = await api.adminPolls();
		} catch (e) {
			loadError = e instanceof Error ? e.message : 'could not load polls';
		}
	}

	async function login(e: Event) {
		e.preventDefault();
		if (loggingIn) return;
		loginError = '';
		loggingIn = true;
		try {
			await api.adminLogin(token.trim());
			token = '';
			phase = 'ready';
			await load();
		} catch (err) {
			loginError = err instanceof Error ? err.message : 'login failed';
		} finally {
			loggingIn = false;
		}
	}

	async function logout() {
		try {
			await api.adminLogout();
		} catch {
			/* ignore */
		}
		polls = [];
		openCode = null;
		detail = null;
		phase = 'login';
	}

	async function advance(p: AdminPollSummary) {
		const what =
			p.status === 'round2'
				? 'close the poll'
				: 'advance to voting (or close it, if there are fewer than two nominations)';
		if (!confirm(`Force "${p.title}" to ${what}?`)) return;
		busy = p.code;
		try {
			const updated = await api.adminAdvance(p.code);
			polls = polls.map((x) => (x.code === p.code ? updated : x));
			if (openCode === p.code) await openDetail(p.code, true);
		} catch (e) {
			alert(e instanceof Error ? e.message : 'could not advance poll');
		} finally {
			busy = '';
		}
	}

	async function del(p: AdminPollSummary) {
		if (!confirm(`Delete "${p.title}" and all of its data? This can't be undone.`)) return;
		busy = p.code;
		try {
			await api.adminDeletePoll(p.code);
			polls = polls.filter((x) => x.code !== p.code);
			if (openCode === p.code) {
				openCode = null;
				detail = null;
			}
		} catch (e) {
			alert(e instanceof Error ? e.message : 'could not delete poll');
		} finally {
			busy = '';
		}
	}

	async function toggleDetail(code: string) {
		if (openCode === code) {
			openCode = null;
			detail = null;
			return;
		}
		await openDetail(code, false);
	}

	async function openDetail(code: string, keepStale: boolean) {
		openCode = code;
		if (!keepStale) detail = null;
		detailLoading = true;
		try {
			detail = await api.adminPoll(code);
		} catch {
			detail = null;
		} finally {
			detailLoading = false;
		}
	}

	const filtered = $derived(
		polls.filter((p) => {
			if (statusFilter !== 'all' && p.status !== statusFilter) return false;
			const q = search.trim().toLowerCase();
			if (!q) return true;
			return p.title.toLowerCase().includes(q) || p.code.toLowerCase().includes(q);
		})
	);

	function fmtDate(s?: string): string {
		if (!s) return '';
		const d = new Date(s);
		if (isNaN(d.getTime())) return '';
		return d.toLocaleString(undefined, {
			month: 'short',
			day: 'numeric',
			year: 'numeric',
			hour: 'numeric',
			minute: '2-digit'
		});
	}
</script>

<svelte:head><title>seeurchin · admin</title></svelte:head>

<main class="mx-auto max-w-3xl px-4 py-8">
	<header class="mb-6 flex items-center justify-between gap-3">
		<a href="/" class="inline-flex items-center gap-1.5 text-sm font-bold text-accent hover:opacity-80">
			<span>←</span>
			<UrchinMark size={18} />
			<span class="font-display text-[15px] font-semibold tracking-tight">seeurchin</span>
		</a>
		<div class="flex items-center gap-2">
			{#if phase === 'ready'}
				<button onclick={logout} class="btn btn-ghost btn-sm">Log out</button>
			{/if}
			<ThemeToggle />
		</div>
	</header>

	{#if phase === 'loading'}
		<p class="py-12 text-center text-sm font-semibold text-muted">Loading…</p>
	{:else if phase === 'disabled'}
		<div class="card p-6 text-center">
			<h1 class="font-display text-xl font-semibold text-ink">Admin dashboard isn't enabled</h1>
			<p class="mt-2 text-sm font-semibold text-muted">
				Set <code class="font-title font-bold text-ink">SEEURCHIN_ADMIN_TOKEN</code> on the server to
				turn on the poll-history dashboard.
			</p>
		</div>
	{:else if phase === 'login'}
		<div class="mx-auto max-w-sm">
			<div class="text-center">
				<h1 class="font-display text-2xl font-semibold tracking-tight text-ink">Admin</h1>
				<p class="mt-1.5 text-sm font-semibold text-muted">
					Enter the admin token to view poll history.
				</p>
			</div>
			<form onsubmit={login} class="card mt-5 space-y-4 p-5">
				<label class="block">
					<span class="mb-1.5 block text-sm font-bold text-muted">Admin token</span>
					<input
						bind:value={token}
						type="password"
						required
						autocomplete="current-password"
						placeholder="••••••••"
						class="input"
					/>
				</label>
				{#if loginError}<p class="text-sm font-semibold text-coral-ink">{loginError}</p>{/if}
				<button type="submit" disabled={loggingIn} class="btn btn-primary w-full">
					{loggingIn ? 'Checking…' : 'Log in'}
				</button>
			</form>
		</div>
	{:else}
		<!-- ready: poll history -->
		<div class="mb-4 flex items-baseline gap-2.5">
			<h1 class="font-display text-2xl font-semibold tracking-tight text-ink">Poll history</h1>
			<span class="text-sm font-bold text-faint">{polls.length}</span>
		</div>

		<div class="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<div class="flex flex-wrap gap-2">
				{#each filterOpts as [val, label] (val)}
					<button
						type="button"
						onclick={() => (statusFilter = val)}
						class="chip"
						class:is-on={statusFilter === val}>{label}</button
					>
				{/each}
			</div>
			<input bind:value={search} placeholder="Search title or code" class="input sm:max-w-xs" />
		</div>

		{#if loadError}
			<p class="text-sm font-semibold text-coral-ink">{loadError}</p>
		{:else if filtered.length === 0}
			<p class="py-12 text-center text-sm font-semibold text-muted">
				{polls.length === 0 ? 'No polls yet.' : 'No polls match your filter.'}
			</p>
		{:else}
			<div class="space-y-3">
				{#each filtered as p (p.code)}
					<div class="card p-4">
						<div class="min-w-0">
							<div class="flex items-center gap-2">
								<h2 class="truncate font-display text-lg font-semibold text-ink">{p.title}</h2>
								<span class="pill {statusClass[p.status]} shrink-0">{statusLabel[p.status]}</span>
							</div>
							<div
								class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px] font-semibold text-muted"
							>
								<span class="font-title tracking-[0.14em] text-faint">{p.code}</span>
								<span>{p.participant_count} {p.participant_count === 1 ? 'person' : 'people'}</span>
								<span>{p.nomination_count} {p.nomination_count === 1 ? 'nom' : 'noms'}</span>
								{#if p.status !== 'round1'}<span>{p.voter_count} voted</span>{/if}
								<span>{p.voting_method_label}</span>
							</div>
							{#if p.status === 'closed' && p.winner_title}
								<p class="mt-1.5 text-sm font-bold text-accent-ink">🏆 {p.winner_title}</p>
							{/if}
							<p class="mt-1 text-xs font-semibold text-faint">
								created {fmtDate(p.created_at)}{#if p.closed_at} · ended {fmtDate(p.closed_at)}{/if}
							</p>
						</div>

						<div class="mt-3 flex flex-wrap gap-2">
							<button onclick={() => toggleDetail(p.code)} class="btn btn-ghost btn-sm">
								{openCode === p.code ? 'Hide' : 'Details'}
							</button>
							<a href={`/p/${p.code}`} class="btn btn-ghost btn-sm">Open</a>
							{#if p.status !== 'closed'}
								<button onclick={() => advance(p)} disabled={busy === p.code} class="btn btn-coral btn-sm">
									{p.status === 'round2' ? 'Close poll' : 'Advance'}
								</button>
							{/if}
							<button
								onclick={() => del(p)}
								disabled={busy === p.code}
								class="btn btn-ghost btn-sm text-coral-ink">Delete</button
							>
						</div>

						{#if openCode === p.code}
							<div class="panel mt-3 p-3">
								{#if detailLoading && !detail}
									<p class="text-sm font-semibold text-muted">Loading…</p>
								{:else if detail}
									{#if detail.nominations.length}
										<p class="mb-1.5 text-xs font-bold uppercase tracking-wide text-faint">
											Nominations
										</p>
										<ul class="space-y-1">
											{#each detail.nominations as n (n.id)}
												<li class="flex items-center justify-between gap-2 text-sm">
													<span class="truncate text-ink"
														>{n.title}{#if n.year}
															<span class="text-faint">({n.year})</span>{/if}</span
													>
													<span class="shrink-0 text-xs font-semibold text-faint"
														>{n.nominator_count}×</span
													>
												</li>
											{/each}
										</ul>
									{:else}
										<p class="text-sm font-semibold text-muted">No nominations.</p>
									{/if}

									{#if detail.results && detail.results.ranked.length}
										{@const max = Math.max(1, ...detail.results.ranked.map((r) => r.score))}
										<p class="mb-1.5 mt-3 text-xs font-bold uppercase tracking-wide text-faint">
											Results
										</p>
										<ul class="space-y-2">
											{#each detail.results.ranked as r (r.nomination_id)}
												{@const win = detail.results.winners.some(
													(w) => w.nomination_id === r.nomination_id
												)}
												<li>
													<div class="flex items-center justify-between gap-2 text-sm">
														<span class="truncate {win ? 'font-bold text-accent-ink' : 'text-ink'}"
															>{win ? '🏆 ' : ''}{r.title}</span
														>
														<span class="shrink-0 text-xs font-semibold text-muted">{r.score}</span>
													</div>
													<div class="bar mt-1 {win ? 'bar-win' : ''}">
														<i style={`width:${(r.score / max) * 100}%`}></i>
													</div>
												</li>
											{/each}
										</ul>
									{:else if detail.status === 'closed'}
										<p class="mt-3 text-sm font-semibold text-muted">No votes were cast.</p>
									{/if}
								{/if}
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	{/if}
</main>
