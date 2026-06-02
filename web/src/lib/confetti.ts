/**
 * Brand-colored confetti for the winner reveal.
 *
 * Two corner cannons fire up-and-inward + a soft burst over the winner card;
 * particles get gravity, drag, spin and fade, then the canvas removes itself.
 * Honors prefers-reduced-motion (no-op).
 *
 * Usage (in Results.svelte):
 *   import { launchConfetti } from '$lib/confetti';
 *   onMount(() => { if (hasWinner) launchConfetti(hostEl); });
 *
 * @param host element to overlay (defaults to document.body). The canvas is
 *             absolutely positioned to fill it, so `host` should be positioned
 *             (position: relative) if you want it scoped.
 */
export function launchConfetti(host?: HTMLElement | null) {
	if (typeof window === 'undefined') return;
	if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;

	const target = host ?? document.body;
	target.querySelector('.confetti-canvas')?.remove();

	const w = target.clientWidth || window.innerWidth;
	const h = target.clientHeight || window.innerHeight;
	const dpr = Math.min(2, window.devicePixelRatio || 1);

	const cv = document.createElement('canvas');
	cv.className = 'confetti-canvas';
	cv.width = w * dpr;
	cv.height = h * dpr;
	cv.style.width = w + 'px';
	cv.style.height = h + 'px';
	target.appendChild(cv);

	const ctx = cv.getContext('2d');
	if (!ctx) {
		cv.remove();
		return;
	}
	ctx.scale(dpr, dpr);

	const colors = ['#ff6f5e', '#11b3aa', '#ffa23a', '#ffce5c', '#0e5a7d', '#1ccfc3'];
	const rand = (a: number, b: number) => a + Math.random() * (b - a);

	type P = {
		x: number; y: number; vx: number; vy: number;
		size: number; color: string; rot: number; vr: number;
		shape: 'rect' | 'circ'; life: number; ttl: number;
	};
	const parts: P[] = [];

	const cannon = (ox: number, oy: number, dir: 1 | -1) => {
		for (let i = 0; i < 60; i++) {
			const ang = dir === 1 ? rand(-1.32, -0.55) : rand(-2.59, -1.82); // up + inward
			const spd = rand(9, 19);
			parts.push({
				x: ox, y: oy,
				vx: Math.cos(ang) * spd, vy: Math.sin(ang) * spd,
				size: rand(5, 10), color: colors[(Math.random() * colors.length) | 0],
				rot: rand(0, 6.28), vr: rand(-0.3, 0.3),
				shape: Math.random() < 0.5 ? 'rect' : 'circ', life: 0, ttl: rand(95, 150)
			});
		}
	};
	cannon(0, h, 1); // bottom-left → right-up
	cannon(w, h, -1); // bottom-right → left-up
	for (let k = 0; k < 36; k++) {
		// soft burst over the winner card (upper-third center)
		const a = rand(-Math.PI, 0);
		const s = rand(2, 8);
		parts.push({
			x: w / 2, y: h * 0.34,
			vx: Math.cos(a) * s, vy: Math.sin(a) * s - 3,
			size: rand(5, 9), color: colors[(Math.random() * colors.length) | 0],
			rot: rand(0, 6.28), vr: rand(-0.3, 0.3),
			shape: Math.random() < 0.5 ? 'rect' : 'circ', life: 0, ttl: rand(90, 140)
		});
	}

	const grav = 0.34;
	const drag = 0.992;
	let frame = 0;
	const maxFrames = 170;

	const tick = () => {
		frame++;
		ctx.clearRect(0, 0, w, h);
		let alive = false;
		for (const p of parts) {
			p.life++;
			if (p.life > p.ttl) continue;
			p.vx *= drag;
			p.vy = p.vy * drag + grav;
			p.x += p.vx;
			p.y += p.vy;
			p.rot += p.vr;
			if (p.y > h + 20) continue;
			alive = true;
			const fade = p.life > p.ttl - 30 ? (p.ttl - p.life) / 30 : 1;
			ctx.save();
			ctx.globalAlpha = Math.max(0, fade);
			ctx.translate(p.x, p.y);
			ctx.rotate(p.rot);
			ctx.fillStyle = p.color;
			if (p.shape === 'rect') ctx.fillRect(-p.size / 2, -p.size / 2, p.size, p.size * 0.62);
			else {
				ctx.beginPath();
				ctx.arc(0, 0, p.size / 2, 0, 6.2832);
				ctx.fill();
			}
			ctx.restore();
		}
		if (alive && frame < maxFrames) requestAnimationFrame(tick);
		else cv.remove();
	};
	requestAnimationFrame(tick);
}
