import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	// During `vite dev`, proxy API + SSE + image calls to the Go backend.
	server: {
		proxy: {
			'/api': { target: 'http://localhost:5859', changeOrigin: true }
		}
	}
});
