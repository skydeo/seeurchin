import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	compilerOptions: {
		// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
		runes: ({ filename }) => filename.split(/[/\\]/).includes('node_modules') ? undefined : true
	},
	kit: {
		// SPA: a single fallback page; all routing happens client-side and data
		// comes from the Go API. Output is written into the Go server's embed dir.
		adapter: adapter({
			fallback: 'index.html',
			pages: '../internal/httpapi/webdist',
			assets: '../internal/httpapi/webdist'
		})
	}
};

export default config;
