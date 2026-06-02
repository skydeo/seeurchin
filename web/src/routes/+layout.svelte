<script lang="ts">
	import './layout.css';
	import { theme } from '$lib/theme.svelte';

	// Instantiate the theme store so the system-preference listener is active.
	// (The no-flash script in app.html has already set data-theme for first paint;
	//  favicons/manifest are declared there too.)
	void theme;

	let { children } = $props();
</script>

{@render children()}
