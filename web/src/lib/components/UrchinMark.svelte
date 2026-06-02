<script lang="ts">
	// The "Reef" urchin mark: 12 thick rounded spikes in poll colors
	// (coral, teal, mango, ocean) radiating from an ink hub.
	let { size = 28, class: cls = '' }: { size?: number; class?: string } = $props();
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 100 100"
	fill="none"
	xmlns="http://www.w3.org/2000/svg"
	class={cls}
	aria-hidden="true"
>
	<g stroke-width="9.2" stroke-linecap="round">
		<line x1="50" y1="33" x2="50" y2="5" stroke="#ff6f5e" />
		<line x1="58.5" y1="35.28" x2="72.5" y2="11.04" stroke="#11b3aa" />
		<line x1="64.72" y1="41.5" x2="88.97" y2="27.5" stroke="#ffa23a" />
		<line x1="67" y1="50" x2="95" y2="50" stroke="#0e5a7d" />
		<line x1="64.72" y1="58.5" x2="88.97" y2="72.5" stroke="#ff6f5e" />
		<line x1="58.5" y1="64.72" x2="72.5" y2="88.97" stroke="#11b3aa" />
		<line x1="50" y1="67" x2="50" y2="95" stroke="#ffa23a" />
		<line x1="41.5" y1="64.72" x2="27.5" y2="88.97" stroke="#0e5a7d" />
		<line x1="35.28" y1="58.5" x2="11.03" y2="72.5" stroke="#ff6f5e" />
		<line x1="33" y1="50" x2="5" y2="50" stroke="#11b3aa" />
		<line x1="35.28" y1="41.5" x2="11.03" y2="27.5" stroke="#ffa23a" />
		<line x1="41.5" y1="35.28" x2="27.5" y2="11.04" stroke="#0e5a7d" />
	</g>
	<circle cx="50" cy="50" r="14.5" fill="#143a45" />
	<circle cx="50" cy="50" r="6" fill="rgba(255,255,255,.14)" />
</svg>
