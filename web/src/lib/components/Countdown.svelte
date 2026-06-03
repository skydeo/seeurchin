<script lang="ts">
	import type { TimerView } from '$lib/types';

	// A quiet strip under the poll title that escalates as time runs out:
	// calm (faint) → warm (mango) → urgent (coral + glow/pulse) → final. The
	// server is the authority on when a round actually advances; this is a
	// cosmetic local countdown driven by closes_at vs. the server clock (we
	// measure the offset once per fetch, so a skewed device clock doesn't lie).
	let {
		timer,
		serverNow,
		kind
	}: { timer: TimerView; serverNow: string; kind: 'nominate' | 'vote' } = $props();

	const verb = $derived(kind === 'nominate' ? 'to nominate' : 'to vote');

	let offset = $state(0); // server clock − local clock, ms
	let remaining = $state(0); // seconds left on the active round
	let stage = $state<'calm' | 'warm' | 'urgent' | 'final'>('calm');
	let phase = $state<'counting' | 'up' | 'advancing'>('counting');

	const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

	// Ramp thresholds are relative to the round's full length (with caps), so the
	// escalation feels right for a 90-second in-room round and a multi-day one
	// alike — calm most of the way, ramping only in the final stretch.
	function stageFor(secLeft: number, total?: number): typeof stage {
		const t = total && total > 0 ? total : 90;
		const warmAt = clamp(t * 0.25, 60, 3600);
		const urgentAt = clamp(t * 0.08, 15, 300);
		if (secLeft <= 5) return 'final';
		if (secLeft <= urgentAt) return 'urgent';
		if (secLeft <= warmAt) return 'warm';
		return 'calm';
	}

	function friendly(sec: number): string {
		const s = Math.max(0, Math.ceil(sec));
		if (s < 60) return `${s}s`;
		if (s < 3600) return `${Math.round(s / 60)} min`;
		if (s < 86400) {
			const h = Math.floor(s / 3600);
			const m = Math.round((s % 3600) / 60);
			return m ? `${h}h ${m}m` : `${h}h`;
		}
		const d = Math.floor(s / 86400);
		const h = Math.round((s % 86400) / 3600);
		return h ? `${d}d ${h}h` : `${d}d`;
	}

	function computeRemaining(): number {
		if (timer.paused_sec && !timer.running) return timer.paused_sec;
		if (timer.armed) return timer.total_sec ?? 0;
		if (!timer.closes_at) return 0;
		return Math.max(0, (Date.parse(timer.closes_at) - (Date.now() + offset)) / 1000);
	}

	const running = $derived(timer.running && !!timer.closes_at);
	const paused = $derived(!!timer.paused_sec && !timer.running);

	// Re-sync to each fresh server payload, then tick locally while running.
	$effect(() => {
		void serverNow; // re-run on every refetch
		void timer.running;
		void timer.closes_at;
		void timer.paused_sec;

		offset = Date.parse(serverNow) - Date.now();
		phase = 'counting';
		remaining = computeRemaining();
		stage = running ? stageFor(remaining, timer.total_sec) : 'calm';

		if (!running) return; // armed/paused: nothing to tick

		const id = setInterval(() => {
			remaining = computeRemaining();
			stage = stageFor(remaining, timer.total_sec);
			if (remaining <= 0 && phase === 'counting') {
				phase = 'up';
				clearInterval(id);
				// Brief "Time's up" beat, then let the server's status event take over.
				setTimeout(() => (phase = 'advancing'), 800);
			}
		}, 1000);
		return () => clearInterval(id);
	});

	// Ring geometry: r = 8 on a 20×20 viewBox.
	const C = 2 * Math.PI * 8;
	const frac = $derived.by(() => {
		const total = timer.total_sec && timer.total_sec > 0 ? timer.total_sec : Math.max(remaining, 1);
		return clamp(remaining / total, 0, 1);
	});
	// While counting, the arc depletes; on expiry it pops to a full ring.
	const dashoffset = $derived(phase === 'counting' ? C * (1 - frac) : 0);

	const expired = $derived(phase !== 'counting');
	const dataStage = $derived(expired ? 'final' : stage);

	const label = $derived.by(() => {
		if (phase === 'up') return "Time's up!";
		if (phase === 'advancing') return kind === 'nominate' ? 'Starting the vote…' : 'Tallying the results…';
		if (paused) return `Paused · ${friendly(remaining)} left`;
		if (timer.armed) return `Timer ready · ${friendly(timer.total_sec ?? 0)}`;
		return `${friendly(remaining)} left ${verb}`;
	});
