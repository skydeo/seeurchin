<script lang="ts">
	import type { PollView } from '$lib/types';
	import UrchinMark from './UrchinMark.svelte';
	import ThemeToggle from './ThemeToggle.svelte';

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
	const statusClass: Record<string, string> = {
		draft: 'pill-draft',
		round1: 'pill-round1',
		round2: 'pill-round2',
		closed: 'pill-closed'
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
	<div class="flex items-center justify-between gap-3">
		<a href="/" class="inline-flex items-center gap-1.5 text-sm font-bold text-accent hover:opacity-80">
			<span>←</span>
			<UrchinMark size={17} />
			<span class="font-display text-[15px] font-semibold tracking-tight">seeurchin</span>
		</a>
		<ThemeToggle />
	</div>

	<div class="mt-3 flex items-center gap-2.5">
		<h1 class="font-display text-2xl font-semibold tracking-tight text-ink sm:text-[28px]">{poll.title}</h1>
		<span class="pill {statusClass[poll.status]}">{statusLabel[poll.status]}</span>
	</div>

	<div class="mt-2.5">
		<button
			onclick={copyLink}
			class="inline-flex items-center gap-2 rounded-[10px] border border-line bg-surface3 px-2.5 py-1.5 text-ink"
			title="Copy share link"
		>
			<span class="font-title font-bold tracking-[0.16em]">{poll.code}</span>
			<span class="text-xs font-bold text-accent">{copied ? '✓ copied' : 'copy link'}</span>
		</button>
	</div>

	<div class="mt-2.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[13px] font-semibold text-muted">
		<span>{poll.participant_count} {poll.participant_count === 1 ? 'person' : 'people'}</span>
		{#if poll.status !== 'round1'}
			<span class="text-faint">·</span>
			<span>{poll.voter_count} voted</span>
		{/if}
		<span class="text-faint">·</span>
		<span>{poll.voting_method_label}</span>
	</div>
</header>
