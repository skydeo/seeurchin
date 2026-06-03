<script lang="ts">
	import { api } from '$lib/api';
	import type { PollView } from '$lib/types';

	// Host-only controls for the active round's timer: start/resume, pause,
	// quick-add time, and end-now (which is just a host advance). Rendered next
	// to the Countdown; assumes poll.timer is present.
	let { poll, code, update }: { poll: PollView; code: string; update: (p: PollView) => void } =
		$props();

	const timer = $derived(poll.timer);
	const running = $derived(!!timer?.running);
	const paused = $derived(!!timer?.paused_sec && !timer?.running);
	const armed = $derived(!!timer?.armed);

	// Quick-add amounts scaled to the mode.
	const adds = $derived(
		timer?.mode === 'scheduled'
			? [
					{ label: '+1h', sec: 3600 },
					{ label: '+1d', sec: 86400 }
				]
			: [
					{ label: '+30s', sec: 30 },
					{ label: '+1m', sec: 60 }
				]
	);

	const endLabel = $derived(poll.status === 'round1' ? 'End now →' : 'Close now →');

	let busy = $state(false);
	let err = $state('');

	async function run(fn: () => Promise<PollView>) {
		busy = true;
		err = '';
		try {
			update(await fn());
		} catch (e) {
			err = e instanceof Error ? e.message : 'something went wrong';
		} finally {
			busy = false;
		}
	}
</script>

{#if timer}
	<div class="mt-2.5 flex flex-wrap items-center gap-2">
		{#if armed}
			<button class="btn btn-primary btn-sm" disabled={busy} onclick={() => run(() => api.startTimer(code))}>
				Start timer
			</button>
		{:else if paused}
			<button class="btn btn-primary btn-sm" disabled={busy} onclick={() => run(() => api.startTimer(code))}>
				Resume
			</button>
		{:else if running}
			<button class="btn btn-ghost btn-sm" disabled={busy} onclick={() => run(() => api.pauseTimer(code))}>
				Pause
			</button>
		{/if}

		{#if running || paused}
			{#each adds as a (a.sec)}
				<button class="btn btn-ghost btn-sm" disabled={busy} onclick={() => run(() => api.extendTimer(code, a.sec))}>
					{a.label}
				</button>
			{/each}
		{/if}

		<button class="btn btn-coral btn-sm" disabled={busy} onclick={() => run(() => api.advance(code))}>
			{endLabel}
		</button>
	</div>
	{#if err}<p class="mt-1.5 text-xs font-semibold text-coral-ink">{err}</p>{/if}
{/if}
