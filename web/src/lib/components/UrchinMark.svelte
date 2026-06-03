<script lang="ts">
	// The "Reef" urchin mark: 12 thick spikes in poll colors (coral, teal,
	// mango, ocean) radiating from a clear two-tone hub. Kept in lockstep with
	// the OG-card renderer in internal/httpapi/preview.go.
	//
	// Each spike has a FLAT inner base (it tucks under the hub, so the arms read
	// as attached) and a ROUNDED outer tip. SVG can't give a single <line> two
	// different caps, so each spike = a butt-capped <line> (flat both ends) + a
	// <circle> at the outer end to round only the tip. The hub is a clear ink
	// ring with a distinct sun inner circle.
	let { size = 28, class: cls = '' }: { size?: number; class?: string } = $props();

	const COLORS = ['#ff6f5e', '#11b3aa', '#ffa23a', '#0e5a7d']; // coral, teal, mango, ocean
	const CX = 50,
		CY = 50,
		R_IN = 11, // inner base tucks under the hub (r=14.5)
		R_OUT = 45,
		SW = 9.2,
		TIP = SW / 2;

	const spikes = Array.from({ length: 12 }, (_, i) => {
		const a = ((i * 30 - 90) * Math.PI) / 180;
		return {
			x1: CX + Math.cos(a) * R_IN,
			y1: CY + Math.sin(a) * R_IN,
			x2: CX + Math.cos(a) * R_OUT,
			y2: CY + Math.sin(a) * R_OUT,
			c: COLORS[i % 4]
		};
	});
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
	<!-- Flat-capped shafts tucked under the hub -->
	<g stroke-width={SW} stroke-linecap="butt">
		{#each spikes as s}
			<line x1={s.x1.toFixed(2)} y1={s.y1.toFixed(2)} x2={s.x2.toFixed(2)} y2={s.y2.toFixed(2)} stroke={s.c} />
		{/each}
	</g>
	<!-- Discs round only the outer tips -->
	{#each spikes as s}
		<circle cx={s.x2.toFixed(2)} cy={s.y2.toFixed(2)} r={TIP} fill={s.c} />
	{/each}
	<!-- Clear two-tone hub: ink ring + sun inner circle -->
	<circle cx="50" cy="50" r="14.5" fill="#143a45" />
	<circle cx="50" cy="50" r="6" fill="#ffce5c" />
</svg>
