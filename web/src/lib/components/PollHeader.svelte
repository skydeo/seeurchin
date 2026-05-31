<script lang="ts">
	import type { PollView } from '$lib/types';

	let { poll }: { poll: PollView } = $props();

	let copied = $state(false);

	// Derive the share link from the origin the app was actually loaded from, so
	// it's correct regardless of SEEURCHIN_BASE_URL (localhost, LAN IP, or the
	// public tunnel hostname all just work). Falls back to the server value.
	const shareUrl = $derived(
		typeof window !== 'undefined' ? `${window.location.origin}/p/${poll.code}` : poll.share_url
	);

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
			if (navigator.clipboard && window.isSecureContext) {
				await navigator.clipboard.writeText(shareUrl);
			} else {
				// Clipboard API is unavailable on insecure origins (e.g. a LAN IP
				// over http). Fall back to a temporary textarea + execCommand.
				const ta = document.createElement('textarea');
				ta.value = shareUrl;
				ta.style.position = 'fixed';
				ta.style.top = '0';
				ta.style.opacity = '0';
				document.body.appendChild(ta);
				ta.focus();
				ta.select();
				document.execCommand('copy');
				ta.remove();
			}
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			// Last resort: show the link so it can be copied by hand.
			window.prompt('Copy this link:', shareUrl);
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
