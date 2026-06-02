/**
 * Reef appearance store.
 *
 * mode: "system" (default) | "light" | "dark"  — persisted in localStorage.
 * The resolved light/dark value is written to <html data-theme="…">, which the
 * Tailwind theme (layout.css) keys all colors off. A matching no-flash script
 * in app.html sets the attribute before first paint so there's no flicker.
 *
 * Svelte 5 runes module — import as `$lib/theme.svelte`.
 */
export type ThemeMode = 'system' | 'light' | 'dark';
const KEY = 'seeurchin-theme';

class ThemeStore {
	mode = $state<ThemeMode>('system');

	constructor() {
		if (typeof localStorage !== 'undefined') {
			const s = localStorage.getItem(KEY) as ThemeMode | null;
			if (s === 'light' || s === 'dark' || s === 'system') this.mode = s;
		}
		if (typeof window !== 'undefined') {
			window
				.matchMedia('(prefers-color-scheme: dark)')
				.addEventListener('change', () => {
					if (this.mode === 'system') this.apply();
				});
		}
	}

	/** The concrete theme currently shown. */
	get resolved(): 'light' | 'dark' {
		if (this.mode === 'system') {
			return typeof window !== 'undefined' &&
				window.matchMedia('(prefers-color-scheme: dark)').matches
				? 'dark'
				: 'light';
		}
		return this.mode;
	}

	apply() {
		if (typeof document !== 'undefined') {
			document.documentElement.setAttribute('data-theme', this.resolved);
		}
	}

	set(m: ThemeMode) {
		this.mode = m;
		try {
			if (m === 'system') localStorage.removeItem(KEY);
			else localStorage.setItem(KEY, m);
		} catch {
			/* ignore storage failures (private mode, etc.) */
		}
		this.apply();
	}

	/** system → light → dark → system */
	cycle() {
		const order: ThemeMode[] = ['system', 'light', 'dark'];
		this.set(order[(order.indexOf(this.mode) + 1) % 3]);
	}
}

export const theme = new ThemeStore();
