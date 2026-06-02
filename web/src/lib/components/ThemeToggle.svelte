<script lang="ts">
	import { theme } from '$lib/theme.svelte';

	const label = $derived(
		theme.mode === 'light' ? 'Light' : theme.mode === 'dark' ? 'Dark' : 'Auto'
	);
</script>

<button
	type="button"
	onclick={() => theme.cycle()}
	class="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-full border border-line bg-surface px-2.5 text-xs font-extrabold text-muted shadow-sm transition hover:border-line2 hover:text-ink"
	title={`Appearance: ${label} — tap to change`}
	aria-label={`Appearance: ${label}. Tap to change.`}
>
	{#if theme.mode === 'light'}
		<!-- sun -->
		<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
			<circle cx="12" cy="12" r="4.2" />
			<path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5 5l1.4 1.4M17.6 17.6L19 19M19 5l-1.4 1.4M6.4 17.6L5 19" />
		</svg>
	{:else if theme.mode === 'dark'}
		<!-- moon -->
		<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
			<path d="M20 14.5A8 8 0 1 1 9.5 4a6.3 6.3 0 0 0 10.5 10.5z" />
		</svg>
	{:else}
		<!-- auto (half-filled) -->
		<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
			<circle cx="12" cy="12" r="9" />
			<path d="M12 3a9 9 0 0 0 0 18z" fill="currentColor" stroke="none" />
		</svg>
	{/if}
	<span>{label}</span>
</button>
