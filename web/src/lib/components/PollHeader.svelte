<script lang="ts">
	import type { PollView } from '$lib/types';

	let { poll }: { poll: PollView } = $props();

	let copied = $state(false);

	const statusLabel: Record<string, string> = {
		draft: 'Draft',
		round1: 'Nominating',
		round2: 'Voting',
		closed: 'Results'
	};
	const statusColor: Record<string, string> = {
		draft: 'bg-slate-500/20 text-slate-300',
		round1: 'bg-amber-500/20 text-amber-300',
		round2: 'bg-brand-500/20 text-brand-300',
		closed: 'bg-emerald-500/20 text-emerald-300'
	};

	async function copyLink() {
		try {
			await navigator.clipboard.writeText(poll.share_url);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			// ignore clipboard failures (e.g. insecure context)
		}
	}
</script>

<header class="mb-6">
	<a href="/" class="text-sm text-slate-400 hover:text-slate-200">← seeurchin</a>
	<div class="mt-1 flex flex-wrap items-center gap-3">
		<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{poll.title}</h1>
		<span class="rounded-full px-2.5 py-1 text-xs font-semibold {statusColor[poll.status]}">
			{statusLabel[poll.status]}
		</span>
	</div>
	<div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-slate-400">
		<button
			onclick={copyLink}
			class="inline-flex items-center gap-1.5 rounded-lg bg-slate-800 px-2.5 py-1 font-mono text-slate-200 ring-1 ring-white/10 hover:bg-slate-700"
			title="Copy share link"
		>
			<span class="tracking-widest">{poll.code}</span>
			<span class="text-xs text-slate-400">{copied ? '✓ copied' : 'copy link'}</span>
		</button>
		<span>{poll.participant_count} {poll.participant_count === 1 ? 'person' : 'people'}</span>
		{#if poll.status !== 'round1'}
			<span>{poll.voter_count} voted</span>
		{/if}
		<span class="text-slate-500">{poll.voting_method_label}</span>
	</div>
</header>
