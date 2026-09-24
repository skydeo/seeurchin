// Lock page scrolling while a full-screen overlay/sheet is open.
//
// Without this, on iOS Safari a touch that lands outside the overlay's own
// scroller scrolls the page *behind* it, and the toolbar collapsing mid-gesture
// makes the whole overlay jump — the "page slips" bug. Nested locks are
// reference-counted so two sheets can't unlock each other early.
let depth = 0;

export function lockScroll(): () => void {
	if (typeof document === 'undefined') return () => {};
	const root = document.documentElement;
	if (depth++ === 0) {
		root.style.overflow = 'hidden';
		document.body.style.overflow = 'hidden';
	}
	let released = false;
	return () => {
		if (released) return;
		released = true;
		if (--depth === 0) {
			root.style.overflow = '';
			document.body.style.overflow = '';
		}
	};
}
