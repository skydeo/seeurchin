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

<div class="poster">
	{#if !failed}
		<img
			{src}
			alt={title}
			loading="lazy"
			onerror={() => (failed = true)}
			class="absolute inset-0 h-full w-full object-cover"
		/>
	{:else}
		<div class="poster-fallback">{title}</div>
	{/if}
</div>
