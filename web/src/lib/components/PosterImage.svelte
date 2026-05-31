<script lang="ts">
	import { api } from '$lib/api';

	let {
		itemId,
		tag = '',
		title = '',
		posterUrl = ''
	}: { itemId: string; tag?: string; title?: string; posterUrl?: string } = $props();

	let failed = $state(false);
	// Write-ins carry an external (TMDB) poster URL; library items use the proxy.
	const src = $derived(posterUrl || api.imageURL(itemId, tag));
</script>

<div class="relative aspect-[2/3] w-full overflow-hidden rounded-xl bg-slate-800 ring-1 ring-white/5">
	{#if !failed}
		<img
			{src}
			alt={title}
			loading="lazy"
			onerror={() => (failed = true)}
			class="h-full w-full object-cover"
		/>
	{:else}
		<div
			class="flex h-full w-full items-center justify-center bg-gradient-to-br from-slate-700 to-slate-900 p-3 text-center text-sm font-medium text-slate-300"
		>
			{title}
		</div>
	{/if}
</div>
