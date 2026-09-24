// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// Shallow-routing state: open sheets get their own history entry so the
		// browser back button / swipe-back closes them instead of leaving the page.
		interface PageState {
			options?: boolean; // home: More options sheet
			browse?: boolean; // poll: Add titles sheet
		}
		// interface Platform {}
	}
}

export {};