</script>

<div
	class="countdown"
	class:is-expired={expired}
	class:is-paused={paused && !expired}
	data-stage={dataStage}
	role="timer"
	aria-live={expired ? 'assertive' : 'off'}
>
	<span class="cd-ring">
		<svg viewBox="0 0 20 20" fill="none">
			<circle class="trk" cx="10" cy="10" r="8" stroke-width="2.4" />
			<circle
				class="prg"
				cx="10"
				cy="10"
				r="8"
				stroke-width="2.4"
				stroke-linecap="round"
				stroke-dasharray={C}
				stroke-dashoffset={dashoffset}
			/>
		</svg>
	</span>
	<span class="cd-label">{label}</span>
</div>

<style>
	.countdown {
		display: inline-flex;
		align-items: center;
		gap: 9px;
		padding: 7px 13px 7px 9px;
		border-radius: 999px;
		border: 1px solid transparent;
		background: transparent;
		--cd: var(--color-faint);
		color: var(--cd);
		max-width: 100%;
		transition:
			color 0.35s ease,
			background 0.35s ease,
			border-color 0.35s ease,
			box-shadow 0.35s ease;
		will-change: transform;
	}
	.cd-ring {
		width: 20px;
		height: 20px;
		flex-shrink: 0;
		display: block;
	}
	.cd-ring svg {
		width: 20px;
		height: 20px;
		transform: rotate(-90deg);
		display: block;
	}
	.cd-ring .trk {
		stroke: color-mix(in srgb, var(--cd) 24%, transparent);
	}
	.cd-ring .prg {
		stroke: var(--cd);
		transition:
			stroke-dashoffset 0.95s linear,
			stroke 0.35s ease;
	}
	.cd-label {
		font-size: 13px;
		font-weight: 700;
		letter-spacing: 0.005em;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}

	/* calm — barely there */
	.countdown[data-stage='calm'] {
		--cd: var(--color-faint);
	}
	.countdown.is-paused {
		--cd: var(--color-muted);
	}
	.countdown.is-paused .cd-ring .prg {
		transition: none; /* frozen, don't animate the arc */
	}

	/* warm — mango; the strip gains a faint home */
	.countdown[data-stage='warm'] {
		--cd: var(--color-mango);
		background: color-mix(in srgb, var(--color-mango) 12%, transparent);
		border-color: color-mix(in srgb, var(--color-mango) 30%, transparent);
	}
	.countdown[data-stage='warm'] .cd-label {
		font-weight: 800;
		font-size: 13.5px;
	}

	/* urgent — coral, bolder, soft glow + gentle pulse */
	.countdown[data-stage='urgent'] {
		--cd: var(--color-coral-ink);
		background: color-mix(in srgb, var(--color-coral) 16%, transparent);
		border-color: color-mix(in srgb, var(--color-coral) 44%, transparent);
		box-shadow:
			0 0 0 4px color-mix(in srgb, var(--color-coral) 12%, transparent),
			0 7px 20px -9px var(--color-coral);
		animation: cd-pulse 1.5s ease-in-out infinite;
	}
	.countdown[data-stage='urgent'] .cd-label {
		font-weight: 800;
		font-size: 14.5px;
	}

	/* final — ≤5s; tighter, faster pulse */
	.countdown[data-stage='final'] {
		--cd: var(--color-coral-ink);
		background: color-mix(in srgb, var(--color-coral) 22%, transparent);
		border-color: color-mix(in srgb, var(--color-coral) 62%, transparent);
		box-shadow:
			0 0 0 5px color-mix(in srgb, var(--color-coral) 16%, transparent),
			0 9px 24px -8px var(--color-coral);
		animation: cd-pulse 0.72s ease-in-out infinite;
	}
	.countdown[data-stage='final'] .cd-label {
		font-weight: 900;
		font-size: 15px;
	}

	/* expiry pop — overrides the pulse, keeps the final coloring */
	.countdown.is-expired {
		animation: cd-pop 0.5s cubic-bezier(0.18, 0.8, 0.3, 1.25) both;
	}

	@keyframes cd-pulse {
		0%,
		100% {
			transform: none;
		}
		50% {
			transform: scale(1.035);
		}
	}
	@keyframes cd-pop {
		0% {
			transform: scale(0.9);
		}
		55% {
			transform: scale(1.06);
		}
		100% {
			transform: none;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.countdown {
			animation: none !important;
		}
		.cd-ring .prg {
			transition: none;
		}
	}
</style>
