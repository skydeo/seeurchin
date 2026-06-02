import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Node global (the project's tsconfig references @types/node but it isn't
// installed); declared locally so this config type-checks without a new dep.
declare const process: { env: Record<string, string | undefined> };

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	// During `vite dev`, proxy API + SSE + image calls to the Go backend.
	// Override the target with SEEURCHIN_API_PROXY (e.g. to point at a running
	// container on :5858 instead of a local `go run` on :5859).
	server: {
		proxy: {
			'/api': {
				target: process.env.SEEURCHIN_API_PROXY || 'http://localhost:5859',
				changeOrigin: true
			}
		}
	}
});
